package patching

import (
	"testing"
	"time"

	"ourway/server/models"
	"ourway/server/ws"
)

func utc(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, time.UTC) }

func TestInWindowBasics(t *testing.T) {
	p := &models.PatchPolicy{Schedule: "daily", WindowStart: "02:00", WindowHours: 4, Timezone: "UTC"}
	for _, tc := range []struct {
		at time.Time
		in bool
	}{
		{utc(2026, 10, 4, 1, 59), false},
		{utc(2026, 10, 4, 2, 0), true},
		{utc(2026, 10, 4, 5, 59), true},
		{utc(2026, 10, 4, 6, 0), false},
	} {
		if _, _, in := inWindow(p, tc.at); in != tc.in {
			t.Errorf("at %v: in=%v want %v", tc.at, in, tc.in)
		}
	}
}

func TestInWindowCrossesMidnightOnScheduledDay(t *testing.T) {
	// Weekly (Sunday) 22:00 for 6h: runs Sunday 22:00 -> Monday 04:00.
	p := &models.PatchPolicy{Schedule: "weekly", WindowStart: "22:00", WindowHours: 6}
	sundayLate := utc(2026, 10, 4, 23, 0)
	mondayEarly := utc(2026, 10, 5, 3, 0)
	mondayLate := utc(2026, 10, 5, 23, 0)
	if _, _, in := inWindow(p, sundayLate); !in {
		t.Error("Sunday 23:00 should be inside the window")
	}
	start, _, in := inWindow(p, mondayEarly)
	if !in || !start.Equal(utc(2026, 10, 4, 22, 0)) {
		t.Errorf("Monday 03:00 belongs to Sunday's window, got in=%v start=%v", in, start)
	}
	if _, _, in := inWindow(p, mondayLate); in {
		t.Error("Monday 23:00 is not a scheduled day")
	}
}

func TestInWindowUsesPolicyTimezone(t *testing.T) {
	// 02:00 America/New_York (EDT, UTC-4) is 06:00 UTC.
	p := &models.PatchPolicy{Schedule: "daily", WindowStart: "02:00", WindowHours: 1, Timezone: "America/New_York"}
	if _, _, in := inWindow(p, utc(2026, 10, 4, 6, 30)); !in {
		t.Error("06:30 UTC should be inside a 02:00 New York window")
	}
	if _, _, in := inWindow(p, utc(2026, 10, 4, 2, 30)); in {
		t.Error("02:30 UTC is 22:30 New York, outside the window")
	}
}

func TestLegacyPolicyIsWholeDayAndRunsOnce(t *testing.T) {
	p := &models.PatchPolicy{Schedule: "daily"} // no window fields
	now := utc(2026, 10, 4, 15, 0)
	if _, _, due := policyDue(p, now); !due {
		t.Fatal("legacy policy should be due on a scheduled day")
	}
	ran := now
	p.LastRunAt = &ran
	if _, _, due := policyDue(p, now.Add(time.Hour)); due {
		t.Fatal("must not run again later in the same window (it used to fire every hour)")
	}
	if _, _, due := policyDue(p, now.Add(24*time.Hour)); !due {
		t.Fatal("must run again in the next day's window")
	}
}

func TestInvalidWindowStartNeverRuns(t *testing.T) {
	p := &models.PatchPolicy{Schedule: "daily", WindowStart: "25:99"}
	if _, _, in := inWindow(p, utc(2026, 10, 4, 3, 0)); in {
		t.Fatal("unparseable window start must not match")
	}
}

func TestShouldAutoReboot(t *testing.T) {
	dep := &models.PatchDeployment{AutoReboot: true}
	if !shouldAutoReboot(dep, &models.Device{RebootPending: true}) {
		t.Error("should reboot when flagged and pending")
	}
	if shouldAutoReboot(dep, &models.Device{RebootPending: false}) {
		t.Error("must not reboot a device that does not need it")
	}
	if shouldAutoReboot(&models.PatchDeployment{}, &models.Device{RebootPending: true}) {
		t.Error("must not reboot when the deployment did not ask for it")
	}
	if shouldAutoReboot(nil, nil) {
		t.Error("nil safe")
	}
}

func TestPolicyRunQueuesOfflineDevicesAndRunsOncePerWindow(t *testing.T) {
	st := newPolicyTestStore(t)
	hub := ws.NewHub("http://localhost:3000", "")
	e := NewPolicyEngine(st, hub, NewDeployer(st, hub), NewRebooter(hub), NewScanner(st, hub))
	e.ScanWait = 0
	_ = st.Devices.Create(&models.Device{ID: "off-1", Name: "off", Hostname: "off", OS: "windows", Arch: "amd64", Status: "offline", DeviceKey: "k-off"})
	_ = st.PatchPolicies.Create(&models.PatchPolicy{ID: "pol-w", Name: "nightly", Scope: "all", Schedule: "daily",
		WindowStart: "02:00", WindowHours: 4, Timezone: "UTC", ApprovalRequired: true, MaxDevicesPerBatch: 10})

	e.Now = func() time.Time { return utc(2026, 10, 4, 3, 0) }
	e.EvaluateDue(nil)

	q, _ := st.QueuedDeploys.ListActive(e.Now())
	if len(q) != 1 || q[0].DeviceID != "off-1" || q[0].PolicyID != "pol-w" {
		t.Fatalf("offline device should be queued for the policy, got %+v", q)
	}
	if !q[0].ExpiresAt.Equal(utc(2026, 10, 4, 6, 0)) {
		t.Errorf("queue entry should expire at window end 06:00, got %v", q[0].ExpiresAt)
	}
	pol, _ := st.PatchPolicies.GetByID("pol-w")
	if pol.LastRunAt == nil {
		t.Fatal("run must be recorded")
	}

	// Same window, later tick: no second run (queue unchanged, one entry).
	e.Now = func() time.Time { return utc(2026, 10, 4, 4, 0) }
	e.EvaluateDue(nil)
	if q, _ := st.QueuedDeploys.ListActive(e.Now()); len(q) != 1 {
		t.Fatalf("expected a single queue entry, got %d", len(q))
	}
}

func TestProcessQueueGatesOnWindowAndPrunes(t *testing.T) {
	st := newPolicyTestStore(t)
	hub := ws.NewHub("http://localhost:3000", "")
	e := NewPolicyEngine(st, hub, NewDeployer(st, hub), NewRebooter(hub), NewScanner(st, hub))
	e.ScanWait = 0
	_ = st.Devices.Create(&models.Device{ID: "d1", Name: "d1", Hostname: "d1", OS: "linux", Arch: "amd64", Status: "online", DeviceKey: "k1"})
	_ = st.PatchPolicies.Create(&models.PatchPolicy{ID: "pol-q", Name: "q", Scope: "all", Schedule: "daily", WindowStart: "02:00", WindowHours: 2, ApprovalRequired: true})
	_ = st.QueuedDeploys.Enqueue("d1", "pol-q", utc(2026, 10, 5, 0, 0))
	_ = st.QueuedDeploys.Enqueue("ghost", "", utc(2026, 10, 3, 0, 0)) // already expired

	// Outside the 02:00-04:00 window the policy entry is kept and nothing deploys.
	e.Now = func() time.Time { return utc(2026, 10, 4, 12, 0) }
	e.ProcessQueue(nil)
	if q, _ := st.QueuedDeploys.ListActive(e.Now()); len(q) != 1 {
		t.Fatalf("policy entry must wait for its window, got %d entries", len(q))
	}
	if deps, _ := st.PatchDeployments.ListAll(); len(deps) != 0 {
		t.Fatalf("nothing should deploy outside the window, got %d", len(deps))
	}

	// Inside the window the device has no approved updates, so the entry is
	// consumed without creating a deployment.
	e.Now = func() time.Time { return utc(2026, 10, 4, 3, 0) }
	e.ProcessQueue(nil)
	if q, _ := st.QueuedDeploys.ListActive(e.Now()); len(q) != 0 {
		t.Fatalf("entry should be consumed, got %d", len(q))
	}
}

func TestDeployToOfflineDeviceQueuesIt(t *testing.T) {
	st := newPolicyTestStore(t)
	hub := ws.NewHub("http://localhost:3000", "")
	d := NewDeployer(st, hub)
	_ = st.Devices.Create(&models.Device{ID: "d9", Name: "d9", Hostname: "d9", OS: "linux", Arch: "amd64", Status: "offline", DeviceKey: "k9"})
	_ = st.SoftwareUpdates.Create(&models.SoftwareUpdate{ID: "u9", DeviceID: "d9", Source: "apt", Title: "curl", Status: "approved"})

	if _, err := d.DeployToDevices(nil, []string{"d9"}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if q, _ := st.QueuedDeploys.ListActive(time.Now()); len(q) != 1 || q[0].PolicyID != "" {
		t.Fatalf("manual deploy to an offline device must be queued, got %+v", q)
	}
	if u, _ := st.SoftwareUpdates.GetByID("u9"); u.Status != "approved" {
		t.Fatalf("update must stay approved for the retry, got %q", u.Status)
	}
}
