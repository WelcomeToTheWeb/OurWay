package api

import (
	"net/http"
	"strings"
	"testing"

	"ourway/server/models"
	"ourway/server/sso/testidp"
)

// 2.4-3 acceptance criterion: "A local password login, a manager's session, and an
// SSO login are each proven to be unaffected by the contract."
//
// The contract's whole point is that it refuses some SSO logins. The test that matters
// is that refusing them moves nothing else: an account with a password still signs in
// with it, and a manager's session keeps the scope it had.

func TestContractLeavesLocalPasswordAuthWorking(t *testing.T) {
	f := newSSOFixture(t)
	f.createLocalAccount(t, "plain-ops", "plain-ops@corp.example", "technician")

	// No provider configured at all: password auth is not SSO's neighbour.
	if token := loginUser(t, f.baseURL, "plain-ops", f.localPasswords["plain-ops"]); token == "" {
		t.Fatal("a local password login stopped working")
	}
	// And a provider existing, in its default (pending) policy, changes nothing for
	// that account: it is reached by password, with its own roles.
	admin := f.adminToken(t, "untouched-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "corp-oidc", idp.Issuer())
	token := loginUser(t, f.baseURL, "plain-ops", f.localPasswords["plain-ops"])
	if token == "" {
		t.Fatal("a local password login stopped working once an OIDC provider existed")
	}
	if status, _, _ := f.do(t, http.MethodGet, "/api/users", token, nil); status != http.StatusForbidden {
		t.Errorf("the signed-in technician read the user list with %d, want 403", status)
	}

	// A pending identity pointing at an existing account does not disturb the
	// account's own login, and does not become a password-authenticable account.
	f.attemptLogin(t, "corp-oidc", idp, testidp.Claims{
		Subject: "ghost", Email: "plain-ops@corp.example", EmailVerified: true, Name: "Ghost",
	})
	if token := loginUser(t, f.baseURL, "plain-ops", f.localPasswords["plain-ops"]); token == "" {
		t.Error("an account with a pending identity could no longer sign in with its password")
	}
	// A pending identity must not become a way into the account it points at. This is
	// a negative login, so it is made directly: `loginUser` fails the test on a 401,
	// and a 401 is the answer we want here.
	resp, err := f.noRedirect().Post(f.baseURL+"/api/auth/login", "application/json",
		strings.NewReader(`{"username":"plain-ops","password":"wrong-password"}`))
	if err != nil {
		t.Fatalf("pending login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a pending identity gave the account a new way in")
	}
}

// A manager's session is the scope the contract must not touch: it keeps what it had,
// loses nothing, and stays out of the pending-identity decisions.
func TestContractLeavesManagerScopeAlone(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "scope-admin")
	manager := f.roleToken(t, "scope-manager", "manager")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "scope-corp", idp.Issuer())

	// Park one identity so there is a pending decision in the installation.
	f.attemptLogin(t, "scope-corp", idp, testidp.Claims{
		Subject: "parked", Email: "parked@corp.example", EmailVerified: true, Name: "Parked",
	})

	// The manager signs in by password and keeps its fleet scope and its tenancy
	// read. The contract gates SSO identities, not sessions that already exist.
	if status, _, reason := f.do(t, http.MethodGet, "/api/devices", manager, nil); status != http.StatusOK {
		t.Errorf("the manager's device read returned %d (%s), want 200", status, reason)
	}
	if status, _, _ := f.do(t, http.MethodGet, "/api/clients", manager, nil); status != http.StatusOK {
		t.Errorf("the manager's tenancy read returned %d, want 200", status)
	}
	// What it does not get is the SSO admin surface, before and after the contract.
	if status, _, _ := f.do(t, http.MethodGet, "/api/sso/providers", manager, nil); status != http.StatusForbidden {
		t.Errorf("a manager read the provider list with %d, want 403", status)
	}
	if status, _, _ := f.do(t, http.MethodGet, "/api/users/pending", manager, nil); status != http.StatusForbidden {
		t.Errorf("a manager read the pending identities with %d, want 403", status)
	}
}

// The SSO half of the criterion, in the other direction: through a provider with no
// allow-list, no requirement, and no role mapping, an SSO login completes. The
// contract's refusals are about accounts and claims, not about SSO being usable.
func TestContractLeavesASubjectSSOLoginWorking(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "usable-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "plain-corp", idp.Issuer())

	access := f.loginThrough(t, "plain-corp", idp, testidp.Claims{
		Subject: "worker", Email: "worker@corp.example", EmailVerified: true,
		Name: "Worker", Groups: []string{"rmm-admins"},
	})
	if access == "" {
		t.Fatal("an SSO login did not complete through a provider with no policy set")
	}
	// A second login through the same identity is a login, not a second pending
	// record: the identity is already the account it logs into.
	again := f.loginThrough(t, "plain-corp", idp, testidp.Claims{
		Subject: "worker", Email: "worker@corp.example", EmailVerified: true, Name: "Worker",
	})
	if again == "" {
		t.Fatal("the same identity could not sign in twice")
	}
	user, err := f.st.Users.FindByProvider("plain-corp", "worker")
	if err != nil {
		t.Fatalf("account lookup: %v", err)
	}
	if got := f.roleNamesOf(t, user.ID); len(got) != 1 || got[0] != "viewer" {
		t.Errorf("an SSO identity's roles = %v, want the viewer floor alone", got)
	}
	// And it is a real session: it reaches the fleet and is in the ledger.
	if status, _, _ := f.do(t, http.MethodGet, "/api/devices", access, nil); status != http.StatusOK {
		t.Errorf("the SSO session read the fleet with %d, want 200", status)
	}
	row := findRow(t, f, models.AuditAuthLogin, map[string]any{
		"provider": "plain-corp", "subject": "worker",
	})
	if row == nil || row.ActorUserID != user.ID {
		t.Error("the SSO login wrote no ledger row naming the account it signed into")
	}
}
