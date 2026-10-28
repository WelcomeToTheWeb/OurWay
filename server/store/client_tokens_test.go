package store

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// 2.3-4c — the tenant's enrollment credential, tested in the store.
//
// The claims that matter are the ones the agent-facing path depends on: a token
// is findable **by hash** and by nothing else, an inert token is indistinguishable
// from one that never existed, and a token cannot outlive its tenant.

func mkClient(t *testing.T, st *Store, name string) *models.Client {
	t.Helper()
	c, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: name, Slug: name})
	if err != nil {
		t.Fatalf("create client %s: %v", name, err)
	}
	return c
}

func TestClientTokenRoundTripIsByHash(t *testing.T) {
	st := newClientStore(t)
	client := mkClient(t, st, "North Grid")

	tok := &models.ClientEnrollmentToken{ClientID: client.ID, Label: "front desk image"}
	if err := st.ClientTokens.Create(tok); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if tok.Token == "" || len(tok.Token) < 8 {
		t.Fatalf("the generated secret is empty or trivial: %q", tok.Token)
	}
	if tok.TokenHash == tok.Token {
		t.Fatal("the stored value is the secret, not its hash")
	}

	found, err := st.ClientTokens.GetByHash(models.HashAPIKey(tok.Token))
	if err != nil || found == nil {
		t.Fatalf("GetByHash: %v %+v", err, found)
	}
	if found.ID != tok.ID || found.ClientID != client.ID || found.Label != "front desk image" {
		t.Fatalf("found %+v, want the token just created", found)
	}

	// Nothing else finds a token: a presented value that is not a stored secret
	// answers the same as a token that never existed. (`token_hash` itself is not
	// probed — registration hashes what the agent presents before it looks up, so
	// the stored hash is never a lookup key from the wire.)
	for _, probe := range []string{"", "owt_nope", tok.Token[:12]} {
		got, err := st.ClientTokens.GetByHash(probe)
		if err != nil || got != nil {
			t.Errorf("GetByHash(%q) = %+v, %v; want (nil, nil)", probe, got, err)
		}
	}

	list, err := st.ClientTokens.ListByClient(client.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListByClient = %d (%v), want 1", len(list), err)
	}
	// The list carries no secret, in any column.
	if list[0].TokenHash != tok.TokenHash {
		t.Fatal("the list row is not the stored row")
	}
	if list[0].Token != "" {
		t.Fatal("the list row carries a plaintext secret")
	}
}

func TestClientTokenExpiryAndRevocationAreInert(t *testing.T) {
	st := newClientStore(t)
	client := mkClient(t, st, "Inert Co")

	past := time.Now().UTC().Add(-time.Minute)
	expired := &models.ClientEnrollmentToken{ClientID: client.ID, Label: "yesterday", ExpiresAt: &past}
	if err := st.ClientTokens.Create(expired); err != nil {
		t.Fatalf("create expired token: %v", err)
	}
	if expired.IsActive() {
		t.Fatal("a token past its expiry is active")
	}

	live := &models.ClientEnrollmentToken{ClientID: client.ID, Label: "live"}
	if err := st.ClientTokens.Create(live); err != nil {
		t.Fatalf("create live token: %v", err)
	}
	if !live.IsActive() {
		t.Fatal("a token with no expiry is not active")
	}
	if err := st.ClientTokens.Revoke(client.ID, live.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	after, err := st.ClientTokens.GetByHash(models.HashAPIKey(live.Token))
	if err != nil || after == nil {
		t.Fatalf("revoked token is unreadable: %v %+v", err, after)
	}
	if after.IsActive() {
		t.Error("a revoked token is active")
	}
	if after.RevokedAt == nil || after.RevokedAt.IsZero() {
		t.Error("the revoked row has no revocation time")
	}

	// Revoking twice is not a success: the console must not report a second
	// revocation of a token that was already inert.
	if err := st.ClientTokens.Revoke(client.ID, live.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("second revoke = %v, want record-not-found", err)
	}
}

func TestClientTokenRevokeIsScopedToItsClient(t *testing.T) {
	st := newClientStore(t)
	a := mkClient(t, st, "A Co")
	b := mkClient(t, st, "B Co")

	tok := &models.ClientEnrollmentToken{ClientID: b.ID, Label: "B's"}
	if err := st.ClientTokens.Create(tok); err != nil {
		t.Fatalf("create: %v", err)
	}
	// The id is B's. A caller who passes it under A's client must not be able to
	// reach it — the route is checked against A, and the store is the second lock.
	if err := st.ClientTokens.Revoke(a.ID, tok.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-client revoke = %v, want record-not-found", err)
	}
	live, err := st.ClientTokens.GetByHash(models.HashAPIKey(tok.Token))
	if err != nil || live == nil || !live.IsActive() {
		t.Fatalf("the cross-client revoke changed the token: %v %+v", err, live)
	}
}

func TestClientTokenUseStamp(t *testing.T) {
	st := newClientStore(t)
	client := mkClient(t, st, "Stamped Co")
	tok := &models.ClientEnrollmentToken{ClientID: client.ID, Label: "build"}
	if err := st.ClientTokens.Create(tok); err != nil {
		t.Fatalf("create: %v", err)
	}
	if tok.LastUsedAt != nil {
		t.Fatal("a new token reports a use")
	}
	if err := st.ClientTokens.MarkUsed(tok.ID); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	list, err := st.ClientTokens.ListByClient(client.ID)
	if err != nil || len(list) != 1 || list[0].LastUsedAt == nil {
		t.Fatalf("after MarkUsed, last_used_at is not recorded: %v %+v", err, list)
	}
}

func TestClientDeleteDropsItsEnrollmentTokens(t *testing.T) {
	st := newClientStore(t)
	client := mkClient(t, st, "Gone Co")
	tok := &models.ClientEnrollmentToken{ClientID: client.ID, Label: "was live"}
	if err := st.ClientTokens.Create(tok); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.Clients.Delete(client.ID); err != nil {
		t.Fatalf("delete client: %v", err)
	}
	left, err := st.ClientTokens.ListByClient(client.ID)
	if err != nil || len(left) != 0 {
		t.Fatalf("client deleted, %d enrollment tokens remain (%v)", len(left), err)
	}
	// And the credential is inert: enrollment by hash finds nothing.
	found, err := st.ClientTokens.GetByHash(models.HashAPIKey(tok.Token))
	if err != nil || found != nil {
		t.Fatalf("the deleted client's token is still findable: %+v %v", found, err)
	}
}
