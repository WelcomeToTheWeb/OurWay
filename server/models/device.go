package models

import (
	"strings"
	"time"
)

// Device represents a managed device in the RMM system.
type Device struct {
	ID           string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name         string    `gorm:"not null;index" json:"name"`
	Hostname     string    `gorm:"not null" json:"hostname"`
	OS           string    `gorm:"not null" json:"os"`
	Arch         string    `gorm:"not null" json:"arch"`
	AgentVersion string    `json:"agent_version"`
	Status       string    `gorm:"not null;default:offline" json:"status"`
	LastSeen     time.Time `json:"last_seen"`
	PublicIP     string    `json:"public_ip"`
	PrivateIP    string    `json:"private_ip"`
	// Tags are free-form labels used to group devices (policy scope, filters,
	// bulk actions). Normalized by NormalizeTags.
	Tags []string `gorm:"type:text;serializer:json" json:"tags"`
	// ClientID is the tenant this device belongs to (2.3-0). NULL is not a
	// missing value: it *is* "unassigned", and it is the only definition of it.
	// It is indexed because every scoped read filters on it, and a 10,000-device
	// fleet must not pay a scan per list (2.3-8).
	ClientID *string `gorm:"type:uuid;index" json:"client_id"`
	// RebootPending is set when a deployment reported that the device
	// needs a reboot to finish patching; cleared when a reboot is sent.
	RebootPending bool `gorm:"not null;default:false" json:"reboot_pending"`
	// DeviceKey is the agent's wire credential. It is never serialized
	// in JSON (C3): any API that exposes it lets the holder connect as
	// the device. It is returned explicitly, exactly once, by
	// POST /api/agent/register.
	// InputCapability and CaptureState are reported by the agent with each
	// metrics sample (2.2-1). Input capability says whether synthetic input can
	// land at all: "granted", "denied" (macOS without the Accessibility grant),
	// "unavailable" (no tooling, or a build without the capability). Capture
	// state says whether frames are being produced: "ok" / "error".
	// "unknown" is what a unit that has never reported shows — never
	// presented as granted.
	InputCapability string `gorm:"not null;default:'unknown'" json:"input_capability"`
	CaptureState    string `gorm:"not null;default:'unknown'" json:"capture_state"`

	DeviceKey string    `gorm:"uniqueIndex;not null" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NormalizeTags lower-cases, trims, validates and de-duplicates tags. Tags
// are 1-32 chars of a-z 0-9 . _ - : and at most 20 per device; invalid
// entries are dropped. The result is never nil (it serializes as []).
func NormalizeTags(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || len(t) > 32 || seen[t] {
			continue
		}
		ok := true
		for _, r := range t {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' || r == ':') {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == 20 {
			break
		}
	}
	return out
}

// HasAnyTag reports whether the device carries at least one of tags.
func (d *Device) HasAnyTag(tags []string) bool {
	for _, want := range tags {
		for _, have := range d.Tags {
			if have == want {
				return true
			}
		}
	}
	return false
}
