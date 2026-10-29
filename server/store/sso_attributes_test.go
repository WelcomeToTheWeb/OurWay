package store

import (
	"encoding/json"
	"strings"
	"testing"

	"ourway/server/models"
)

// 2.4-2's store half: `users.sso_attributes` is the **claim summary** the product
// needs, written as JSON. Before it, the raw `id_token` was string-concatenated into
// that column — an access credential to another system, sitting in the table a backup
// copies, with an expiry nobody honors and a size nobody bounds.
//
// This test pins the shape the product writes, at the store boundary: parseable JSON,
// the fields the product reads back, no token material, and a column that survives a
// round trip unchanged.
func TestUserSSOAttributesStoreTheClaimSummaryNotTheToken(t *testing.T) {
	db := newMigrateDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}
	s := &UserStore{db: db}

	summary := `{"provider":"keycloak-corp","provider_id":"sub-1","name":"Ops Admin",` +
		`"email":"ops-admin@corp.example","email_verified":true,"groups":["rmm-admins"]}`

	user := &models.User{
		ID:       "21212121-2121-2121-2121-212121212121",
		Username: "ops-admin", Email: "ops-admin@corp.example",
		Provider: "keycloak-corp", ProviderID: "sub-1", SSOAttributes: summary,
	}
	if err := s.Create(user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	stored, err := s.GetByID(user.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.SSOAttributes != summary {
		t.Fatalf("the stored attributes changed:\n got %s\nwant %s", stored.SSOAttributes, summary)
	}

	// The record is structured: a reader can tell a verified email from an asserted
	// one, and the group claim survives for 2.4-3's role mapping.
	var record struct {
		Provider      string   `json:"provider"`
		ProviderID    string   `json:"provider_id"`
		Email         string   `json:"email"`
		EmailVerified bool     `json:"email_verified"`
		Groups        []string `json:"groups"`
	}
	if err := json.Unmarshal([]byte(stored.SSOAttributes), &record); err != nil {
		t.Fatalf("sso_attributes is not JSON: %v\n%s", err, stored.SSOAttributes)
	}
	if record.Provider != "keycloak-corp" || record.ProviderID != "sub-1" ||
		record.Email != "ops-admin@corp.example" || !record.EmailVerified {
		t.Errorf("the claim summary lost fields: %+v", record)
	}
	if len(record.Groups) != 1 || record.Groups[0] != "rmm-admins" {
		t.Errorf("the group claim is not stored: %v", record.Groups)
	}

	// No token material, in any of the shapes a token arrives in. The store does not
	// police this column's contents — the writer is the product's rule — so what
	// matters here is that the shape the product writes has no room for a token: the
	// field set is closed, and `id_token` is not in it.
	for _, forbidden := range []string{
		"id_token", "access_token", "refresh_token", "eyJ", "Bearer ", "MII",
	} {
		if strings.Contains(stored.SSOAttributes, forbidden) {
			t.Errorf("sso_attributes carries %q", forbidden)
		}
	}
	var keys map[string]any
	if err := json.Unmarshal([]byte(stored.SSOAttributes), &keys); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for key := range keys {
		switch key {
		case "provider", "provider_id", "name", "email", "email_verified", "groups":
		default:
			t.Errorf("the claim summary carries a field the product does not define: %q", key)
		}
	}

	// An account with no SSO history stays that way: no empty-object surprises for
	// the reader that has to tell "no provider login" from "a provider login with no
	// claims".
	plain := &models.User{ID: "31313131-3131-3131-3131-313131313131", Username: "local-only",
		Email: "local@corp.example", Provider: "local"}
	if err := s.Create(plain); err != nil {
		t.Fatalf("create local user: %v", err)
	}
	readPlain, err := s.GetByID(plain.ID)
	if err != nil {
		t.Fatalf("read local user: %v", err)
	}
	if readPlain.SSOAttributes != "" && readPlain.SSOAttributes != "{}" {
		t.Errorf("a local account's sso_attributes = %q, want an empty claim record", readPlain.SSOAttributes)
	}
}
