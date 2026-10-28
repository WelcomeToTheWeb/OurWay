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

	return &ssoFixture{baseURL: baseURL, st: st, client: &http.Client{Jar: jar}}
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
