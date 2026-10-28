package models

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ClientEnrollmentToken is a tenant's enrollment credential (2.3-4c): an agent
// that registers presenting it becomes a unit **of that tenant**.
//
// The reason it exists is the shape of a managed fleet. One installation-wide
// `ENROLLMENT_SECRET` says "you may join this installation", and the operator
// then plays the matching game with the ledger: find the new unit in the
// unassigned pool, move it into the tenant, and hope nobody moved it into
// another one. A per-tenant token makes the tenancy part of the act of joining —
// a technician handed a token for North Grid cannot accidentally enroll a North
// Grid machine into Contoso, and "whose machine is this?" is answered at the
// door rather than in the ledger.
type ClientEnrollmentToken struct {
	ID       string `gorm:"type:uuid;primaryKey" json:"id"`
	ClientID string `gorm:"type:uuid;not null;index" json:"client_id"`

	// Label is the operator's note — "front desk image", "Q4 build". It is
	// the only stable text a revoked token keeps, and the ledger's answer to
	// "which stick of this did the field tech have?".
	Label string `gorm:"not null;default:''" json:"label"`

	// TokenHash is the only stored form of the secret.
	//
	// `models.APIKey` stores its plaintext key beside the hash; this model
	// does not, deliberately. Registration needs a *lookup*, and a hash
	// lookup is all the product requires — so the plaintext is generated,
	// returned once by the create endpoint, and never persisted. A database
	// dump must not hand out credentials that let anyone place a machine
	// inside a customer's tenant.
	TokenHash string `gorm:"uniqueIndex;not null" json:"-"`

	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"`

	// Token is the plaintext, present only on the create response and never
	// stored. `json:"-"` keeps it out of every encoder that walks the struct;
	// the create handler returns it explicitly, as `models.APIKey`'s create
	// does with `key_value`.
	Token string `gorm:"-" json:"-"`
}

// TableName names the table explicitly: the default plural of this name is long
// enough to be typed by hand in a migration, and a migration string that does
// not match the model is a migration that creates a table nobody reads.
func (ClientEnrollmentToken) TableName() string { return "client_enrollment_tokens" }

// BeforeCreate fills the id and the secret.
func (t *ClientEnrollmentToken) BeforeCreate(tx *gorm.DB) (err error) {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	if t.Token == "" {
		t.Token = GenerateEnrollmentToken()
	}
	if t.TokenHash == "" {
		t.TokenHash = HashAPIKey(t.Token)
	}
	return nil
}

// GenerateEnrollmentToken creates a prefixed enrollment token: owt_<32 hex>.
//
// The prefix is not decoration. It is how an operator who finds `owt_…` in a
// pasted shell history knows which secret it is — `owk_` is an API key, which
// reads the installation, and `owt_` is an enrollment token, which places a
// machine inside a tenant.
func GenerateEnrollmentToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "owt_" + uuid.New().String()
	}
	return "owt_" + hex.EncodeToString(b)
}

// IsRevoked reports a revoked token.
func (t *ClientEnrollmentToken) IsRevoked() bool {
	return t.RevokedAt != nil && !t.RevokedAt.IsZero()
}

// IsExpired reports a token whose window has passed.
func (t *ClientEnrollmentToken) IsExpired() bool {
	return t.ExpiresAt != nil && !t.ExpiresAt.IsZero() && time.Now().After(*t.ExpiresAt)
}

// IsActive is the gate registration applies: a revoked or expired token is
// refused with the same answer as a token that never existed.
func (t *ClientEnrollmentToken) IsActive() bool {
	return !t.IsRevoked() && !t.IsExpired()
}
