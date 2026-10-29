package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"ourway/server/models"
	"ourway/server/sso/testidp"
	"ourway/server/store"
)

// 2.4-3: the security contract generic OIDC ships with.
//
// The headline is the one the release gate names. `findOrCreateUser` used to fall
// back to "find by email" and stamp the provider onto whatever account it found, so a
// permissive IdP that let anyone assert `ops-admin@corp.example` claimed a local,
// password-authenticated admin. That path is closed: the assertion becomes a pending
// identity, an audit row, and no session.

// createLocalAccount is the pre-existing account the contract must not claim: a
// password-authenticated account with a role, the shape that used to be takeover-able.
func (f *ssoFixture) createLocalAccount(t *testing.T, username, email, role string) *models.User {
	t.Helper()
	password := "Passw0rd-2.4!"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user := &models.User{
		ID: username + "-local-account", Username: username, Email: email,
		PasswordHash: string(hash), Provider: "local", Status: models.UserStatusActive,
	}
	if err := f.st.Users.Create(user); err != nil {
		t.Fatalf("create account: %v", err)
	}
	roleRow, err := f.st.Roles.GetRoleByName(role)
	if err != nil {
		t.Fatalf("role %s: %v", role, err)
	}
	if err := f.st.UserRoles.AssignRole(user.ID, roleRow.ID); err != nil {
		t.Fatalf("assign role: %v", err)
	}
	f.localPasswords[username] = password
	return user
}

// roleNamesOf reads an account's roles.
func (f *ssoFixture) roleNamesOf(t *testing.T, userID string) []string {
	t.Helper()
	roles, err := f.st.UserRoles.GetUserRoles(userID)
	if err != nil {
		t.Fatalf("roles: %v", err)
	}
	out := []string{}
	for _, r := range roles {
		out = append(out, r.Name)
	}
	return out
}

// rowsFor reads ledger rows for an action.
func rowsFor(t *testing.T, f *ssoFixture, action string) []models.AuditLog {
	t.Helper()
	rows, err := f.st.Audit.List(store.AuditFilter{Action: action})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	return rows
}

// findRow returns the first row whose details contain every wanted value.
func findRow(t *testing.T, f *ssoFixture, action string, want map[string]any) *models.AuditLog {
	t.Helper()
	for _, row := range rowsFor(t, f, action) {
		var details map[string]any
		if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
			continue
		}
		match := true
		for key, value := range want {
			if details[key] != value {
				match = false
				break
			}
		}
		if match {
			found := row
			return &found
		}
	}
	return nil
}

// The release's gate test, named in the roadmap: with a permissive fixture IdP,
// asserting the email of an existing **admin** local account produces a pending
// identity and no access — the admin's roles are unchanged, the login is refused, and
// the attempt is audited.
func TestSSOCannotClaimAnExistingAdminAccount(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "contract-admin")
	target := f.createLocalAccount(t, "ops-admin", "ops-admin@corp.example", "admin")
	idp := testidp.New()
	defer idp.Close()
	// Permissive on purpose: no allow-list, no verified-email requirement, the
	// default link policy. This is the IdP shape the contract exists for.
	f.saveOIDCProvider(t, admin, "permissive-corp", idp.Issuer())

	_, status, reason := f.attemptLogin(t, "permissive-corp", idp, testidp.Claims{
		Subject: "idp-subject-1", Email: "ops-admin@corp.example",
		EmailVerified: true, Name: "Ops Admin", Groups: []string{"rmm-admins"},
	})
	if status == http.StatusFound {
		t.Fatal("an IdP claimed a local admin account by asserting its email")
	}
	if reason != "identity_pending" {
		t.Errorf("refusal reason = %q, want identity_pending (status %d)", reason, status)
	}
	if status != http.StatusForbidden {
		t.Errorf("refusal status = %d, want 403: the identity was proven and the contract refused it", status)
	}

	// The admin is untouched: same account, same roles, same password.
	after, err := f.st.Users.GetByID(target.ID)
	if err != nil {
		t.Fatalf("re-read the admin: %v", err)
	}
	if after.Provider == "permissive-corp" {
		t.Error("the provider was stamped onto the existing admin account")
	}
	roles := f.roleNamesOf(t, target.ID)
	if strings.Join(roles, ",") != "admin" {
		t.Errorf("the admin's roles changed: %v", roles)
	}
	if loginUser(t, f.baseURL, "ops-admin", f.localPasswords["ops-admin"]) == "" {
		t.Error("the admin's own password login stopped working")
	}

	// A pending identity exists, with no roles, naming the account it points at.
	pending, err := f.st.Users.FindByProvider("permissive-corp", "idp-subject-1")
	if err != nil {
		t.Fatalf("no pending identity was recorded: %v", err)
	}
	if !pending.IsPending() {
		t.Errorf("the identity is not pending: status %q", pending.Status)
	}
	if pending.PendingLinkUserID != target.ID {
		t.Errorf("the pending identity points at %q, want the account the email belongs to (%q)",
			pending.PendingLinkUserID, target.ID)
	}
	if got := f.roleNamesOf(t, pending.ID); len(got) != 0 {
		t.Errorf("a pending identity was given roles: %v", got)
	}

	// The attempt is in the ledger, naming the rule, the asserted email, and the
	// account it pointed at.
	row := findRow(t, f, models.AuditAuthLoginFailed, map[string]any{
		"reason":         "identity_pending",
		"asserted_email": "ops-admin@corp.example",
		"target_user":    target.ID,
	})
	if row == nil {
		t.Fatal("the takeover attempt wrote no audit row naming the target account")
	}

	// A second attempt with the same identity is not a second identity.
	_, status, reason = f.attemptLogin(t, "permissive-corp", idp, testidp.Claims{
		Subject: "idp-subject-1", Email: "ops-admin@corp.example", EmailVerified: true, Name: "Ops Admin",
	})
	if reason != "account_pending" {
		t.Errorf("second attempt reason = %q, want account_pending (status %d)", reason, status)
	}
	pendingAgain, err := f.st.Users.FindByProvider("permissive-corp", "idp-subject-1")
	if err != nil || pendingAgain.ID != pending.ID {
		t.Error("the same assertion created a second identity record")
	}

	// And the pending identity cannot sign in: no password, no roles, and the login
	// path says why.
	resp, err := f.noRedirect().Post(f.baseURL+"/api/auth/login", "application/json",
		strings.NewReader(`{"username":"`+pending.Username+`","password":"anything"}`))
	if err != nil {
		t.Fatalf("pending login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("a pending identity logged in")
	}
}

// The domain allow-list: an IdP may only assert identities in the domains the
// operator lists.
func TestSSODomainAllowListRefusesAnOutsideEmail(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "domain-admin")
	idp := testidp.New()
	defer idp.Close()

	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oidc", "name": "listed-corp",
		"client_id": "ourway", "client_secret": "sso-fixture-secret",
		"issuer":          idp.Issuer(),
		"allowed_domains": "corp.example,partner.example",
	})
	if code != http.StatusOK {
		t.Fatalf("save = %d\n%s", code, body)
	}

	_, status, reason := f.attemptLogin(t, "listed-corp", idp, testidp.Claims{
		Subject: "outsider", Email: "outsider@evil.example", EmailVerified: true, Name: "Outsider",
	})
	if status == http.StatusFound {
		t.Fatal("an email outside the allow-list signed in")
	}
	if reason != "domain_not_allowed" {
		t.Errorf("reason = %q, want domain_not_allowed (status %d)", reason, status)
	}
	if row := findRow(t, f, models.AuditAuthLoginFailed, map[string]any{"reason": "domain_not_allowed"}); row == nil {
		t.Error("the allow-list refusal wrote no audit row")
	}
	if _, err := f.st.Users.FindByProvider("listed-corp", "outsider"); err == nil {
		t.Error("an identity outside the allow-list was provisioned")
	}

	// Inside the list, the same door works — including the second listed domain.
	f.loginThrough(t, "listed-corp", idp, testidp.Claims{
		Subject: "partner-user", Email: "user@partner.example", EmailVerified: true, Name: "Partner",
	})
	f.loginThrough(t, "listed-corp", idp, testidp.Claims{
		Subject: "corp-user", Email: "user@corp.example", EmailVerified: true, Name: "Corp",
	})
}

// `email_verified` is honored, not parsed and ignored.
func TestSSOUnverifiedEmailIsRefusedWhenRequired(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "verified-admin")
	idp := testidp.New()
	defer idp.Close()

	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oidc", "name": "verified-corp",
		"client_id": "ourway", "client_secret": "sso-fixture-secret",
		"issuer":                 idp.Issuer(),
		"require_verified_email": true,
	})
	if code != http.StatusOK {
		t.Fatalf("save = %d\n%s", code, body)
	}

	_, status, reason := f.attemptLogin(t, "verified-corp", idp, testidp.Claims{
		Subject: "unverified", Email: "unverified@corp.example", EmailVerified: false, Name: "Unverified",
	})
	if status == http.StatusFound {
		t.Fatal("an unverified email signed in through a provider that requires a verified one")
	}
	if reason != "email_unverified" {
		t.Errorf("reason = %q, want email_unverified (status %d)", reason, status)
	}

	f.loginThrough(t, "verified-corp", idp, testidp.Claims{
		Subject: "verified", Email: "verified@corp.example", EmailVerified: true, Name: "Verified",
	})
}

// The `viewer` floor is the default for SSO-provisioned identities, and no role
// mapping means no escalation.
func TestSSOJITFloorIsViewerAndCannotEscalate(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "floor-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "floor-corp", idp.Issuer())

	// The IdP asserts a group claim that says "admin". With no role mapping
	// configured, the assertion buys nothing.
	access := f.loginThrough(t, "floor-corp", idp, testidp.Claims{
		Subject: "floor-user", Email: "floor@corp.example", EmailVerified: true,
		Name: "Floor", Groups: []string{"admins", "rmm-admins"},
	})
	user, err := f.st.Users.FindByProvider("floor-corp", "floor-user")
	if err != nil {
		t.Fatalf("JIT account: %v", err)
	}
	if got := f.roleNamesOf(t, user.ID); strings.Join(got, ",") != "viewer" {
		t.Errorf("a JIT account's roles = %v, want the viewer floor", got)
	}
	// And the session says the same thing: the admin-only door is closed.
	if status, _, _ := f.do(t, http.MethodGet, "/api/sso/providers", access, nil); status != http.StatusForbidden {
		t.Errorf("the JIT session read admin-only providers with %d, want 403", status)
	}
}

// Role mapping is opt-in. Configured, it owns role assignment — including taking the
// role back when the claim stops asserting it.
func TestSSORoleMappingIsOptInAndOwnsRoles(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "mapping-admin")
	idp := testidp.New()
	defer idp.Close()

	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oidc", "name": "mapped-corp",
		"client_id": "ourway", "client_secret": "sso-fixture-secret",
		"issuer":       idp.Issuer(),
		"role_mapping": `{"rmm-admins":"admin","rmm-tech":"technician"}`,
	})
	if code != http.StatusOK {
		t.Fatalf("save = %d\n%s", code, body)
	}

	f.loginThrough(t, "mapped-corp", idp, testidp.Claims{
		Subject: "mapped-user", Email: "mapped@corp.example", EmailVerified: true,
		Name: "Mapped", Groups: []string{"rmm-admins"},
	})
	user, err := f.st.Users.FindByProvider("mapped-corp", "mapped-user")
	if err != nil {
		t.Fatalf("JIT account: %v", err)
	}
	roles := strings.Join(f.roleNamesOf(t, user.ID), ",")
	if !strings.Contains(roles, "admin") {
		t.Fatalf("the mapped group did not grant admin: %v", roles)
	}

	// The group claim changes; the next login owns the roles.
	f.loginThrough(t, "mapped-corp", idp, testidp.Claims{
		Subject: "mapped-user", Email: "mapped@corp.example", EmailVerified: true,
		Name: "Mapped", Groups: []string{"nobody"},
	})
	if got := f.roleNamesOf(t, user.ID); strings.Join(got, ",") != "viewer" {
		t.Errorf("after losing the group the roles are %v, want the viewer floor alone", got)
	}

	// A provider with no mapping never touches roles.
	f.saveOIDCProvider(t, admin, "unmapped-corp", idp.Issuer())
	f.loginThrough(t, "unmapped-corp", idp, testidp.Claims{
		Subject: "mapped-user-2", Email: "mapped2@corp.example", EmailVerified: true,
		Name: "Unmapped", Groups: []string{"rmm-admins"},
	})
	plain, err := f.st.Users.FindByProvider("unmapped-corp", "mapped-user-2")
	if err != nil {
		t.Fatalf("second JIT account: %v", err)
	}
	if got := f.roleNamesOf(t, plain.ID); strings.Join(got, ",") != "viewer" {
		t.Errorf("a provider with no mapping changed roles: %v", got)
	}
}

// `link_policy=link` is the pre-2.4 behaviour, and it exists only because the
// operator chose it for this provider. It links, it grants nothing new, and it is
// audited.
func TestSSOLinkPolicyLinkIsAnExplicitAuditedChoice(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "link-admin")
	target := f.createLocalAccount(t, "legacy-ops", "legacy-ops@corp.example", "technician")
	idp := testidp.New()
	defer idp.Close()

	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oidc", "name": "linking-corp",
		"client_id": "ourway", "client_secret": "sso-fixture-secret",
		"issuer":      idp.Issuer(),
		"link_policy": "link",
	})
	if code != http.StatusOK {
		t.Fatalf("save = %d\n%s", code, body)
	}

	access := f.loginThrough(t, "linking-corp", idp, testidp.Claims{
		Subject: "legacy-subject", Email: "legacy-ops@corp.example", EmailVerified: true,
		Name: "Legacy Ops",
	})
	if access == "" {
		t.Fatal("the explicit link policy did not complete a login")
	}
	after, err := f.st.Users.GetByID(target.ID)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.Provider != "linking-corp" || after.ProviderID != "legacy-subject" {
		t.Errorf("the identity was not stamped onto the account: %+v", after)
	}
	if got := f.roleNamesOf(t, target.ID); strings.Join(got, ",") != "technician" {
		t.Errorf("linking changed the account's roles: %v", got)
	}
	if row := findRow(t, f, models.AuditSSOLink, map[string]any{
		"provider": "linking-corp", "subject": "legacy-subject", "policy": "link",
	}); row == nil {
		t.Error("an explicit link wrote no sso.link audit row")
	}
	// No second account was created for the same person.
	if _, err := f.st.Users.FindByEmail("legacy-ops@corp.example"); err != nil {
		t.Errorf("the link left no account at the asserted email: %v", err)
	}
}

// The admin's decisions, on the record: approve an identity as its own account, or
// merge it into the account its email points at.
func TestSSOPendingIdentityApproveAndMerge(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "decide-admin")
	target := f.createLocalAccount(t, "target-ops", "target-ops@corp.example", "technician")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "decide-corp", idp.Issuer())

	// Two assertions, two accounts, two decisions.
	f.attemptLogin(t, "decide-corp", idp, testidp.Claims{
		Subject: "merge-subject", Email: "target-ops@corp.example", EmailVerified: true, Name: "Target Ops",
	})
	f.attemptLogin(t, "decide-corp", idp, testidp.Claims{
		Subject: "new-person", Email: "brand-new@corp.example", EmailVerified: true, Name: "New Person",
	})
	// The second one is a new email: JIT at the floor, not pending.
	if newPerson, err := f.st.Users.FindByProvider("decide-corp", "new-person"); err != nil {
		t.Fatalf("a new identity should be provisioned at the floor: %v", err)
	} else if newPerson.IsPending() {
		t.Error("a brand-new identity was parked as pending")
	}

	code, body, _ := f.do(t, http.MethodGet, "/api/users/pending", admin, nil)
	if code != http.StatusOK {
		t.Fatalf("pending list = %d\n%s", code, body)
	}
	var listed struct {
		Identities []struct {
			ID              string   `json:"id"`
			Provider        string   `json:"provider"`
			Subject         string   `json:"subject"`
			Email           string   `json:"email"`
			EmailVerified   bool     `json:"email_verified"`
			LinkTargetID    string   `json:"link_target_id"`
			LinkTargetUser  string   `json:"link_target_username"`
			LinkTargetRoles []string `json:"link_target_roles"`
		} `json:"identities"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if len(listed.Identities) != 1 {
		t.Fatalf("the pending list has %d identities, want 1\n%s", len(listed.Identities), body)
	}
	pending := listed.Identities[0]
	if pending.Subject != "merge-subject" || pending.LinkTargetID != target.ID {
		t.Fatalf("the pending list names the wrong identity: %+v", pending)
	}
	if pending.LinkTargetUser != "target-ops" || strings.Join(pending.LinkTargetRoles, ",") != "technician" {
		t.Errorf("the pending list does not name the account it points at: %+v", pending)
	}
	if !pending.EmailVerified {
		t.Error("the pending list does not state the verified-email fact the decision needs")
	}

	// A manager does not see another account's identity arriving.
	manager := f.roleToken(t, "pending-manager", "manager")
	if code, body, _ := f.do(t, http.MethodGet, "/api/users/pending", manager, nil); code != http.StatusForbidden {
		t.Errorf("a manager read the pending list with %d, want 403\n%s", code, body)
	}
	if code, body, _ := f.do(t, http.MethodPost, "/api/users/"+pending.ID+"/merge", manager, map[string]any{}); code != http.StatusForbidden {
		t.Errorf("a manager merged an identity with %d, want 403\n%s", code, body)
	}

	// Merge: the identity is attached to the account it points at, and the pending
	// record goes away.
	code, body, _ = f.do(t, http.MethodPost, "/api/users/"+pending.ID+"/merge", admin, map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("merge = %d\n%s", code, body)
	}
	after, err := f.st.Users.GetByID(target.ID)
	if err != nil {
		t.Fatalf("re-read the target: %v", err)
	}
	if after.Provider != "decide-corp" || after.ProviderID != "merge-subject" {
		t.Errorf("the merge did not attach the identity: %+v", after)
	}
	if got := f.roleNamesOf(t, target.ID); strings.Join(got, ",") != "technician" {
		t.Errorf("a merge changed the account's roles: %v", got)
	}
	if _, err := f.st.Users.GetByID(pending.ID); err == nil {
		t.Error("the pending record survived the merge")
	}
	if row := findRow(t, f, models.AuditUserMerge, map[string]any{
		"identity": pending.ID, "provider": "decide-corp", "subject": "merge-subject",
	}); row == nil || row.TargetID != target.ID {
		t.Error("the merge wrote no audit row naming the account it merged into")
	}

	// And the merged identity now logs in as that account.
	access := f.loginThrough(t, "decide-corp", idp, testidp.Claims{
		Subject: "merge-subject", Email: "target-ops@corp.example", EmailVerified: true, Name: "Target Ops",
	})
	if access == "" {
		t.Fatal("the merged identity could not log in")
	}

	// Approve: a pending identity becomes its own account at the floor. Park one
	// against a third account and approve it.
	third := f.createLocalAccount(t, "third-ops", "third-ops@corp.example", "viewer")
	f.attemptLogin(t, "decide-corp", idp, testidp.Claims{
		Subject: "approve-subject", Email: "third-ops@corp.example", EmailVerified: true, Name: "Third Ops",
	})
	pending2, err := f.st.Users.FindByProvider("decide-corp", "approve-subject")
	if err != nil {
		t.Fatalf("parked identity: %v", err)
	}
	code, body, _ = f.do(t, http.MethodPost, "/api/users/"+pending2.ID+"/approve", admin, map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("approve = %d\n%s", code, body)
	}
	approved, err := f.st.Users.GetByID(pending2.ID)
	if err != nil {
		t.Fatalf("re-read the approved identity: %v", err)
	}
	if approved.IsPending() {
		t.Error("the identity is still pending after approval")
	}
	if got := f.roleNamesOf(t, approved.ID); strings.Join(got, ",") != "viewer" {
		t.Errorf("an approved identity's roles = %v, want the viewer floor", got)
	}
	// Approving does not merge: the account the email belonged to keeps its identity.
	if thirdAfter, err := f.st.Users.GetByID(third.ID); err != nil {
		t.Fatalf("re-read the third account: %v", err)
	} else if thirdAfter.Provider == "decide-corp" {
		t.Error("approving attached the identity to the account its email belonged to")
	}
	// The details shape is the point: the row names the provider, the subject, the
	// email, and the account that was *proposed* as the target.
	if row := findRow(t, f, models.AuditUserApprove, map[string]any{
		"subject":         "approve-subject",
		"provider":        "decide-corp",
		"email":           "third-ops@corp.example",
		"proposed_target": third.ID,
		"granted_role":    "viewer",
	}); row == nil || row.TargetID != pending2.ID {
		t.Error("the approval wrote no audit row naming the identity, the proposal, and the floor role")
	}

	// Approving twice is a conflict, not a second grant.
	code, body, _ = f.do(t, http.MethodPost, "/api/users/"+pending2.ID+"/approve", admin, map[string]any{})
	if code != http.StatusConflict {
		t.Errorf("approving an approved identity = %d, want 409\n%s", code, body)
	}
}
