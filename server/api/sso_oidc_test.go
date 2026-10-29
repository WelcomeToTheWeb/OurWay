package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"golang.org/x/crypto/bcrypt"
	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/sso/testidp"
	"ourway/server/store"
	"ourway/server/ws"
)

// ssoFixture is the shape 2.4-1's acceptance tests need and the other API fixtures
// do not give: a server whose **own URL is the SSO redirect base**, because the
// round trip ends at `<server>/api/auth/sso/<provider>/callback` and a redirect base
// pointing at a port nothing listens on cannot be walked.
type ssoFixture struct {
	baseURL string
	st      *store.Store
	// client carries cookies: the SSO flow sets `sso_state` on the authorize
	// redirect and the callback refuses a state it cannot match to the cookie.
	client *http.Client
	// localPasswords is the fixture's book of the passwords it set on local
	// accounts, so a test can prove a contract change did not lock anyone out.
	localPasswords map[string]string
}

func newSSOFixture(t *testing.T) *ssoFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	jwtAuth := auth.NewJWTAuth("test-secret-key")
	hub := ws.NewHub("http://localhost:3000", "")
	engine := alerts.NewEngine(st.Alerts)

	// The redirect base must be the URL of the server the routes live on, and the
	// router must be built with that URL. So: reserve the listener first, name its
	// address, build the router around it, and hand the listener to httptest.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	baseURL := "http://" + listener.Addr().String()

	router := SetupRouter(t.Context(), st, jwtAuth, hub, engine, baseURL, "", 2000, 0)
	ts := httptest.NewUnstartedServer(router)
	ts.Listener = listener
	ts.Start()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}

	t.Cleanup(func() {
		ts.Close()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	return &ssoFixture{baseURL: baseURL, st: st, client: &http.Client{Jar: jar}, localPasswords: map[string]string{}}
}

// adminToken creates an administrator and returns its access token.
func (f *ssoFixture) adminToken(t *testing.T, username string) string {
	t.Helper()
	return f.roleToken(t, username, "admin")
}

func (f *ssoFixture) roleToken(t *testing.T, username, role string) string {
	t.Helper()
	password := "Passw0rd-2.4!"
	f.createUserWithRole(t, username, role, password)
	return loginUser(t, f.baseURL, username, password)
}

func (f *ssoFixture) createUserWithRole(t *testing.T, username, role, password string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	user := &models.User{
		ID: uuid.NewString(), Username: username,
		Email: username + "@example.test", PasswordHash: string(hash), Provider: "local",
	}
	if err := f.st.Users.Create(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	roleRow, err := f.st.Roles.GetRoleByName(role)
	if err != nil {
		t.Fatalf("role %s: %v", role, err)
	}
	if err := f.st.UserRoles.AssignRole(user.ID, roleRow.ID); err != nil {
		t.Fatalf("assign role: %v", err)
	}
}

// noRedirect is the browser's behaviour where the test has to read the redirect
// itself: the authorize step, and the IdP's answer, are the things under test.
func (f *ssoFixture) noRedirect() *http.Client {
	return &http.Client{
		Jar:           f.client.Jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (f *ssoFixture) do(t *testing.T, method, path, token string, body any) (int, []byte, http.Header) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	}
	req, err := http.NewRequest(method, f.baseURL+path, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, data, resp.Header
}

// saveProvider POSTs the provider form an operator would fill in.
func (f *ssoFixture) saveProvider(t *testing.T, admin string, form map[string]any) (int, []byte) {
	t.Helper()
	code, body, _ := f.do(t, http.MethodPost, "/api/sso/providers", admin, form)
	return code, body
}

// 2.4-1's headline: Keycloak (here, the fixture IdP) added with **only an issuer
// and a client id/secret** — the endpoints are the ones the provider publishes.
func TestSSOProviderSavedFromIssuerAlone(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "sso-admin")
	idp := testidp.New()
	defer idp.Close()

	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oidc", "name": "keycloak-corp",
		"client_id": "ourway", "client_secret": "sso-fixture-secret",
		"issuer": idp.Issuer(),
	})
	if code != http.StatusOK {
		t.Fatalf("save provider = %d\n%s", code, body)
	}

	var answer struct {
		Provider models.SSOProvider `json:"provider"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	saved := answer.Provider

	for name, got := range map[string]string{
		"auth_url":      saved.AuthURL,
		"token_url":     saved.TokenURL,
		"user_info_url": saved.UserInfoURL,
		"jwks_uri":      saved.JwksURI,
	} {
		if !strings.HasPrefix(got, idp.Issuer()) {
			t.Errorf("%s = %q, want an endpoint published by %s", name, got, idp.Issuer())
		}
	}
	if saved.DiscoveredAt == nil {
		t.Error("the row records no discovery time, so nothing says the endpoints were fetched and not typed")
	}
	if saved.LinkPolicy != models.SSOLinkPolicyPending {
		t.Errorf("link_policy = %q, want the contract's default %q", saved.LinkPolicy, models.SSOLinkPolicyPending)
	}
	if strings.Contains(string(body), "sso-fixture-secret") {
		t.Error("the provider response serialized the client secret")
	}

	// The row on disk is the row the flow will use.
	stored, err := f.st.SSOProviders.GetByName("keycloak-corp")
	if err != nil {
		t.Fatalf("read provider back: %v", err)
	}
	if stored.JwksURI != saved.JwksURI {
		t.Errorf("stored jwks_uri = %q, want %q", stored.JwksURI, saved.JwksURI)
	}
}

// A provider whose issuer answers no usable document is refused **at save time**,
// and nothing is written: half a provider row is a login page with a button that
// cannot work.
func TestSSODiscoveryFailureWritesNoProviderRow(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "sso-admin")
	idp := testidp.New()
	defer idp.Close()
	idp.BreakDocument("no_jwks")

	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oidc", "name": "broken-corp",
		"client_id": "ourway", "client_secret": "x",
		"issuer": idp.Issuer(),
	})
	if code != http.StatusBadRequest {
		t.Fatalf("save = %d, want 400\n%s", code, body)
	}
	var answer struct {
		Error     string `json:"error"`
		Discovery string `json:"discovery"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if answer.Discovery != "discovery_document" {
		t.Errorf("discovery state = %q, want discovery_document (%q)", answer.Discovery, answer.Error)
	}
	if !strings.Contains(answer.Error, "not saved") {
		t.Errorf("error = %q, want it to say the provider was not saved", answer.Error)
	}
	if _, err := f.st.SSOProviders.GetByName("broken-corp"); err == nil {
		t.Fatal("a provider was saved despite a failed discovery")
	}
}

// The provider list the login page reads is public. It carries what to show and
// nothing an attacker wanted.
func TestSSOPublicProviderListCarriesNoSecretAndNoEndpoints(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "sso-admin")
	idp := testidp.New()
	defer idp.Close()

	for i, name := range []string{"alpha-corp", "beta-corp"} {
		code, body := f.saveProvider(t, admin, map[string]any{
			"type": "oidc", "name": name,
			"client_id": "ourway", "client_secret": "leak-canary-" + name,
			"issuer":     idp.Issuer(),
			"sort_order": i,
		})
		if code != http.StatusOK {
			t.Fatalf("save %s = %d\n%s", name, code, body)
		}
	}

	code, body, _ := f.do(t, http.MethodGet, "/api/auth/sso/providers", "", nil)
	if code != http.StatusOK {
		t.Fatalf("public list = %d\n%s", code, body)
	}
	text := string(body)
	for _, secret := range []string{"leak-canary-alpha-corp", "leak-canary-beta-corp"} {
		if strings.Contains(text, secret) {
			t.Errorf("the public list carries %s", secret)
		}
	}
	for _, endpoint := range []string{"/token", "/jwks", "/userinfo"} {
		if strings.Contains(text, endpoint) {
			t.Errorf("the public list carries the provider endpoint %s", endpoint)
		}
	}

	var answer struct {
		Providers []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			SortOrder int    `json:"sort_order"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if len(answer.Providers) != 2 {
		t.Fatalf("public list has %d providers, want 2\n%s", len(answer.Providers), body)
	}
	// The order is the operator's, not the database's: `alpha-corp` was saved
	// second with the lower sort order.
	if answer.Providers[0].Name != "alpha-corp" {
		t.Errorf("first provider = %q, want alpha-corp (sort_order decides)", answer.Providers[0].Name)
	}
}

// Test-connection answers named states, and only to an admin.
func TestSSOTestConnectionReportsNamedStates(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "sso-admin")
	idp := testidp.New()
	defer idp.Close()

	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oidc", "name": "probe-corp",
		"client_id": "ourway", "client_secret": "x",
		"issuer": idp.Issuer(),
	})
	if code != http.StatusOK {
		t.Fatalf("save = %d\n%s", code, body)
	}
	var saved struct {
		Provider struct {
			ID string `json:"id"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}

	states := func() map[string]string {
		code, body, _ := f.do(t, http.MethodPost, "/api/sso/providers/"+saved.Provider.ID+"/test", admin, map[string]any{})
		if code != http.StatusOK {
			t.Fatalf("test = %d\n%s", code, body)
		}
		var answer struct {
			OK     bool `json:"ok"`
			Checks []struct {
				Name  string `json:"name"`
				State string `json:"state"`
			} `json:"checks"`
		}
		if err := json.Unmarshal(body, &answer); err != nil {
			t.Fatalf("decode: %v\n%s", err, body)
		}
		got := map[string]string{}
		for _, c := range answer.Checks {
			got[c.Name] = c.State
		}
		return got
	}

	got := states()
	for _, name := range []string{"discovery", "redirect_uri", "jwks"} {
		if got[name] == "" {
			t.Fatalf("no %q check in the probe: %v", name, got)
		}
	}
	if got["discovery"] != "pass" || got["jwks"] != "pass" {
		t.Errorf("a healthy provider reported %v", got)
	}

	idp.BreakDocument("off")
	got = states()
	if got["discovery"] != "fail" {
		t.Errorf("after the IdP stopped answering, discovery state = %q, want fail", got["discovery"])
	}

	// The probe contacts the IdP on the operator's behalf, so it is an admin
	// surface: a manager of a tenant does not get to probe the installation's
	// identity provider.
	manager := f.roleToken(t, "sso-mgr", "manager")
	code, _, _ = f.do(t, http.MethodPost, "/api/sso/providers/"+saved.Provider.ID+"/test", manager, map[string]any{})
	if code != http.StatusForbidden {
		t.Errorf("manager probe = %d, want 403", code)
	}
}

// The full authorization-code round trip through a provider configured by issuer:
// the console's authorize redirect, the IdP's own redirect back with a code, the
// callback, the one-time `sso_code`, and the session it buys.
func TestSSOLoginRoundTripThroughADiscoveredProvider(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "sso-admin")
	idp := testidp.New()
	defer idp.Close()

	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oidc", "name": "roundtrip-corp",
		"client_id": "ourway", "client_secret": "x",
		"issuer": idp.Issuer(),
	})
	if code != http.StatusOK {
		t.Fatalf("save = %d\n%s", code, body)
	}

	// 1. Ask the console to start the login. It redirects to the IdP's published
	//    authorization endpoint with the state cookie set.
	resp, err := f.noRedirect().Get(f.baseURL + "/api/auth/sso/roundtrip-corp/authorize")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize = %d, want 302\n%s", resp.StatusCode, payload)
	}
	authorizeURL, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	state := authorizeURL.Query().Get("state")
	if state == "" {
		t.Fatal("the authorize redirect carries no state")
	}
	if !strings.HasPrefix(authorizeURL.String(), idp.Issuer()) {
		t.Fatalf("authorize sent the browser to %s, want the IdP at %s", authorizeURL.String(), idp.Issuer())
	}

	// 2. The IdP knows this login and redirects back with the code. Its authorize
	//    handler enforces the redirect URI, the same rule a real IdP enforces — a
	//    provider that was told a different callback answers this request with an
	//    error, not a code.
	redirectURI := f.baseURL + "/api/auth/sso/roundtrip-corp/callback"
	idp.SeedLogin(testidp.Claims{
		Subject: "sso-subject-1", Email: "roundtrip@corp.example",
		EmailVerified: true, Name: "Round Trip", RedirectURI: redirectURI,
	})

	authorizeURL.Query().Set("state", state)
	browserToIdP := authorizeURL.String()
	resp2, err := f.noRedirect().Get(browserToIdP)
	if err != nil {
		t.Fatalf("idp authorize: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusFound {
		t.Fatalf("idp authorize = %d, want 302", resp2.StatusCode)
	}
	back, err := url.Parse(resp2.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse callback location: %v", err)
	}
	realCode := back.Query().Get("code")
	if realCode == "" {
		t.Fatalf("the IdP redirected to %s with no code", back.String())
	}

	// 3. The callback exchanges the code (server to server, over the discovered
	//    token endpoint), verifies the identity, and redirects with a one-time
	//    `sso_code` — never with a token in the URL (C6).
	resp3, err := f.noRedirect().Get(back.String())
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp3.Body)
		t.Fatalf("callback = %d, want 302\n%s", resp3.StatusCode, body)
	}
	landing, err := url.Parse(resp3.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse landing: %v", err)
	}
	loginCode := landing.Query().Get("sso_code")
	if loginCode == "" {
		t.Fatalf("the callback landed on %s with no sso_code", landing.String())
	}
	for _, token := range []string{"access_token", "refresh_token"} {
		if strings.Contains(landing.String(), token) {
			t.Errorf("the callback landed on a URL carrying %s", token)
		}
	}

	// 4. The browser redeems the code for the session.
	code2, session, _ := f.do(t, http.MethodPost, "/api/auth/sso/exchange", "", map[string]string{"code": loginCode})
	if code2 != http.StatusOK {
		t.Fatalf("exchange = %d\n%s", code2, session)
	}
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(session, &tokens); err != nil || tokens.AccessToken == "" {
		t.Fatalf("exchange answered no access token: %v\n%s", err, session)
	}

	// The session is real: it reads the fleet as its own account.
	status, fleet, _ := f.do(t, http.MethodGet, "/api/devices", tokens.AccessToken, nil)
	if status != http.StatusOK {
		t.Fatalf("fleet read with the SSO session = %d\n%s", status, fleet)
	}

	// 5. The JIT account exists, at the floor: **viewer**, with the provider's
	//    subject recorded, and no raw id_token stored.
	user, err := f.st.Users.FindByProvider("roundtrip-corp", "sso-subject-1")
	if err != nil {
		t.Fatalf("find JIT user: %v", err)
	}
	if user.Email != "roundtrip@corp.example" {
		t.Errorf("JIT email = %q", user.Email)
	}
	if strings.Contains(user.SSOAttributes, "id_token") {
		t.Error("the raw id_token is stored on the account")
	}
	roles, err := f.st.UserRoles.GetUserRoles(user.ID)
	if err != nil {
		t.Fatalf("roles: %v", err)
	}
	names := map[string]bool{}
	for _, r := range roles {
		names[r.Name] = true
	}
	if !names["viewer"] {
		t.Errorf("the JIT floor is not viewer: %v", names)
	}
	if names["admin"] {
		t.Error("a JIT user arrived with admin, and 2.4-3's contract says role mapping is opt-in")
	}

	// The code is single-use (C6): a replay buys nothing.
	status, _, _ = f.do(t, http.MethodPost, "/api/auth/sso/exchange", "", map[string]string{"code": loginCode})
	if status == http.StatusOK {
		t.Error("an SSO login code was redeemed twice")
	}
}

// attemptLogin walks the full authorization-code round trip through a saved
// provider: authorize → the IdP's own redirect back with a code → the callback →
// `sso_code` → the exchange. The fixture IdP enforces the redirect URI, so the
// claims are pointed at this provider's callback.
//
// It returns the access token, and — when the callback refused the login — the HTTP
// status and the named reason the callback answered with. A test that expects a
// refusal needs the reason, because the reason is the assertion.
func (f *ssoFixture) attemptLogin(t *testing.T, providerName string, idp *testidp.IdP, claims testidp.Claims) (access string, status int, reason string) {
	t.Helper()
	claims.RedirectURI = f.baseURL + "/api/auth/sso/" + providerName + "/callback"
	idp.SeedLogin(claims)

	resp, err := f.noRedirect().Get(f.baseURL + "/api/auth/sso/" + providerName + "/authorize")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize = %d, want 302\n%s", resp.StatusCode, payload)
	}
	toIdP, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse authorize location: %v", err)
	}
	if toIdP.Query().Get("state") == "" {
		t.Fatal("the authorize redirect carries no state")
	}

	resp2, err := f.noRedirect().Get(toIdP.String())
	if err != nil {
		t.Fatalf("idp authorize: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusFound {
		payload, _ := io.ReadAll(resp2.Body)
		t.Fatalf("idp authorize = %d, want 302\n%s", resp2.StatusCode, payload)
	}
	back, err := url.Parse(resp2.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse callback location: %v", err)
	}
	if e := back.Query().Get("error"); e != "" {
		t.Fatalf("the IdP refused the login with %q (redirect_uri %s)", e, claims.RedirectURI)
	}
	if back.Query().Get("code") == "" {
		t.Fatalf("the IdP redirected to %s with no code", back.String())
	}

	resp3, err := f.noRedirect().Get(back.String())
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusFound {
		payload, _ := io.ReadAll(resp3.Body)
		var refused struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(payload, &refused)
		return "", resp3.StatusCode, refused.Reason
	}
	landing, err := url.Parse(resp3.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse landing: %v", err)
	}
	loginCode := landing.Query().Get("sso_code")
	if loginCode == "" {
		t.Fatalf("the callback landed on %s with no sso_code", landing.String())
	}

	code, body, _ := f.do(t, http.MethodPost, "/api/auth/sso/exchange", "", map[string]string{"code": loginCode})
	if code != http.StatusOK {
		t.Fatalf("exchange = %d\n%s", code, body)
	}
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokens); err != nil || tokens.AccessToken == "" {
		t.Fatalf("exchange answered no access token: %v\n%s", err, body)
	}
	return tokens.AccessToken, http.StatusFound, ""
}

// loginThrough is the round trip with the success asserted: a refusal is a test
// failure, with the reason printed.
func (f *ssoFixture) loginThrough(t *testing.T, providerName string, idp *testidp.IdP, claims testidp.Claims) string {
	t.Helper()
	access, status, reason := f.attemptLogin(t, providerName, idp, claims)
	if status != http.StatusFound {
		t.Fatalf("the callback refused the login with %d (reason %q)", status, reason)
	}
	return access
}

// saveOIDCProvider is the operator's form: a name, a client id/secret, an issuer.
// Nothing else — 2.4-1's premise is that the IdP publishes the rest.
func (f *ssoFixture) saveOIDCProvider(t *testing.T, admin, name, issuer string) {
	t.Helper()
	code, body := f.saveProvider(t, admin, map[string]any{
		"type": "oidc", "name": name,
		"client_id": "ourway", "client_secret": "sso-fixture-secret",
		"issuer": issuer,
	})
	if code != http.StatusOK {
		t.Fatalf("save %s = %d\n%s", name, code, body)
	}
}

// 2.4-1's preview: "show the operator exactly what was discovered before they
// save it". The console calls this with an issuer it has not saved, and shows the
// answer; the row is written only by the save.
func TestSSODiscoveryPreviewShowsWhatTheIssuerPublishes(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "sso-admin")
	idp := testidp.New()
	defer idp.Close()

	code, body, _ := f.do(t, http.MethodPost, "/api/sso/providers/discover", admin, map[string]any{
		"issuer":        idp.Issuer(),
		"provider_name": "preview-corp",
	})
	if code != http.StatusOK {
		t.Fatalf("preview = %d\n%s", code, body)
	}
	var answer struct {
		OK        bool   `json:"ok"`
		Issuer    string `json:"issuer"`
		Discovery string `json:"discovery"`
		Redirect  string `json:"redirect_uri"`
		Endpoints struct {
			Authorization string `json:"authorization"`
			Token         string `json:"token"`
			UserInfo      string `json:"userinfo"`
			Jwks          string `json:"jwks"`
		} `json:"endpoints"`
		SigningAlgs []string `json:"signing_algs"`
		Document    struct {
			Issuer string `json:"issuer"`
			Jwks   string `json:"jwks_uri"`
		} `json:"document"`
		Checks []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if !answer.OK || answer.Discovery != "ok" {
		t.Fatalf("preview answered ok=%v discovery=%q\n%s", answer.OK, answer.Discovery, body)
	}
	if answer.Endpoints.Authorization == "" || answer.Endpoints.Token == "" ||
		answer.Endpoints.Jwks == "" || answer.Endpoints.UserInfo == "" {
		t.Errorf("the preview published no endpoints: %+v", answer.Endpoints)
	}
	for _, endpoint := range []string{
		answer.Endpoints.Authorization, answer.Endpoints.Token,
		answer.Endpoints.UserInfo, answer.Endpoints.Jwks,
	} {
		if !strings.HasPrefix(endpoint, idp.Issuer()) {
			t.Errorf("discovered endpoint %s is not this issuer's", endpoint)
		}
	}
	if answer.Document.Issuer != idp.Issuer() || answer.Document.Jwks == "" {
		t.Errorf("the preview omitted the document the IdP published: %+v", answer.Document)
	}
	// The redirect URI is the one this installation will send, named for the
	// provider being configured: the value the operator pastes into the IdP.
	if !strings.HasSuffix(answer.Redirect, "/api/auth/sso/preview-corp/callback") {
		t.Errorf("redirect URI = %q, want the preview provider's callback", answer.Redirect)
	}
	if len(answer.SigningAlgs) == 0 {
		t.Error("the preview reports no id_token signing algorithms")
	}
	states := map[string]string{}
	for _, ch := range answer.Checks {
		states[ch.Name] = ch.State
	}
	for _, name := range []string{"discovery", "jwks", "pkce", "email_scope", "id_token_alg"} {
		if states[name] != "pass" {
			t.Errorf("check %s = %q, want pass (%s)", name, states[name], body)
		}
	}
	// The fixture's redirect base is a loopback address, and the honest answer
	// about that is a warning: an IdP will not redirect to 127.0.0.1.
	if states["redirect_uri"] != "warn" {
		t.Errorf("check redirect_uri = %q, want warn on a loopback base (%s)", states["redirect_uri"], body)
	}

	// A preview is not a write. The whole point of showing the discovery result
	// first is that nothing is saved from a guess.
	if providers, err := f.st.SSOProviders.ListAll(); err != nil || len(providers) != 0 {
		t.Errorf("a discovery preview wrote %d provider rows, want 0 (%v)", len(providers), err)
	}

	// It is an admin act: probing the installation's identity provider is not a
	// tenant operation.
	manager := f.roleToken(t, "preview-manager", "manager")
	code, body, _ = f.do(t, http.MethodPost, "/api/sso/providers/discover", manager, map[string]any{
		"issuer": idp.Issuer(),
	})
	if code != http.StatusForbidden {
		t.Errorf("manager preview = %d, want 403\n%s", code, body)
	}

	// The act is in the ledger.
	rows, err := f.st.Audit.List(store.AuditFilter{Action: models.AuditSSODiscover})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.Action == models.AuditSSODiscover {
			found = true
		}
	}
	if !found {
		t.Error("a discovery preview wrote no audit row")
	}
}

// The preview answers the same named states the save and the probe answer, so the
// console has one vocabulary for "what did the IdP say", and it never writes a row.
func TestSSODiscoveryPreviewNamesEveryBrokenShape(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "sso-admin")

	type broken struct {
		name     string
		sabotage string
		kind     string
	}
	for _, tc := range []broken{
		{"issuer names another provider", "issuer", "issuer_mismatch"},
		{"no jwks_uri published", "no_jwks", "discovery_document"},
		{"no authorization-code flow", "no_code", "no_code_flow"},
		{"the document is not JSON", "garbage", "discovery_document"},
		{"the endpoint does not answer", "off", "discovery_unreachable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idp := testidp.New()
			defer idp.Close()
			idp.BreakDocument(tc.sabotage)

			code, body, _ := f.do(t, http.MethodPost, "/api/sso/providers/discover", admin, map[string]any{
				"issuer": idp.Issuer(), "provider_name": "broken-preview",
			})
			if code != http.StatusOK {
				t.Fatalf("preview = %d, want 200 with a named state\n%s", code, body)
			}
			var answer struct {
				OK        bool   `json:"ok"`
				Discovery string `json:"discovery"`
				Error     string `json:"error"`
			}
			if err := json.Unmarshal(body, &answer); err != nil {
				t.Fatalf("decode: %v\n%s", err, body)
			}
			if answer.OK {
				t.Errorf("a broken issuer answered ok (%s)", body)
			}
			if answer.Discovery != tc.kind {
				t.Errorf("discovery state = %q, want %q (%q)", answer.Discovery, tc.kind, answer.Error)
			}
			if _, err := f.st.SSOProviders.GetByName("broken-preview"); err == nil {
				t.Error("a failed preview wrote a provider row")
			}
		})
	}

	// An issuer that is not a URL never reaches the network.
	for _, issuer := range []string{"not a url", "ftp://idp.corp", "https://", "https://u:p@idp.corp"} {
		code, body, _ := f.do(t, http.MethodPost, "/api/sso/providers/discover", admin, map[string]any{"issuer": issuer})
		if code != http.StatusBadRequest {
			t.Errorf("preview of %q = %d, want 400\n%s", issuer, code, body)
		}
	}
	code, body, _ := f.do(t, http.MethodPost, "/api/sso/providers/discover", admin, map[string]any{})
	if code != http.StatusBadRequest {
		t.Errorf("preview with no issuer = %d, want 400\n%s", code, body)
	}
}

// 2.4-1's "multiple concurrent providers": two IdPs enabled at once, both
// completing a login, each provisioning its own account.
func TestSSOTwoEnabledProvidersBothCompleteLogins(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "sso-admin")
	idpA := testidp.New()
	defer idpA.Close()
	idpB := testidp.New()
	defer idpB.Close()

	f.saveOIDCProvider(t, admin, "keycloak-a", idpA.Issuer())
	f.saveOIDCProvider(t, admin, "authentik-b", idpB.Issuer())

	code, body, _ := f.do(t, http.MethodGet, "/api/auth/sso/providers", "", nil)
	if code != http.StatusOK {
		t.Fatalf("public list = %d\n%s", code, body)
	}
	var listed struct {
		Providers []struct {
			Name string `json:"name"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if len(listed.Providers) != 2 {
		t.Fatalf("the login page lists %d providers, want 2\n%s", len(listed.Providers), body)
	}

	for _, tc := range []struct {
		provider string
		idp      *testidp.IdP
		subject  string
		email    string
	}{
		{"keycloak-a", idpA, "sub-a", "a@corp.example"},
		{"authentik-b", idpB, "sub-b", "b@corp.example"},
	} {
		access := f.loginThrough(t, tc.provider, tc.idp, testidp.Claims{
			Subject: tc.subject, Email: tc.email, EmailVerified: true, Name: tc.provider + " user",
		})
		status, _, _ := f.do(t, http.MethodGet, "/api/devices", access, nil)
		if status != http.StatusOK {
			t.Errorf("%s: the session cannot read the fleet (%d)", tc.provider, status)
		}
		user, err := f.st.Users.FindByProvider(tc.provider, tc.subject)
		if err != nil {
			t.Errorf("%s: no JIT account for %s (%v)", tc.provider, tc.subject, err)
			continue
		}
		if user.Email != tc.email {
			t.Errorf("%s: JIT email = %q, want %q", tc.provider, user.Email, tc.email)
		}
	}

	// The two accounts are distinct: provider+subject is the identity, and an
	// equal email from a second provider must not collapse onto the first.
	if a, err := f.st.Users.FindByProvider("keycloak-a", "sub-a"); err == nil {
		if b, err := f.st.Users.FindByProvider("authentik-b", "sub-b"); err == nil && a.ID == b.ID {
			t.Error("two providers provisioned one account")
		}
	}
}

// An IdP that signs id_tokens with an algorithm OurWay's verifier does not accept
// (RS256 only, per 2.4-2) is a provider that will hand us tokens we refuse. The
// preview is where the operator hears it: discovery succeeded, and the provider is
// still unusable.
func TestSSODiscoveryPreviewReportsAnUnverifiableSigningAlgorithm(t *testing.T) {
	f := newSSOFixture(t)
	admin := f.adminToken(t, "sso-admin")
	idp := testidp.New()
	defer idp.Close()
	idp.BreakDocument("es256_only")

	code, body, _ := f.do(t, http.MethodPost, "/api/sso/providers/discover", admin, map[string]any{
		"issuer": idp.Issuer(), "provider_name": "ec-corp",
	})
	if code != http.StatusOK {
		t.Fatalf("preview = %d\n%s", code, body)
	}
	var answer struct {
		OK        bool   `json:"ok"`
		Discovery string `json:"discovery"`
		Checks    []struct {
			Name   string `json:"name"`
			State  string `json:"state"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if answer.Discovery != "ok" {
		t.Errorf("discovery state = %q, want ok: the document is conformant", answer.Discovery)
	}
	if answer.OK {
		t.Errorf("a provider whose tokens cannot be verified answered ok (%s)", body)
	}
	var algState, algDetail string
	for _, ch := range answer.Checks {
		if ch.Name == "id_token_alg" {
			algState, algDetail = ch.State, ch.Detail
		}
	}
	if algState != "fail" {
		t.Fatalf("id_token_alg check = %q, want fail (%s)", algState, body)
	}
	if !strings.Contains(algDetail, "ES256") || !strings.Contains(algDetail, "RS256") {
		t.Errorf("id_token_alg detail = %q, want it to name both the provider's algorithm and ours", algDetail)
	}
}
