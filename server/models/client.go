package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client is a tenant: one customer's fleet inside an MSP installation
// (2.3-0). `PRODUCT.md` names "many client fleets" as the normal deployment,
// and until this release the only grouping a device had was tags — which scope
// patch policies and nothing about who may see the device.
//
// Assignment is a single nullable column on the device (`Device.ClientID`),
// not a join table: a device belongs to exactly zero or one client. **Zero is
// `client_id IS NULL`**, and that is the whole definition of "unassigned" —
// there is no unassigned row in this table, because a row a technician can
// delete, rename and collide with would be a *real* tenant that is not one.
// The API presents the sentinel (`UnassignedClientID`) in list responses so
// the console has one name for the concept; nothing stores it.
type Client struct {
	ID      string `gorm:"type:uuid;primaryKey" json:"id"`
	Name    string `gorm:"not null" json:"name"`
	// Slug is the URL/filter identity (`?client=<slug>`), unique and
	// normalized like tags: lower-case, trimmed, 1-32 chars of a-z 0-9 . _ - :.
	// Two clients whose names differ only by case or spacing are the same
	// client, and the API says so with 409 (`store.ErrSlugTaken`).
	Slug     string `gorm:"uniqueIndex;not null;size:32" json:"slug"`
	Notes    string `gorm:"type:text;not null;default:''" json:"notes"`
	Disabled bool   `gorm:"not null;default:false" json:"disabled"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UnassignedClientID is the sentinel the API uses for "no client". It is not a
// row: `Device.ClientID IS NULL` is the definition, and every layer that needs
// to name the concept uses this constant so the definition stays single-sourced.
const UnassignedClientID = "unassigned"

// IsUnassignedClientID reports whether an API-facing client id is the
// unassigned sentinel.
func IsUnassignedClientID(id string) bool {
	return strings.EqualFold(strings.TrimSpace(id), UnassignedClientID)
}

// NormalizeSlug turns a client name (or slug) into the canonical slug:
// lower-case, trimmed, spaces to dashes, and stripped of the characters tags
// already forbid. An input that normalizes to nothing yields "" and the caller
// reports it — silently inventing a slug is how two clients end up sharing one.
//
// The rule is `NormalizeTags`'s rule (see `device.go`), applied to one value,
// so the project has one idea of what a label is.
func NormalizeSlug(in string) string {
	s := strings.ToLower(strings.TrimSpace(in))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == ':' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-")
	if len(out) > 32 {
		out = out[:32]
		out = strings.Trim(out, "-")
	}
	return out
}

// NewClientID mints the UUID a new client row carries.
func NewClientID() string { return uuid.NewString() }
