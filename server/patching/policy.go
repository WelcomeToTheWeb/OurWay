package patching

import (
	"context"
	"log"
	"strings"
	"time"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// PolicyEngine periodically evaluates patch policies and drives the
// scan -> approve -> deploy -> (optional) reboot pipeline for the devices
// each policy scopes to. Schedule, Scope, AutoReboot and
// MaxDevicesPerBatch were previously stored but never read; this engine is
// their consumer.
type PolicyEngine struct {
	store    *store.Store
	hub      *ws.Hub
	deployer *Deployer
	rebooter *Rebooter
	scanner  *Scanner
	// Now is swappable so tests can pin the evaluation time.
	Now func() time.Time
	// ScanWait is how long a run waits for freshly requested scans to
	// report before approving and deploying (default 2 minutes).
	ScanWait time.Duration
}

// NewPolicyEngine creates a patch policy engine.
func NewPolicyEngine(store *store.Store, hub *ws.Hub, deployer *Deployer, rebooter *Rebooter, scanner *Scanner) *PolicyEngine {
	return &PolicyEngine{
		store:    store,
		hub:      hub,
		deployer: deployer,
		rebooter: rebooter,
		scanner:  scanner,
		Now:      time.Now,
		ScanWait: 2 * time.Minute,
	}
}

// Run starts the periodic loop: every minute it starts policies whose
// maintenance window has opened and retries queued deployments for devices
// that came back online. Stops when ctx is cancelled.
func (e *PolicyEngine) Run(ctx context.Context) {
	const interval = time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.EvaluateDue(ctx)
			e.ProcessQueue(ctx)
		}
	}
}

// EvaluateDue runs every policy that is inside its maintenance window and
// has not yet run in it. It is exported (and uses e.Now) so it can be
// tested without waiting for wall-clock windows.
func (e *PolicyEngine) EvaluateDue(ctx context.Context) {
	now := e.Now()
	policies, err := e.store.PatchPolicies.ListAll()
	if err != nil {
		log.Printf("policy: failed to list policies: %v", err)
		return
	}
	for i := range policies {
		policy := policies[i]
		if _, _, due := policyDue(&policy, now); !due {
			continue
		}
		// Record the run first: a policy that fails halfway must not
		// restart on the next tick of the same window.
		ran := now
		policy.LastRunAt = &ran
		if err := e.store.PatchPolicies.Update(&policy); err != nil {
			log.Printf("policy: failed to record run of %s: %v", policy.Name, err)
			continue
		}
		if err := e.applyPolicy(ctx, &policy); err != nil {
			log.Printf("policy: failed to apply policy %s (%s): %v", policy.Name, policy.ID, err)
		}
	}
}

// ProcessQueue retries queued deployments whose device is back online.
// Policy entries only run inside their policy's maintenance window;
// manual entries run as soon as the device returns.
func (e *PolicyEngine) ProcessQueue(ctx context.Context) {
	now := e.Now()
	if err := e.store.QueuedDeploys.DeleteExpired(now); err != nil {
		log.Printf("policy: failed to prune deploy queue: %v", err)
	}
	entries, err := e.store.QueuedDeploys.ListActive(now)
	if err != nil {
		log.Printf("policy: failed to read deploy queue: %v", err)
		return
	}
	for _, q := range entries {
		device, err := e.store.Devices.GetByID(q.DeviceID)
		if err != nil {
			_ = e.store.QueuedDeploys.Delete(q.ID)
			continue
		}
		if device.Status != "online" {
			continue
		}
		opts := DeployOptions{}
		if q.PolicyID != "" {
			policy, err := e.store.PatchPolicies.GetByID(q.PolicyID)
			if err != nil {
				_ = e.store.QueuedDeploys.Delete(q.ID) // policy was deleted
				continue
			}
			if _, _, in := inWindow(policy, now); !in {
				continue
			}
			opts = DeployOptions{PolicyID: policy.ID, AutoReboot: policy.AutoReboot}
			if !policy.ApprovalRequired {
				if err := e.autoApprove([]string{q.DeviceID}); err != nil {
					log.Printf("policy: auto-approve for returning device %s failed: %v", device.Name, err)
				}
			}
		}
		// Drop the entry first: a still-unreachable device re-queues
		// itself inside DeployToDevicesWithOptions.
		_ = e.store.QueuedDeploys.Delete(q.ID)
		if _, err := e.deployer.DeployToDevicesWithOptions(ctx, []string{q.DeviceID}, opts); err != nil && err != ErrNoApprovedUpdates {
			log.Printf("policy: queued deploy for %s failed: %v", device.Name, err)
		}
	}
}

// applyPolicy resolves the policy's device scope, scans those devices for
// updates, auto-approves detected updates when approval is not required,
// deploys the approved set in batches of MaxDevicesPerBatch, and reboots
// the devices that reported success when AutoReboot is set.
func (e *PolicyEngine) applyPolicy(ctx context.Context, policy *models.PatchPolicy) error {
	deviceIDs, err := e.resolveScope(policy)
	if err != nil {
		return err
	}
	if len(deviceIDs) == 0 {
		log.Printf("policy: %s matched 0 devices, nothing to do", policy.Name)
		return nil
	}
	online := make([]string, 0, len(deviceIDs))
	windowEnd := e.Now().Add(time.Hour)
	if _, end, in := inWindow(policy, e.Now()); in {
		windowEnd = end
	}
	for _, id := range deviceIDs {
		device, err := e.store.Devices.GetByID(id)
		if err != nil {
			continue
		}
		if device.Status == "online" {
			online = append(online, id)
		} else if err := e.store.QueuedDeploys.Enqueue(id, policy.ID, windowEnd); err != nil {
			// Offline now: queued so it patches if it returns during the window.
			log.Printf("policy: failed to queue offline device %s: %v", device.Name, err)
		}
	}
	// Refresh patch inventory for the online devices so approvals are
	// made against current data, then auto-approve when allowed.
	for _, id := range online {
		device, err := e.store.Devices.GetByID(id)
		if err != nil {
			continue
		}
		if err := e.hub.SendToDevice(device.DeviceKey, "scan_updates", map[string]interface{}{
			"device_id": id,
		}); err != nil {
			log.Printf("policy: scan send failed for device %s: %v", id, err)
		}
	}
	if len(online) > 0 && e.ScanWait > 0 {
		// Give the agents time to report what the scan found, otherwise
		// this run would approve and deploy last cycle's inventory.
		select {
		case <-time.After(e.ScanWait):
		case <-ctxDone(ctx):
			return ctx.Err()
		}
	}
	if !policy.ApprovalRequired {
		if err := e.autoApprove(deviceIDs); err != nil {
			return err
		}
	}
	// Batch the deploy (MaxDevicesPerBatch, default 10 when unset). Reboots
	// happen when each device reports its result (DeployOptions.AutoReboot).
	batchSize := policy.MaxDevicesPerBatch
	if batchSize <= 0 {
		batchSize = 10
	}
	for start := 0; start < len(online); start += batchSize {
		end := start + batchSize
		if end > len(online) {
			end = len(online)
		}
		batch := online[start:end]
		deploymentID, err := e.deployer.DeployToDevicesWithOptions(ctx, batch, DeployOptions{
			PolicyID:   policy.ID,
			AutoReboot: policy.AutoReboot,
			QueueTTL:   windowEnd.Sub(e.Now()),
		})
		if err != nil {
			if err == ErrNoApprovedUpdates {
				continue
			}
			log.Printf("policy: deploy failed for batch of %d: %v", len(batch), err)
			continue
		}
		log.Printf("policy: %s deployment %s started for %d devices", policy.Name, deploymentID, len(batch))
	}
	return nil
}

// resolveScope maps a policy's Scope/ScopeValue to concrete device IDs.
func (e *PolicyEngine) resolveScope(policy *models.PatchPolicy) ([]string, error) {
	devices, err := e.store.Devices.ListAll()
	if err != nil {
		return nil, err
	}
	switch policy.Scope {
	case "all":
		ids := make([]string, 0, len(devices))
		for _, d := range devices {
			ids = append(ids, d.ID)
		}
		return ids, nil
	case "tags", "devices":
		// ScopeValue is comma-separated. For scope "tags" the entries are
		// device tags; for "devices" they are device IDs, names or
		// hostnames. (Scope "tags" also still honours IDs/names so
		// policies saved before tags existed keep matching.)
		wanted := make(map[string]bool)
		var wantedTags []string
		for _, part := range strings.Split(policy.ScopeValue, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				wanted[part] = true
				wantedTags = append(wantedTags, strings.ToLower(part))
			}
		}
		var ids []string
		for _, d := range devices {
			if wanted[d.ID] || wanted[d.Name] || wanted[d.Hostname] ||
				(policy.Scope == "tags" && d.HasAnyTag(wantedTags)) {
				ids = append(ids, d.ID)
			}
		}
		return ids, nil
	default:
		log.Printf("policy: unknown scope %q on policy %s, treating as no devices", policy.Scope, policy.Name)
		return nil, nil
	}
}

// autoApprove marks every detected update for the scoped devices approved
// so the deploy phase can pick them up.
func (e *PolicyEngine) autoApprove(deviceIDs []string) error {
	for _, deviceID := range deviceIDs {
		updates, err := e.store.SoftwareUpdates.ListByDeviceAndStatus(deviceID, "detected")
		if err != nil {
			log.Printf("policy: failed to list detected updates for device %s: %v", deviceID, err)
			continue
		}
		for i := range updates {
			if _, err := e.store.SoftwareUpdates.MarkApproved(updates[i].ID); err != nil {
				log.Printf("policy: failed to approve update %s: %v", updates[i].ID, err)
			}
		}
	}
	return nil
}

// scheduleDue reports whether the schedule (daily/weekly/monthly) is due
// on the day of t. Daily matches every day; weekly Sundays; monthly the
// 1st. Run-once-per-window is enforced by policyDue via LastRunAt.
func scheduleDue(schedule string, now time.Time) bool {
	switch strings.ToLower(strings.TrimSpace(schedule)) {
	case "daily":
		return true
	case "weekly":
		return now.Weekday() == time.Sunday
	case "monthly":
		return now.Day() == 1
	default:
		return false
	}
}

// ctxDone returns ctx.Done(), or a nil channel (never ready) for a nil ctx.
func ctxDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}

// parseWindowStart parses "HH:MM" (empty means midnight).
func parseWindowStart(v string) (hour, min int, ok bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, 0, true
	}
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}

// policyLocation resolves the policy's time zone (UTC when empty/unknown).
func policyLocation(policy *models.PatchPolicy) *time.Location {
	if policy.Timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(policy.Timezone)
	if err != nil {
		log.Printf("policy: unknown timezone %q on %s, using UTC", policy.Timezone, policy.Name)
		return time.UTC
	}
	return loc
}

// inWindow reports whether now falls inside one of the policy's scheduled
// maintenance windows, returning that window's bounds. A window starts at
// WindowStart (policy time zone) on a day the schedule selects and lasts
// WindowHours (default 24); it may run past midnight, so yesterday's
// window is considered too.
func inWindow(policy *models.PatchPolicy, now time.Time) (start, end time.Time, in bool) {
	hour, min, ok := parseWindowStart(policy.WindowStart)
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	dur := time.Duration(policy.WindowHours) * time.Hour
	if policy.WindowHours <= 0 || policy.WindowHours > 24 {
		dur = 24 * time.Hour
	}
	loc := policyLocation(policy)
	local := now.In(loc)
	for _, back := range []int{0, -1} {
		day := local.AddDate(0, 0, back)
		start = time.Date(day.Year(), day.Month(), day.Day(), hour, min, 0, 0, loc)
		end = start.Add(dur)
		if !local.Before(start) && local.Before(end) && scheduleDue(policy.Schedule, start) {
			return start, end, true
		}
	}
	return time.Time{}, time.Time{}, false
}

// policyDue reports whether the policy should start now: it is inside a
// scheduled window and has not already run in that window.
func policyDue(policy *models.PatchPolicy, now time.Time) (start, end time.Time, due bool) {
	start, end, in := inWindow(policy, now)
	if !in {
		return start, end, false
	}
	if policy.LastRunAt != nil && !policy.LastRunAt.Before(start) {
		return start, end, false
	}
	return start, end, true
}
