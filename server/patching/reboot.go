package patching

import (
	"log"

	"ourway/server/store"
	"ourway/server/ws"
)

// Rebooter schedules reboots for patched devices.
type Rebooter struct {
	hub   *ws.Hub
	store *store.Store
}

// WithStore lets the rebooter clear a device's reboot-pending flag when it
// sends a reboot. Optional.
func (r *Rebooter) WithStore(st *store.Store) *Rebooter {
	r.store = st
	return r
}

func (r *Rebooter) clearPending(deviceKey string) {
	if r.store == nil {
		return
	}
	if dev, err := r.store.Devices.GetByKey(deviceKey); err == nil && dev.RebootPending {
		dev.RebootPending = false
		if err := r.store.Devices.Update(dev); err != nil {
			log.Printf("reboot: failed to clear reboot-pending for %s: %v", dev.Name, err)
		}
	}
}

// NewRebooter creates a new reboot manager.
func NewRebooter(hub *ws.Hub) *Rebooter {
	return &Rebooter{hub: hub}
}

// RebootDevice sends a reboot command to a device.
func (r *Rebooter) RebootDevice(deviceKey string) error {
	if err := r.hub.SendToDevice(deviceKey, "reboot", nil); err != nil {
		return err
	}
	r.clearPending(deviceKey)
	return nil
}

// RebootDevices sends reboot commands to multiple devices.
func (r *Rebooter) RebootDevices(deviceKeys []string) {
	for _, key := range deviceKeys {
		if err := r.RebootDevice(key); err != nil {
			log.Printf("reboot: failed to send reboot to device %s: %v", key, err)
		}
	}
}

// RebootWithDelay schedules a reboot after a delay (seconds).
func (r *Rebooter) RebootWithDelay(deviceKey string, delaySec int) error {
	return r.hub.SendToDevice(deviceKey, "reboot", map[string]interface{}{
		"delay_seconds": delaySec,
	})
}
