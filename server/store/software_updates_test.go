package store

import (
	"testing"

	"ourway/server/models"
)

func newUpdateStore(t *testing.T) *SoftwareUpdateStore {
	t.Helper()
	db := newMigrateDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return &SoftwareUpdateStore{db: db}
}

func TestUpsertDetectedDeduplicates(t *testing.T) {
	s := newUpdateStore(t)
	mk := func(id string) *models.SoftwareUpdate {
		return &models.SoftwareUpdate{ID: id, DeviceID: "d1", Source: "windows-update", Title: "KB1", ExternalID: "ext-1", Severity: "critical"}
	}
	if err := s.UpsertDetected(mk("a")); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertDetected(mk("b")); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListByDevice("d1")
	if len(list) != 1 {
		t.Fatalf("rescan must not duplicate, got %d rows", len(list))
	}
	// An approved update stays approved on rescan.
	if _, err := s.MarkApproved("a"); err != nil {
		t.Fatal(err)
	}
	_ = s.UpsertDetected(mk("c"))
	if got, _ := s.GetByID("a"); got.Status != "approved" {
		t.Fatalf("approved update changed status to %q", got.Status)
	}
}

func TestDeploymentLifecycle(t *testing.T) {
	s := newUpdateStore(t)
	for _, id := range []string{"u1", "u2"} {
		_ = s.Create(&models.SoftwareUpdate{ID: id, DeviceID: "d1", Source: "apt", Title: id, Status: "approved"})
	}
	if err := s.MarkInstalling([]string{"u1", "u2"}, "dep"); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishDeployment("dep", "d1", true, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetByID("u1"); got.Status != "installed" || got.InstalledAt == nil {
		t.Fatalf("want installed, got %+v", got)
	}

	_ = s.Create(&models.SoftwareUpdate{ID: "u3", DeviceID: "d2", Source: "apt", Title: "u3", Status: "approved"})
	_ = s.MarkInstalling([]string{"u3"}, "dep2")
	_ = s.FinishDeployment("dep2", "d2", false, "boom")
	if got, _ := s.GetByID("u3"); got.Status != "failed" || got.ErrorMessage != "boom" {
		t.Fatalf("want failed/boom, got %+v", got)
	}

	_ = s.Create(&models.SoftwareUpdate{ID: "u4", DeviceID: "d3", Source: "apt", Title: "u4", Status: "approved"})
	_ = s.MarkInstalling([]string{"u4"}, "dep3")
	_ = s.RevertToApproved("dep3", "d3")
	if got, _ := s.GetByID("u4"); got.Status != "approved" {
		t.Fatalf("want approved after revert, got %q", got.Status)
	}
}
