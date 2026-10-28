package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ourway/server/models"
)

// 2.3-4c — a tenant's enrollment tokens.
//
// The surface is deliberately small, because what it mints is the ability to
// place a machine inside a customer's tenant.
//
//  **Admin-only, reads included.** A token is a credential, not a attribute of
//  the tenant: a manager who can list a tenant's tokens can hand them out, and
//  the ledger's grant model says nothing about who may do that. So the routes
//  require `admin` at the middleware, and the audit rows are the operator's
//  record of who minted what.
//
//  **The secret is returned once and stored as a hash.** The response carries
//  `token_value`; the list carries `id`, `label`, `last_used_at`, `expires_at`,
//  `revoked_at` — and no secret. `models.ClientEnrollmentToken` has no plaintext
//  column, so there is nothing to leak from a dump.
//
//  **Registration accepts it in the header the agent already sends.** The agent
//  has one enrollment field, and the operator pastes one string into it. A
//  client token and the installation's `ENROLLMENT_SECRET` travel in the same
//  `X-Enrollment-Secret` header: the token places the unit in a tenant, the
//  global secret means the pre-2.3 meaning ("join, unassigned"), and anything
//  else answers the 401 the agent already knows. No agent change, no new wire
//  field.
//
//  **Expiry and revocation are enforced at the door, not in the UI.** A revoked
//  or expired token answers the same as a token that never existed.

// maxEnrollmentTokenLifetimeDays bounds an explicit expiry. A token with no
// expiry is allowed (see `noExpiry`), so this caps the number an operator can
// ask for, not the lifetime they can choose.
const maxEnrollmentTokenLifetimeDays = 3650

// enrollmentTokenView is a token as the console reads it: no secret, the dates
// that say whether it works.
type enrollmentTokenView struct {
	ID         string     `json:"id"`
	ClientID   string     `json:"client_id"`
	Label      string     `json:"label"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func tokenView(t models.ClientEnrollmentToken) enrollmentTokenView {
	return enrollmentTokenView{
		ID: t.ID, ClientID: t.ClientID, Label: t.Label,
		LastUsedAt: t.LastUsedAt, ExpiresAt: t.ExpiresAt,
		RevokedAt: t.RevokedAt, CreatedAt: t.CreatedAt,
	}
}

// ListClientTokens returns a tenant's enrollment tokens.
// GET /api/clients/:id/tokens  (admin)
func (h *ClientHandler) ListClientTokens(c *gin.Context) {
	client, ok := h.scopedClient(c)
	if !ok {
		return
	}
	tokens, err := h.store.ClientTokens.ListByClient(client.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read the client's enrollment tokens"})
		return
	}
	views := make([]enrollmentTokenView, 0, len(tokens))
	for _, t := range tokens {
		views = append(views, tokenView(t))
	}
	c.JSON(http.StatusOK, gin.H{"tokens": views, "total": len(views)})
}

// CreateClientToken mints an enrollment token for a tenant.
// POST /api/clients/:id/tokens  {label, expires_in_days}  (admin)
//
// The plaintext is in this response and nowhere else.
func (h *ClientHandler) CreateClientToken(c *gin.Context) {
	client, ok := h.scopedClient(c)
	if !ok {
		return
	}
	var req struct {
		Label         string `json:"label"`
		ExpiresInDays *int   `json:"expires_in_days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	tok := &models.ClientEnrollmentToken{ClientID: client.ID, Label: req.Label}
	if req.ExpiresInDays != nil {
		days := *req.ExpiresInDays
		// Absent means "no expiry"; a number must be a real window. The
		// distinction is deliberate: an image build that runs for two years
		// is a legitimate reason to want a non-expiring token, and it must be
		// a choice the operator made, not the default.
		if days <= 0 || days > maxEnrollmentTokenLifetimeDays {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "expires_in_days must be between 1 and 3650 (omit it for a token that does not expire)",
			})
			return
		}
		until := time.Now().UTC().AddDate(0, 0, days)
		tok.ExpiresAt = &until
	}

	// The record is written before the response, with the token's id and its
	// label — never its value. An audit ledger that contained the secret would
	// turn the ledger into the thing an attacker steals.
	defer auditForAnswer(c, h.store, models.AuditClientTokenCreate, ClientTarget(client.ID), map[string]any{
		"label":      req.Label,
		"expires_at": tok.ExpiresAt,
	})

	if err := h.store.ClientTokens.Create(tok); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create the enrollment token"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"token": tokenView(*tok),
		// The one-time reveal, named as such. `models.APIKey`'s create uses
		// `key_value` for the same purpose.
		"token_value": tok.Token,
	})
}

// RevokeClientToken marks a tenant's enrollment token inert.
// DELETE /api/clients/:id/tokens/:tokenId  (admin)
func (h *ClientHandler) RevokeClientToken(c *gin.Context) {
	client, ok := h.scopedClient(c)
	if !ok {
		return
	}
	id := c.Param("tokenId")
	defer auditForAnswer(c, h.store, models.AuditClientTokenRevoke, ClientTarget(client.ID), map[string]any{
		"token_id": id,
	})
	if err := h.store.ClientTokens.Revoke(client.ID, id); err != nil {
		// A token id that is not this tenant's answers the same as one that
		// does not exist: the ledger's rows name every tenant, and an error
		// naming a foreign token would confirm it exists.
		c.JSON(http.StatusNotFound, gin.H{"error": "enrollment token not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true, "id": id})
}

// enrollmentClientID resolves the tenant an enrollment credential names.
//
// It is the whole of 2.3-4c's registration path: hash what the agent presented,
// look the hash up, and accept the tenant only if the token is live. Not found,
// revoked and expired all answer the same empty result, so the endpoint cannot
// be used to learn which tokens exist.
func (h *DeviceHandler) enrollmentClientID(presented string) (clientID string, tokenID string) {
	if presented == "" {
		return "", ""
	}
	sum := sha256.Sum256([]byte(presented))
	tok, err := h.store.ClientTokens.GetByHash(hex.EncodeToString(sum[:]))
	if err != nil || tok == nil {
		return "", ""
	}
	if !tok.IsActive() {
		return "", ""
	}
	return tok.ClientID, tok.ID
}
