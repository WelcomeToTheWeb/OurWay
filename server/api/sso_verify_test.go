package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"ourway/server/models"
	"ourway/server/sso/testidp"
	"ourway/server/store"
)

// 2.4-2: an identity claim is trusted only after the token the provider **signed**
// has been checked. Before this task, identity came from the userinfo call alone —
// whoever the bearer token said it was, with nothing checked against anything the
// issuer published, and the raw `id_token` kept in the user row as a credential.

// refusalReasons reads the ledger for the refusals of one provider.
func refusalRows(t *testing.T, f *ssoFixture, provider string) []models.AuditLog {
	t.Helper()
	rows, err := f.st.Audit.List(store.AuditFilter{Action: models.AuditAuthLoginFailed})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	var out []models.AuditLog
	for _, row := range rows {
		if row.TargetID == provider {
			out = append(out, row)
		}
	}
	return out
}

// refusedRow finds the ledger row for a refusal naming a reason.
func refusedRow(t *testing.T, f *ssoFixture, provider, reason string) *models.AuditLog {
	t.Helper()
	for _, row := range refusalRows(t, f, provider) {
		var details map[string]any
		if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
			continue
		}
		if details["reason"] == reason {
			found := row
			return &found
		}
	}
	return nil
}

// The five tokens 2.4-2's acceptance criteria name are refused, each with its own
// named outcome, and none of them produces an account.
func TestSSOCallbackRefusesEveryUnverifiableToken(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "verify-admin")

	cases := []struct {
		sabotage string
		what     string
		kind     string
	}{
		{"expired", "a token whose exp is past", "id_token_verification_failed"},
		{"foreign_aud", "a token minted for another client", "id_token_verification_failed"},
		{"wrong_key", "a token signed by a key the provider does not publish", "id_token_verification_failed"},
		{"none", "an unsigned token (alg none)", "id_token_verification_failed"},
		{"hs256", "an HS256 token against the RSA key set", "id_token_verification_failed"},
		{"missing", "no id_token at all from a provider that published a jwks_uri", "missing_id_token"},
		{"nonce_swap", "a token minted for an earlier login", "nonce_mismatch"},
	}

	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			idp := testidp.New()
			defer idp.Close()
			name := "verify-" + tc.sabotage
			f.saveOIDCProvider(t, admin, name, idp.Issuer())

			email := tc.sabotage + "@corp.example"
			_, status, reason := f.attemptLogin(t, name, idp, testidp.Claims{
				Subject: "sub-" + tc.sabotage, Email: email, EmailVerified: true,
				Name: "Someone", TokenSabotage: tc.sabotage,
			})
			if status == http.StatusFound {
				t.Fatalf("the callback accepted %s", tc.what)
			}
			if status != http.StatusUnauthorized {
				t.Errorf("refusal answered %d, want 401", status)
			}
			if reason != tc.kind {
				t.Errorf("refusal reason = %q, want %q", reason, tc.kind)
			}
			row := refusedRow(t, f, name, tc.kind)
			if row == nil {
				t.Fatalf("the refusal wrote no audit row naming %q", tc.kind)
			}
			if row.Outcome != models.AuditOutcomeRefused {
				t.Errorf("ledger outcome = %q, want refused", row.Outcome)
			}
			var details map[string]any
			if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
				t.Fatalf("ledger details: %v\n%s", err, row.Details)
			}
			if details["asserted_email"] != email {
				t.Errorf("the ledger does not name the asserted email: %v", details)
			}
			// Nothing was provisioned for the asserted identity: a refused login
			// must not leave an account behind.
			if _, err := f.st.Users.FindByEmail(email); err == nil {
				t.Error("a refused login provisioned an account")
			}
		})
	}
}

// userinfo and a verified `id_token` must name the **same** person. A provider that
// asserts one subject to the userinfo call and signs another is not a provider
// OurWay picks an identity from.
func TestSSORequiresUserinfoAndIDTokenToAgreeOnSubject(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "agree-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "agree-corp", idp.Issuer())

	_, status, reason := f.attemptLogin(t, "agree-corp", idp, testidp.Claims{
		Subject:        "userinfo-subject",
		IDTokenSubject: "signed-subject",
		Email:          "disagree@corp.example",
		EmailVerified:  true,
		Name:           "Two People",
		TokenSabotage:  "sub_mismatch",
	})
	if status == http.StatusFound {
		t.Fatal("the callback accepted a userinfo/id_token subject disagreement")
	}
	if reason != "subject_mismatch" {
		t.Errorf("refusal reason = %q, want subject_mismatch", reason)
	}
	row := refusedRow(t, f, "agree-corp", "subject_mismatch")
	if row == nil {
		t.Fatal("the subject disagreement wrote no audit row")
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(row.Details), &details); err != nil {
		t.Fatalf("ledger details: %v", err)
	}
	if details["subject"] != "userinfo-subject" {
		t.Errorf("the ledger does not name the subject userinfo asserted: %v", details)
	}
	if _, err := f.st.Users.FindByProvider("agree-corp", "signed-subject"); err == nil {
		t.Error("the signed subject got an account from a refused login")
	}
	if _, err := f.st.Users.FindByEmail("disagree@corp.example"); err == nil {
		t.Error("the asserted email got an account from a refused login")
	}
}

// PKCE: the authorize redirect carries a challenge, the token request presents the
// verifier it corresponds to, and a provider that demands one is satisfied.
func TestSSOPKCEIsPresentedAndEnforced(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "pkce-admin")
	idp := testidp.New()
	defer idp.Close()
	idp.RequirePKCE()
	f.saveOIDCProvider(t, admin, "pkce-corp", idp.Issuer())

	// The redirect the browser is sent: challenge, method, nonce.
	resp, err := f.noRedirect().Get(f.baseURL + "/api/auth/sso/pkce-corp/authorize")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	q := location.Query()
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("authorize sent code_challenge_method %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("code_challenge") == "" {
		t.Error("authorize sent no code_challenge")
	}
	if q.Get("nonce") == "" {
		t.Error("authorize sent no nonce")
	}

	// The strict provider is the assertion: the exchange only completes if the
	// verifier corresponds to the challenge the code was issued with.
	f.loginThrough(t, "pkce-corp", idp, testidp.Claims{
		Subject: "pkce-subject", Email: "pkce@corp.example", EmailVerified: true, Name: "PKCE User",
	})
	if idp.LastCodeChallenge() == "" {
		t.Error("the login never presented a PKCE challenge")
	}
	if !idp.VerifierAccepted() {
		t.Error("the token endpoint did not accept the code verifier")
	}
}

// A provider that rotates its signing key mid-session keeps logins working: an
// unknown `kid` re-fetches the published set once, and the new key is published.
func TestSSOKeyRotationMidSessionKeepsLoginsWorking(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "rotate-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "rotate-corp", idp.Issuer())

	f.loginThrough(t, "rotate-corp", idp, testidp.Claims{
		Subject: "before-rotation", Email: "before@corp.example", EmailVerified: true, Name: "Before",
	})
	idp.Rotate()
	f.loginThrough(t, "rotate-corp", idp, testidp.Claims{
		Subject: "after-rotation", Email: "after@corp.example", EmailVerified: true, Name: "After",
	})

	for _, subject := range []string{"before-rotation", "after-rotation"} {
		if _, err := f.st.Users.FindByProvider("rotate-corp", subject); err != nil {
			t.Errorf("no account for %s across the rotation (%v)", subject, err)
		}
	}
}

// What a completed login records: the account it arrived as, and the verification
// state — stated, not hidden.
func TestSSOLoginIsAuditedWithTheVerificationState(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "login-audit-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "audited-corp", idp.Issuer())

	f.loginThrough(t, "audited-corp", idp, testidp.Claims{
		Subject: "audited-subject", Email: "audited@corp.example", EmailVerified: true, Name: "Audited",
	})

	rows, err := f.st.Audit.List(store.AuditFilter{Action: models.AuditAuthLogin})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	var found *models.AuditLog
	for _, row := range rows {
		if row.TargetID == "audited-corp" {
			row := row
			found = &row
		}
	}
	if found == nil {
		t.Fatal("an SSO login wrote no auth.login row")
	}
	user, err := f.st.Users.FindByProvider("audited-corp", "audited-subject")
	if err != nil {
		t.Fatalf("JIT account: %v", err)
	}
	if found.ActorUserID != user.ID {
		t.Errorf("the login row names actor %q, want the account the identity arrived as (%q)",
			found.ActorUserID, user.ID)
	}
	if found.ActorRoles != "viewer" {
		t.Errorf("the login row records roles %q, want the viewer floor", found.ActorRoles)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(found.Details), &details); err != nil {
		t.Fatalf("ledger details: %v\n%s", err, found.Details)
	}
	if details["id_token_verified"] != true {
		t.Errorf("the row does not state that the identity was verified against the provider's keys: %v", details)
	}
	if details["email_verified"] != true {
		t.Errorf("the row does not state the verified email: %v", details)
	}
	for _, secret := range []string{`"id_token":`, "eyJ", "access_token\u003d", "client_secret"} {
		if strings.Contains(found.Details, secret) {
			t.Errorf("the login row carries %q", secret)
		}
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(user.SSOAttributes), &summary); err != nil {
		t.Errorf("the account's sso_attributes is not JSON: %v\n%s", err, user.SSOAttributes)
	}
	if strings.Contains(user.SSOAttributes, "eyJ") || strings.Contains(user.SSOAttributes, "id_token") {
		t.Error("the account stores an id_token")
	}
}

// An operator who configures a provider by typing endpoints, with no issuer, has no
// key set to verify against. That is allowed — and the login record says so,
// instead of pretending the identity was verified.
func TestSSOHandTypedProviderStatesThatNothingWasVerified(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "typed-admin")
	idp := testidp.New()
	defer idp.Close()

	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oauth2", "name": "legacy-corp",
		"client_id": "ourway", "client_secret": "sso-fixture-secret",
		"auth_url":      idp.URL("/authorize"),
		"token_url":     idp.URL("/token"),
		"user_info_url": idp.URL("/userinfo"),
		"scope":         "openid profile email",
	})
	if code != http.StatusOK {
		t.Fatalf("save = %d\n%s", code, body)
	}

	f.loginThrough(t, "legacy-corp", idp, testidp.Claims{
		Subject: "legacy-subject", Email: "legacy@corp.example", EmailVerified: true, Name: "Legacy",
	})

	rows, err := f.st.Audit.List(store.AuditFilter{Action: models.AuditAuthLogin})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	var found bool
	for _, row := range rows {
		if row.TargetID != "legacy-corp" {
			continue
		}
		found = true
		if strings.Contains(row.Details, "\"id_token_verified\":true") {
			t.Error("a provider with no issuer is recorded as verified")
		}
	}
	if !found {
		t.Fatal("the hand-typed provider's login wrote no row")
	}
}

// The callback's answer to a refusal is a named reason, not the 500 it used to be —
// and the answer never carries a token.
func TestSSORefusalAnswersANamedReasonAndNoToken(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "named-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "named-corp", idp.Issuer())

	idp.SeedLogin(testidp.Claims{
		Subject: "named-subject", Email: "named@corp.example", EmailVerified: true,
		Name: "Named", RedirectURI: f.baseURL + "/api/auth/sso/named-corp/callback",
		TokenSabotage: "expired",
	})
	resp, err := f.noRedirect().Get(f.baseURL + "/api/auth/sso/named-corp/authorize")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	toIdP, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	resp2, err := f.noRedirect().Get(toIdP.String())
	if err != nil {
		t.Fatalf("idp authorize: %v", err)
	}
	defer resp2.Body.Close()
	back, err := url.Parse(resp2.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse callback: %v", err)
	}
	resp3, err := f.noRedirect().Get(back.String())
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer resp3.Body.Close()
	payload, err := io.ReadAll(resp3.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp3.StatusCode == http.StatusInternalServerError {
		t.Fatalf("a refused identity answered 500: %s", payload)
	}
	var answer struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(payload, &answer); err != nil {
		t.Fatalf("decode: %v\n%s", err, payload)
	}
	if answer.Reason != "id_token_verification_failed" {
		t.Errorf("refusal reason = %q, want id_token_verification_failed (%s)", answer.Reason, payload)
	}
	for _, secret := range []string{`"id_token":`, "eyJ", "access_token", "refresh_token", "sso-fixture-secret"} {
		if strings.Contains(string(payload), secret) {
			t.Errorf("the refusal answer carries %q", secret)
		}
	}
}

// A state value is single-use: a replayed callback with a valid cookie and a
// matching state is refused, because the login's verifier and nonce were consumed by
// the first attempt.
func TestSSOStateIsSingleUse(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "state-admin")
	idp := testidp.New()
	defer idp.Close()
	f.saveOIDCProvider(t, admin, "state-corp", idp.Issuer())

	idp.SeedLogin(testidp.Claims{
		Subject: "state-subject", Email: "state@corp.example", EmailVerified: true,
		Name: "State", RedirectURI: f.baseURL + "/api/auth/sso/state-corp/callback",
	})

	resp, err := f.noRedirect().Get(f.baseURL + "/api/auth/sso/state-corp/authorize")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	toIdP, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	state := toIdP.Query().Get("state")

	resp2, err := f.noRedirect().Get(toIdP.String())
	if err != nil {
		t.Fatalf("idp authorize: %v", err)
	}
	defer resp2.Body.Close()
	back, err := url.Parse(resp2.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse callback: %v", err)
	}
	code := back.Query().Get("code")
	callback := f.baseURL + "/api/auth/sso/state-corp/callback?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)

	if _, err := f.noRedirect().Get(callback); err != nil {
		t.Fatalf("first callback: %v", err)
	}
	replay, err := f.noRedirect().Get(callback)
	if err != nil {
		t.Fatalf("replayed callback: %v", err)
	}
	defer replay.Body.Close()
	if replay.StatusCode == http.StatusFound {
		t.Fatal("a replayed callback completed a login")
	}
	// The cookie is consumed by the first callback (M13), so a replay is caught by
	// the cookie check; the state record itself is single-use too, and either name
	// is an honest answer. What must not happen is a silent refusal.
	if refusedRow(t, f, "state-corp", "state_mismatch") == nil &&
		refusedRow(t, f, "state-corp", "state_expired") == nil {
		t.Error("the replayed callback wrote no audit row naming the state")
	}
}
