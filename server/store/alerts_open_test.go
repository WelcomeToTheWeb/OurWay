package store

import (
	"testing"

	"ourway/server/models"
)

// TestAlertOpenDefinition pins the server's half of 2.2-4: "open" is
// **unresolved**, and acknowledging does not close an alert.
//
// The console had three definitions of this count and they disagreed. The fix
// is one definition, and the console's numbers are only as honest as the query
// they come from — so the definition is asserted here as well as in the web
// tests, from the same rows, in the same order.
func TestAlertOpenDefinition(t *testing.T) {
	db := newMigrateDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	alerts := &AlertStore{db: db}

	dev := "11111111-1111-1111-1111-111111111111"
	for _, a := range []*models.Alert{
		{ID: "aaaaaaaa-0000-0000-0000-000000000001", DeviceID: dev, DeviceName: "open", Severity: "critical", Message: "open and untouched", Metric: "cpu", Resolved: false},
		{ID: "aaaaaaaa-0000-0000-0000-000000000002", DeviceID: dev, DeviceName: "acked", Severity: "warning", Message: "open, acknowledged", Metric: "ram", Resolved: false},
		{ID: "aaaaaaaa-0000-0000-0000-000000000003", DeviceID: dev, DeviceName: "closed", Severity: "critical", Message: "resolved", Metric: "disk", Resolved: true},
	} {
		if err := alerts.Create(a); err != nil {
			t.Fatalf("create alert: %v", err)
		}
	}

	open, err := alerts.List(nil, true)
	if err != nil {
		t.Fatalf("list unresolved: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("open alerts = %d, want 2 (the acknowledged one is still open)", len(open))
	}

	// Acknowledging changes who has looked at it, never whether it is open.
	ackedID := "aaaaaaaa-0000-0000-0000-000000000001"
	if err := alerts.Acknowledge(ackedID, "user-1"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	afterAck, err := alerts.List(nil, true)
	if err != nil {
		t.Fatalf("list after acknowledge: %v", err)
	}
	if len(afterAck) != 2 {
		t.Fatalf("open alerts after acknowledge = %d, want 2", len(afterAck))
	}

	// Only resolving closes it.
	if err := alerts.MarkResolved(ackedID); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	afterResolve, err := alerts.List(nil, true)
	if err != nil {
		t.Fatalf("list after resolve: %v", err)
	}
	if len(afterResolve) != 1 {
		t.Fatalf("open alerts after resolve = %d, want 1", len(afterResolve))
	}
}
