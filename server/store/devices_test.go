package store

import (
	"testing"

	"github.com/google/uuid"

	"ourway/server/models"
)

// 2.3-8 — the write path must not be able to blank a unit's credential.
//
// The scale pass is what surfaced this: the Redis-shaped run answers a device
// row with an **empty** `device_key`, because the device cache stores the row as
// JSON and `models.Device.DeviceKey` is `json:"-"` (C3: the key is never
// serialized). A handler that loads a unit and `Save`s the whole row back —
// tags, reboot, client assignment, the WS hub — therefore writes an empty key.
// The first write succeeds and locks the unit's agent out; the next one fails on
// `idx_devices_device_key` (`Key (device_key)=() already exists`), which is the
// operator's 500 on `PUT /api/devices/:id/tags`.
//
// SQLite enforces the same unique index as Postgres here, so this test runs on
// the engine the unit suite uses; the shape is not engine-specific.

// cachedReadRow mimics what a cached read answers: the row, without the key.
func cachedReadRow(t *testing.T, st *Store, id string) *models.Device {
	t.Helper()
	d, err := st.Devices.GetByID(id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	d.DeviceKey = ""
	return d
}

func TestUpdateNeverWritesTheDeviceKey(t *testing.T) {
	st := newClientStore(t)

	mk := func(name string) *models.Device {
		d := &models.Device{
			ID: uuid.NewString(), Name: name, Hostname: name, OS: "linux", Arch: "amd64",
			Status: "online", Tags: []string{}, DeviceKey: "key-" + name,
		}
		if err := st.Devices.Create(d); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return d
	}

	first, second := mk("keyguard-1"), mk("keyguard-2")

	// Enrollment mints the key: Create must still write it.
	if stored, err := st.Devices.GetByKey("key-keyguard-1"); err != nil || stored == nil {
		t.Fatal("Create did not store the device key")
	}

	// The write path a console act takes, twice, on two units — the collision
	// needs two rows to land on the same empty key.
	for i, d := range []*models.Device{first, second} {
		row := cachedReadRow(t, st, d.ID)
		row.Tags = []string{"shift-" + uuid.NewString()[:8]}
		if err := st.Devices.Update(row); err != nil {
			t.Fatalf("update #%d: %v", i+1, err)
		}
		stored, err := st.Devices.GetByID(d.ID)
		if err != nil {
			t.Fatalf("reload after update #%d: %v", i+1, err)
		}
		if stored.DeviceKey != d.DeviceKey {
			t.Errorf("update #%d changed the unit's credential: got %q, want %q", i+1, stored.DeviceKey, d.DeviceKey)
		}
		if len(stored.Tags) != 1 || stored.Tags[0] != row.Tags[0] {
			t.Errorf("update #%d did not persist the tags it was given: %v", i+1, stored.Tags)
		}
	}

	// The columns the write path owns are still written: omitting the key is not
	// omitting the update.
	row := cachedReadRow(t, st, first.ID)
	row.Status = "offline"
	row.RebootPending = true
	if err := st.Devices.Update(row); err != nil {
		t.Fatalf("update status: %v", err)
	}
	stored, err := st.Devices.GetByID(first.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.Status != "offline" || !stored.RebootPending {
		t.Errorf("Update dropped the columns it was asked to write: status %q, reboot %v", stored.Status, stored.RebootPending)
	}
}
