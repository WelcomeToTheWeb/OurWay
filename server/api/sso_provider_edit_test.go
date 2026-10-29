package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"ourway/server/models"
	"ourway/server/sso/testidp"
)

// 2.4-4 · the provider ledger.
//
// The server half is the one the console needs: edit a provider **in place**, so
// "enable" and "order" cannot be done by delete-and-recreate. A provider's name is
// the identity key stored on every account that signed in through it
// (`users.provider`), so recreating a provider under the same name is not a
// replacement an admin can reach, and renaming it is not a label change.

// editProvider sends PUT /api/sso/providers/:id.
func (f *ssoFixture) editProvider(t *testing.T, token, id string, form map[string]any) (int, []byte) {
	t.Helper()
	code, body, _ := f.do(t, http.MethodPut, "/api/sso/providers/"+id, token, form)
	return code, body
}

// providerByID reads the stored row back through the admin list.
func (f *ssoFixture) providerByID(t *testing.T, admin, id string) models.SSOProvider {
	t.Helper()
	code, body, _ := f.do(t, http.MethodGet, "/api/sso/providers", admin, nil)
	if code != http.StatusOK {
		t.Fatalf("provider list = %d\n%s", code, body)
	}
	var listed struct {
		Providers []models.SSOProvider `json:"providers"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	for _, p := range listed.Providers {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no provider row with id %s", id)
	return models.SSOProvider{}
}

// A provider is edited in place: same row, same id, same identity key, with the new
// policy applied. And it still authenticates, which is the proof that an edit that
// sent no client secret kept the one on the row.
func TestProviderEditInPlaceAppliesPolicyAndKeepsTheDoorWorking(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "edit-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "editable-corp", idp.Issuer())
	before := f.providerByID(t, admin, f.providerID(t, admin, "editable-corp"))

	code, body := f.editProvider(t, admin, before.ID, map[string]any{
		"allowed_domains":        "corp.example",
		"require_verified_email": true,
		"role_mapping":           `{"rmm-admins":"admin"}`,
		"claims_groups":          "groups",
		"sort_order":             3,
	})
	if code != http.StatusOK {
		t.Fatalf("edit = %d\n%s", code, body)
	}

	after := f.providerByID(t, admin, before.ID)
	if after.ID != before.ID {
		t.Error("the edit replaced the provider row instead of editing it")
	}
	if after.Name != "editable-corp" {
		t.Errorf("the identity key changed: %q", after.Name)
	}
	if after.AllowedDomains != "corp.example" || !after.RequireVerifiedEmail {
		t.Errorf("the contract policy was not applied: %+v", after)
	}
	if after.RoleMapping != `{"rmm-admins":"admin"}` || after.ClaimsGroups != "groups" {
		t.Errorf("the claim mapping / role mapping was not applied: %+v", after)
	}
	if after.SortOrder != 3 {
		t.Errorf("sort_order = %d, want 3", after.SortOrder)
	}
	// The endpoints stay the discovered ones: an edit is not a re-guess.
	if after.AuthURL == "" || after.JwksURI == "" {
		t.Errorf("the edit lost the discovered endpoints: %+v", after)
	}

	// The door still works, with the new policy in force: an in-domain, verified
	// identity signs in…
	f.loginThrough(t, "editable-corp", idp, testidp.Claims{
		Subject: "worker", Email: "worker@corp.example", EmailVerified: true, Name: "Worker",
	})
	// …and an out-of-domain one is refused by the policy the edit just set, proving
	// the row is live and the client secret survived an edit that sent none.
	_, status, reason := f.attemptLogin(t, "editable-corp", idp, testidp.Claims{
		Subject: "outsider", Email: "outsider@evil.example", EmailVerified: true, Name: "Outsider",
	})
	if status == http.StatusFound || reason != "domain_not_allowed" {
		t.Errorf("the new allow-list is not in force: status %d reason %q", status, reason)
	}
}

// Omitting a field is not clearing it. The console's enable toggle sends one field;
// it must not wipe the domain allow-list the operator set from the API.
func TestProviderEditLeavesUntouchedFieldsAlone(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "toggle-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "toggle-corp", idp.Issuer())
	id := f.providerID(t, admin, "toggle-corp")
	if code, body := f.editProvider(t, admin, id, map[string]any{
		"allowed_domains": "corp.example",
		"role_mapping":    `{"rmm-admins":"admin"}`,
	}); code != http.StatusOK {
		t.Fatalf("policy set = %d\n%s", code, body)
	}

	if code, body := f.editProvider(t, admin, id, map[string]any{"enabled": false}); code != http.StatusOK {
		t.Fatalf("toggle = %d\n%s", code, body)
	}
	after := f.providerByID(t, admin, id)
	if after.Enabled {
		t.Error("the enable toggle did not take")
	}
	if after.AllowedDomains != "corp.example" {
		t.Errorf("an edit that touched only `enabled` cleared the allow-list: %q", after.AllowedDomains)
	}
	if after.RoleMapping != `{"rmm-admins":"admin"}` {
		t.Errorf("an edit that touched only `enabled` cleared the role mapping: %q", after.RoleMapping)
	}

	// An explicit empty value IS a state, and it is applied.
	if code, body := f.editProvider(t, admin, id, map[string]any{"allowed_domains": "", "enabled": true}); code != http.StatusOK {
		t.Fatalf("clear = %d\n%s", code, body)
	}
	if after = f.providerByID(t, admin, id); after.AllowedDomains != "" || !after.Enabled {
		t.Errorf("an explicit empty allow-list was not applied: %+v", after)
	}
}

// The name is the identity key. Editing it is refused, named, and the row is intact.
func TestProviderNameIsImmutable(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "rename-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "rename-corp", idp.Issuer())
	id := f.providerID(t, admin, "rename-corp")

	code, body := f.editProvider(t, admin, id, map[string]any{"name": "rename-corp-v2"})
	if code != http.StatusConflict {
		t.Fatalf("rename = %d, want 409\n%s", code, body)
	}
	if !strings.Contains(string(body), "name_immutable") {
		t.Errorf("the refusal does not name why: %s", body)
	}
	if after := f.providerByID(t, admin, id); after.Name != "rename-corp" {
		t.Errorf("the rename landed: %q", after.Name)
	}
	// The same name in the edit body is not a rename: the console sends the whole
	// form, and it must not fail its own save.
	if code, body := f.editProvider(t, admin, id, map[string]any{"name": "rename-corp", "sort_order": 1}); code != http.StatusOK {
		t.Fatalf("same-name edit = %d\n%s", code, body)
	}
}

// An edit that makes the provider unreachable is refused, and the stored row keeps
// working. The provider the fleet signs in through is never left half-edited.
func TestProviderEditRefusesAnUnreachableIssuerAndChangesNothing(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "guard-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "guarded-corp", idp.Issuer())
	id := f.providerID(t, admin, "guarded-corp")
	before := f.providerByID(t, admin, id)

	// A second IdP that publishes a document the verifier will refuse.
	broken := testidp.New()
	defer broken.Close()
	broken.BreakDocument("issuer")

	code, body := f.editProvider(t, admin, id, map[string]any{"issuer": broken.Issuer()})
	if code != http.StatusBadRequest {
		t.Fatalf("edit with a bad issuer = %d, want 400\n%s", code, body)
	}
	var answer struct {
		Discovery string `json:"discovery"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || answer.Discovery != "issuer_mismatch" {
		t.Errorf("the refusal does not name the discovery kind: %s", body)
	}
	if !strings.Contains(answer.Error, "provider not changed") {
		t.Errorf("the refusal does not say what happened to the row: %q", answer.Error)
	}

	after := f.providerByID(t, admin, id)
	if after.Issuer != before.Issuer || after.AuthURL != before.AuthURL || after.JwksURI != before.JwksURI {
		t.Errorf("the refused edit changed the stored row: %+v", after)
	}
	// The door is open on the row as stored.
	f.loginThrough(t, "guarded-corp", idp, testidp.Claims{
		Subject: "still-works", Email: "works@corp.example", EmailVerified: true, Name: "Works",
	})
}

// The login page's order is the operator's `sort_order`, and enabling a provider is
// a save, not a restart: the public list is what the login page will render.
func TestProviderOrderAndEnableAreTheLoginPagesState(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "order-admin")
	first, second, third := testidp.New(), testidp.New(), testidp.New()
	defer first.Close()
	defer second.Close()
	defer third.Close()

	f.saveOIDCProvider(t, admin, "third-door", third.Issuer())
	f.saveOIDCProvider(t, admin, "first-door", first.Issuer())
	f.saveOIDCProvider(t, admin, "second-door", second.Issuer())
	for name, order := range map[string]int{"third-door": 3, "first-door": 1, "second-door": 2} {
		if code, body := f.editProvider(t, admin, f.providerID(t, admin, name), map[string]any{"sort_order": order}); code != http.StatusOK {
			t.Fatalf("order %s = %d\n%s", name, code, body)
		}
	}
	// Disabled at first: it must be absent from the login page's list.
	if code, body := f.editProvider(t, admin, f.providerID(t, admin, "second-door"), map[string]any{"enabled": false}); code != http.StatusOK {
		t.Fatalf("disable = %d\n%s", code, body)
	}

	names := f.loginPageProviders(t)
	if strings.Join(names, ",") != "first-door,third-door" {
		t.Errorf("login page shows %v, want first-door then third-door (second-door disabled)", names)
	}

	// Enable it: it appears, in its configured place. No restart, no console rebuild.
	if code, body := f.editProvider(t, admin, f.providerID(t, admin, "second-door"), map[string]any{"enabled": true}); code != http.StatusOK {
		t.Fatalf("enable = %d\n%s", code, body)
	}
	if got := strings.Join(f.loginPageProviders(t), ","); got != "first-door,second-door,third-door" {
		t.Errorf("login page shows %v, want the three in sort_order", got)
	}

	// And the public list is not a configuration dump: no endpoints, no secret.
	_, body, _ := f.do(t, http.MethodGet, "/api/auth/sso/providers", "", nil)
	for _, leak := range []string{"token_endpoint", "jwks_uri", "client_secret", "token_url", "\"secret\""} {
		if strings.Contains(string(body), leak) {
			t.Errorf("the public provider list leaks %q: %s", leak, body)
		}
	}
}

// Editing who the installation's accounts sign in through is on the record, and the
// record never carries the client secret.
func TestProviderEditIsAuditedWithoutTheSecret(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "audit-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "audited-corp", idp.Issuer())
	id := f.providerID(t, admin, "audited-corp")

	if code, body := f.editProvider(t, admin, id, map[string]any{
		"link_policy": "link", "client_secret": "rotated-secret", "sort_order": 4,
	}); code != http.StatusOK {
		t.Fatalf("edit = %d\n%s", code, body)
	}
	row := findRow(t, f, models.AuditSSOUpdate, map[string]any{
		"name": "audited-corp", "link_policy": "link", "sort_order": float64(4),
	})
	if row == nil {
		t.Fatal("the provider edit wrote no sso.update row naming the provider")
	}
	if row.TargetID != id {
		t.Errorf("the edit row targets %q, want the provider id", row.TargetID)
	}
	if strings.Contains(row.Details, "rotated-secret") || strings.Contains(row.Details, "client_secret") {
		t.Errorf("the provider edit recorded the client secret: %s", row.Details)
	}
	// The rotated secret is live: the row keeps the provider usable.
	if after := f.providerByID(t, admin, id); after.LinkPolicy != "link" {
		t.Errorf("link_policy = %q, want link", after.LinkPolicy)
	}
}

// Only an admin edits the door. A manager's session is refused on every provider
// route, read included.
func TestProviderEditIsAdminOnly(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "gate-admin")
	manager := f.roleToken(t, "gate-manager", "manager")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "gated-corp", idp.Issuer())
	id := f.providerID(t, admin, "gated-corp")

	for _, call := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/sso/providers"},
		{http.MethodPut, "/api/sso/providers/" + id},
		{http.MethodPost, "/api/sso/providers/" + id + "/test"},
		{http.MethodDelete, "/api/sso/providers/" + id},
		{http.MethodPost, "/api/sso/providers/discover"},
	} {
		if code, body, _ := f.do(t, call.method, call.path, manager, map[string]any{}); code != http.StatusForbidden {
			t.Errorf("a manager called %s %s with %d, want 403\n%s", call.method, call.path, code, body)
		}
	}
	// The refused edit changed nothing.
	if after := f.providerByID(t, admin, id); after.Name != "gated-corp" || !after.Enabled {
		t.Errorf("the refused edit changed the row: %+v", after)
	}
}
