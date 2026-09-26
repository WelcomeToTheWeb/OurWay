package patching

import (
	"log"

	"ourway/server/ws"
)

// Rebooter schedules reboots for patched devices.
type Rebooter struct {
	hub *ws.Hub
}

// NewRebooter creates a new reboot manager.
func NewRebooter(hub *ws.Hub) *Rebooter {
	return &Rebooter{hub: hub}
}

// RebootDevice sends a reboot command to a device.
func (r *Rebooter) RebootDevice(deviceKey string) error {
	return r.hub.SendToDevice(deviceKey, "reboot", nil)
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
