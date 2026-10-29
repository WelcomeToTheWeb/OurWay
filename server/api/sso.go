package api

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/sso"
	"ourway/server/store"
)

// SSOHandler handles SSO authentication and provider management.
type SSOHandler struct {
	store    *store.Store
	jwtAuth  *auth.JWTAuth
	oauth    *sso.OAuthHandler
	redirect string
	// http and jwks are shared by discovery and the test-connection probe: one
	// timeout policy for every call that leaves the building to a machine an
	// operator is configuring.
	http *http.Client
	jwks *sso.JWKSClient

	// pendingCodes holds one-time SSO login codes: after the provider
	// callback, the token pair is parked here for a short TTL and handed
	// to the browser via POST /api/auth/sso/exchange instead of the
	// redirect URL (C6: tokens must not sit in query strings/logs).
	mu      sync.Mutex
	pending map[string]*pendingSSOToken

	// loginStates is the server-side half of the authorize redirect (2.4-2): the
	// PKCE verifier and the nonce the provider will echo back, bound to the state
	// value the browser carries. The cookie proves *this browser* started a login;
	// this map is what proves *which* login, and it is single-use.
	statesMu sync.Mutex
	states   map[string]*loginState

	// replay remembers consumed SAML assertion IDs (2.4-7). An assertion is a
	// bearer artifact that stays valid for its whole window, so "the same assertion
	// twice" is a second login from a captured POST unless the SP remembers what it
	// has already consumed.
	replay *sso.ReplayStore
}

// loginState is what the authorize redirect committed to before the IdP has seen
// anything: the provider, the PKCE verifier, and the nonce the id_token must carry.
type loginState struct {
	provider string
	verifier string
	nonce    string
	// samlRequestID is the AuthnRequest this login sent (2.4-7). The response's
	// InResponseTo must echo it: an assertion that answers no request of ours —
	// or answers a different one — is not this login.
	samlRequestID string
	expires       time.Time
}

// ssoStateTTL is how long an authorize redirect may sit open before its verifier
// and nonce are dead. It matches the `sso_state` cookie's 300 seconds: a login that
// was not completed inside that window is restarted, not resumed later.
const ssoStateTTL = 5 * time.Minute

// storeLoginState records the verifier and nonce for one authorize redirect, and
// sweeps the entries whose window has passed. An unpruned map is a memory leak with
// a login form attached (the M11 lesson), so the sweep rides on the write.
func (h *SSOHandler) storeLoginState(state, provider, verifier, nonce, samlRequestID string) {
	h.statesMu.Lock()
	defer h.statesMu.Unlock()
	now := time.Now()
	for key, st := range h.states {
		if now.After(st.expires) {
			delete(h.states, key)
		}
	}
	h.states[state] = &loginState{
		provider:      provider,
		verifier:      verifier,
		nonce:         nonce,
		samlRequestID: samlRequestID,
		expires:       now.Add(ssoStateTTL),
	}
}

// takeLoginState consumes the record for a state value: a replayed callback has no
// verifier and no nonce to present, which is the point of binding them server-side.
func (h *SSOHandler) takeLoginState(state string) (*loginState, bool) {
	h.statesMu.Lock()
	defer h.statesMu.Unlock()
	st, ok := h.states[state]
	if !ok {
		return nil, false
	}
	delete(h.states, state)
	if time.Now().After(st.expires) {
		return nil, false
	}
	return st, true
}

// pendingSSOToken is a one-time token pair awaiting exchange.
type pendingSSOToken struct {
	access  string
	refresh string
	expires time.Time
}

// ssoCodeTTL is how long an un-exchanged SSO login code stays valid.
const ssoCodeTTL = 60 * time.Second

// NewSSOHandler creates a new SSO handler.
func NewSSOHandler(store *store.Store, jwtAuth *auth.JWTAuth, redirectURI string) *SSOHandler {
	return &SSOHandler{
		store:    store,
		jwtAuth:  jwtAuth,
		oauth:    sso.NewOAuthHandler(store, jwtAuth),
		redirect: redirectURI,
		http:     sso.DefaultHTTPClient(),
		jwks:     sso.NewJWKSClient(sso.DefaultHTTPClient()),
		pending:  make(map[string]*pendingSSOToken),
		states:   make(map[string]*loginState),
		replay:   sso.NewReplayStore(),
	}
}

// ListProviders returns the configured SSO providers for the login page.
func (h *SSOHandler) ListProviders(c *gin.Context) {
	providers, err := h.store.SSOProviders.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list providers"})
		return
	}

	// The login page's list: what to show, in what order, and nothing else. No
	// issuer, no endpoints, no secret — a provider row is a machine the console
	// talks to, and the login page is public.
	type providerInfo struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Protocol    string `json:"protocol"`
		Name        string `json:"name"`
		Label       string `json:"label"`
		SortOrder   int    `json:"sort_order"`
		RedirectURI string `json:"redirect_uri,omitempty"`
	}

	var enabled []providerInfo
	for i := range providers {
		p := providers[i]
		if !p.Enabled {
			continue
		}
		// The login page shows the person a *place*, not a machine identifier: an
		// OIDC issuer reads as "your company's login server", and a SAML entity ID
		// reads as a urn nobody can check. So SAML providers show the name the
		// operator chose, and the protocol says which door the button is.
		label := strings.TrimSpace(p.Issuer)
		if p.IsSAML() {
			label = strings.TrimSpace(p.Name)
		}
		enabled = append(enabled, providerInfo{
			ID: p.ID, Type: p.Type, Protocol: providerProtocol(&p), Name: p.Name,
			Label: label, SortOrder: p.SortOrder,
			RedirectURI: p.RedirectURI,
		})
	}

	c.JSON(http.StatusOK, gin.H{"providers": enabled})
}

// Authorize redirects the user to the SSO provider's authorization page.
func (h *SSOHandler) Authorize(c *gin.Context) {
	providerName := c.Param("provider")

	provider, err := h.store.SSOProviders.GetByName(providerName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	if !provider.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider not available", "reason": "provider_disabled"})
		return
	}

	// 2.4-7: a SAML provider starts its login with a deflated, signed AuthnRequest
	// to the IdP's SSO location, and the browser comes back to the assertion
	// consumer service. The state cookie works the same way as in the OIDC flow, and
	// the server-side state record carries the request ID the response must echo.
	if provider.IsSAML() {
		h.authorizeSAML(c, provider)
		return
	}

	// The rest of this handler is the OAuth2/OIDC flow.
	if t := strings.ToLower(provider.Type); t != "oauth2" && t != "oauth" && t != "oidc" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported SSO type: only oidc and saml are implemented"})
		return
	}

	// Generate state for CSRF protection
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		log.Printf("sso: failed to generate state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	state := hex.EncodeToString(stateBytes)

	// 2.4-2: the login's secrets are committed **server-side**, bound to that
	// state. A provider with an issuer is an OIDC provider, so its `id_token` is
	// verified — and a verified token is only worth having if it is *this* login's,
	// which is what the nonce says. PKCE is sent when the provider published a
	// code-challenge method (discovered at save time, so a login costs no extra
	// round trip to find that out).
	var nonce, verifier, challenge string
	if strings.TrimSpace(provider.Issuer) != "" && strings.TrimSpace(provider.JwksURI) != "" {
		nonce, err = sso.RandomNonce()
		if err != nil {
			log.Printf("sso: failed to generate nonce: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
	}
	if provider.PKCESupported {
		verifier, err = sso.NewCodeVerifier()
		if err != nil {
			log.Printf("sso: failed to generate code verifier: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		challenge = sso.CodeChallenge(verifier)
	}
	h.storeLoginState(state, provider.Name, verifier, nonce, "")

	// Store state in session cookie. Secure=true: this is a CSRF state
	// cookie on a flow that must not be replayable over plaintext HTTP.
	c.SetCookie("sso_state", state, 300, "/", "", true, true)

	redirectURI := h.redirectURIFor(provider)
	authorizeURL := h.oauth.AuthorizeURL(provider, redirectURI, state, nonce, challenge)

	c.Redirect(http.StatusFound, authorizeURL)
}

// Callback handles the SSO provider's redirect back to us. It exchanges
// the provider code for a token pair, parks it under a short-lived one-time
// code, and redirects the browser to /login?sso_code=... — the tokens
// themselves never appear in a URL (C6). The login page exchanges the code
// via POST /api/auth/sso/exchange.
func (h *SSOHandler) Callback(c *gin.Context) {
	providerName := c.Param("provider")

	provider, err := h.store.SSOProviders.GetByName(providerName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	// A SAML provider's browser lands on the assertion consumer service, not here.
	if provider.IsSAML() {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "a SAML provider delivers its assertion to /api/auth/sso/saml/" + provider.Name + "/acs",
			"reason": "saml_uses_acs",
		})
		return
	}
	if t := strings.ToLower(provider.Type); t != "oauth2" && t != "oauth" && t != "oidc" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported SSO type: only oidc and saml are implemented"})
		return
	}

	// Verify state, then consume the cookie so it cannot be replayed (M13).
	expectedState, err := c.Cookie("sso_state")
	if err != nil || expectedState != c.Query("state") {
		h.auditSSORefusal(c, provider, http.StatusBadRequest, ssoRefusal{Reason: sso.ReasonStateMismatch})
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state", "reason": sso.ReasonStateMismatch})
		return
	}
	c.SetCookie("sso_state", "", -1, "/", "", true, true)

	// The state's server-side half is consumed here: the PKCE verifier and the
	// nonce live only on the login they were generated for, and a callback that
	// presents no record is a replay of a state value, not a login in progress.
	login, ok := h.takeLoginState(expectedState)
	if !ok || login.provider != provider.Name {
		h.auditSSORefusal(c, provider, http.StatusBadRequest, ssoRefusal{Reason: sso.ReasonStateExpired})
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state", "reason": sso.ReasonStateExpired})
		return
	}

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing authorization code"})
		return
	}

	redirectURI := h.redirectURIFor(provider)
	outcome, err := h.oauth.HandleCallback(c.Request.Context(), provider, code, redirectURI, login.nonce, login.verifier)
	if err != nil {
		refusal := ssoRefusal{Reason: sso.ReasonCodeExchange}
		var iderr *sso.IdentityError
		if errors.As(err, &iderr) {
			refusal.Reason = iderr.Reason
			refusal.Subject = iderr.Subject
			refusal.Email = iderr.Email
			refusal.Target = iderr.TargetUserID
			refusal.Pending = iderr.PendingUserID
		}
		log.Printf("sso: callback refused for provider %s: %v", providerName, err)
		// A refused identity is a named answer and a ledger row, never a 500: the
		// person standing at the browser wants to know the login was refused, and
		// the operator wants to know which rule refused it. The status separates the
		// two kinds: 401 "the identity was not proven", 403 "the identity was proven
		// and the contract says it may not arrive here".
		status := refusalStatus(refusal.Reason)
		h.auditSSORefusal(c, provider, status, refusal)
		c.JSON(status, gin.H{"error": "SSO login refused", "reason": refusal.Reason})
		return
	}

	h.auditSSOLogin(c, outcome, provider)

	loginCode, err := h.storePendingToken(outcome.AccessToken, outcome.RefreshToken)
	if err != nil {
		log.Printf("sso: failed to store pending token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.Redirect(http.StatusFound, h.redirect+"/login?sso_code="+loginCode)
}

// Exchange redeems a one-time SSO login code for the token pair (C6).
// POST /api/auth/sso/exchange
func (h *SSOHandler) Exchange(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	tok, ok := h.takePendingToken(req.Code)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired SSO code"})
		return
	}

	// The refresh token keeps the SSO session alive; without it the session
	// dies when the access token expires.
	c.JSON(http.StatusOK, gin.H{
		"access_token":  tok.access,
		"refresh_token": tok.refresh,
	})
}

// storePendingToken parks a token pair under a fresh one-time code and
// returns the code. Expired entries are swept opportunistically.
func (h *SSOHandler) storePendingToken(access, refresh string) (string, error) {
	codeBytes := make([]byte, 16)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", err
	}
	code := hex.EncodeToString(codeBytes)

	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	for k, v := range h.pending {
		if now.After(v.expires) {
			delete(h.pending, k)
		}
	}
	h.pending[code] = &pendingSSOToken{
		access:  access,
		refresh: refresh,
		expires: now.Add(ssoCodeTTL),
	}
	return code, nil
}

// takePendingToken atomically removes and returns a pending token pair.
// A missing or expired code reports ok=false (one-time use, C6).
func (h *SSOHandler) takePendingToken(code string) (*pendingSSOToken, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	tok, ok := h.pending[code]
	if !ok {
		return nil, false
	}
	delete(h.pending, code)
	if time.Now().After(tok.expires) {
		return nil, false
	}
	return tok, true
}

// providerPreset returns the issuer and scope for the three providers that ship
// as presets of the generic OIDC path (2.4-1).
//
// There is one code path. The presets do not name endpoints — they name an
// **issuer**, and the endpoints come from that issuer's own discovery document,
// exactly as they do for Keycloak. The pre-2.4 shape (a `switch` on provider name
// with six hardcoded URLs per name) went stale the day Google moved an endpoint,
// and it made every other IdP a support ticket.
func providerPreset(name string) (issuer, scope string) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "google":
		return "https://accounts.google.com", "openid profile email"
	case "microsoft":
		// The `/common` tenant is what lets a work AND a personal account sign in,
		// which is what the preset is for. A single-tenant deployment names its
		// tenant in the issuer field and is no longer a preset at all.
		return "https://login.microsoftonline.com/common/v2.0", "openid profile email offline_access"
	case "apple":
		return "https://appleid.apple.com", "openid"
	default:
		return "", ""
	}
}

// applyDiscovery fills the provider row from the issuer's published configuration.
//
// Precedence, most to least specific: **what the operator typed**, then **what the
// IdP published**, then **the preset**. An explicit endpoint is an operator
// overriding a proxy or a private alias; discovery beats a preset because the
// preset is a guess about a machine nobody is asking.
//
// Discovery failure is an error, and the caller saves nothing on error. A provider
// row that was saved from a guess is a login that fails with a 500 at 2 a.m.
func (h *SSOHandler) applyDiscovery(provider *models.SSOProvider, requestedAuthURL, requestedTokenURL, requestedUserInfoURL string) error {
	if provider.IsSAML() {
		// A SAML provider has no discovery document: its endpoints are the IdP's
		// entity ID and SSO location, which `validateSAMLProvider` checked. Running
		// the OIDC discovery path here would fetch an issuer a SAML row does not
		// have and refuse a provider that is correctly configured.
		return nil
	}
	// The precedence, in the one place it is decided.
	set := func(target *string, discovered, requestedValue string) {
		if strings.TrimSpace(requestedValue) != "" {
			*target = requestedValue
			return
		}
		if strings.TrimSpace(discovered) != "" {
			*target = discovered
		}
	}

	if strings.TrimSpace(provider.Issuer) == "" {
		// No issuer means no discovery, which is the pre-2.4 shape: an operator
		// who typed every endpoint by hand. The typed endpoints are still what the
		// flow calls — dropping them here would save a provider that authorizes to
		// an empty URL.
		set(&provider.AuthURL, "", requestedAuthURL)
		set(&provider.TokenURL, "", requestedTokenURL)
		set(&provider.UserInfoURL, "", requestedUserInfoURL)
		provider.PKCESupported = false
		return nil
	}
	doc, err := sso.Discover(h.http, provider.Issuer, provider.DiscoveryURL)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	provider.DiscoveredAt = &now
	provider.DiscoveryDoc = doc.Raw
	if provider.DiscoveryURL == "" {
		provider.DiscoveryURL = strings.TrimRight(provider.Issuer, "/") + "/.well-known/openid-configuration"
	}
	provider.JwksURI = doc.JwksURI

	set(&provider.AuthURL, doc.AuthorizationEndpoint, requestedAuthURL)
	set(&provider.TokenURL, doc.TokenEndpoint, requestedTokenURL)
	set(&provider.UserInfoURL, doc.UserInfoEndpoint, requestedUserInfoURL)
	// 2.4-2: the code flow is PKCE when the IdP says it understands it. Recorded
	// here, at save time, because the authorize redirect cannot ask the IdP.
	provider.PKCESupported = doc.PKCESupported()
	return nil
}

// TestProvider reports whether a saved provider is reachable and consistent:
// discovery, the JWKS endpoint, and the redirect URI the flow will send.
//
// Every answer is a named state (2.4-1's acceptance criterion), because the
// operator standing here has an IdP admin on the other end of a phone call, and
// "SSO login failed" is not a thing you can act on.
func (h *SSOHandler) TestProvider(c *gin.Context) {
	id := c.Param("id")
	provider, err := h.store.SSOProviders.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	type check struct {
		Name   string `json:"name"`
		State  string `json:"state"`
		Detail string `json:"detail,omitempty"`
	}
	checks := []check{}
	ok := true
	add := func(name, state, detail string) {
		checks = append(checks, check{Name: name, State: state, Detail: detail})
		if state == "fail" {
			ok = false
		}
	}

	if provider.IsSAML() {
		// A SAML provider has no discovery document and no token endpoint, so the
		// probe asks the questions SAML can answer: does the pinned certificate
		// parse and does it still have a validity window, does the ACS URL match
		// what the assertion will be addressed to, and does the metadata this
		// installation serves parse. A certificate that expired is the failure an
		// operator would otherwise discover as a wave of refused logins.
		add("protocol", "pass", "saml")
		cert, certErr := parseProviderCertificate(provider.IdpCertificate)
		if certErr != nil {
			add("idp_certificate", "fail", certErr.Error())
		} else {
			fingerprint := sso.CertificateFingerprint(provider.IdpCertificate)
			if cert.NotAfter.Before(time.Now()) {
				add("idp_certificate", "fail", "expired at "+cert.NotAfter.UTC().Format(time.RFC3339)+" ("+fingerprint+")")
			} else {
				days := int(time.Until(cert.NotAfter).Hours() / 24)
				state := "pass"
				if days < 14 {
					state = "warn"
				}
				add("idp_certificate", state, fmt.Sprintf("%s, expires %s (%d days)", fingerprint, cert.NotAfter.UTC().Format(time.RFC3339), days))
			}
		}
		if problem := samlEndpointIsConfigurable(provider.IdpSSOURL); problem != "" {
			add("idp_sso_url", "fail", problem)
		} else {
			add("idp_sso_url", "pass", provider.IdpSSOURL)
		}
		add("acs_url", "pass", h.acsURLFor(provider))
		add("sp_entity_id", "pass", provider.SPEntityID)
		if doc, docErr := sso.SPMetadata(h.samlConfig(provider)); docErr != nil {
			add("metadata", "fail", docErr.Error())
		} else {
			add("metadata", "pass", fmt.Sprintf("GET /api/auth/sso/saml/%s/metadata (%d bytes)", provider.Name, len(doc)))
		}
		if provider.SPKeyPEM != "" {
			add("signed_requests", "pass", "the SP signs its AuthnRequests")
		} else {
			add("signed_requests", "warn", "the SP sends unsigned AuthnRequests: an IdP that requires signed requests will refuse logins")
		}
		add("response_signature", map[bool]string{true: "pass", false: "warn"}[provider.RequireResponseSignature],
			"the response signature is required by this provider")
		if provider.RequireVerifiedEmail && strings.TrimSpace(provider.VerifiedEmailAttribute) == "" {
			add("verified_email", "fail", "required, and no attribute is configured to state it")
		}
	}

	if strings.TrimSpace(provider.Issuer) == "" {
		add("discovery", "skipped", "no issuer configured: this provider uses the endpoints typed into it")
	} else {
		doc, err := sso.Discover(h.http, provider.Issuer, provider.DiscoveryURL)
		if err != nil {
			kind := sso.DiscoveryUnreachable
			var de *sso.DiscoveryError
			if errors.As(err, &de) {
				kind = de.Kind
			}
			add("discovery", "fail", kind+": "+err.Error())
		} else {
			add("discovery", "pass", doc.Issuer)
			if !doc.SupportsSigningAlg("RS256") {
				// The verifier is RS256-only (2.4-2): a provider that signs with
				// something else hands us tokens we refuse, and the operator should
				// hear that here rather than from a failed login.
				add("id_token_alg", "fail", "the provider signs id_tokens with "+
					strings.Join(doc.IDTokenSigningAlgs, ", ")+"; OurWay verifies RS256")
			}
			if !doc.PKCESupported() {
				add("pkce", "warn", "the provider publishes no code-challenge method")
			}
			if !doc.ScopeSupports("email") {
				add("email_scope", "warn", "the provider does not list the email scope")
			}
		}
	}

	// The redirect URI is the one the flow will send. A mismatch with what the
	// IdP has registered for this client is the single most common reason an IdP
	// answers "invalid_redirect_uri" with no hint of which value it wanted.
	redirectURI := h.redirectURIFor(provider)
	if strings.Contains(redirectURI, "://localhost") || strings.Contains(redirectURI, "://127.0.0.1") {
		add("redirect_uri", "warn", "redirect URI is a loopback address: "+redirectURI)
	} else {
		add("redirect_uri", "pass", redirectURI)
	}

	if strings.TrimSpace(provider.JwksURI) == "" {
		add("jwks", "skipped", "no jwks_uri: discovery has not populated this provider")
	} else if err := h.jwks.Probe(provider.JwksURI); err != nil {
		add("jwks", "fail", err.Error())
	} else {
		add("jwks", "pass", provider.JwksURI)
	}
	defer auditForAnswer(c, h.store, models.AuditSSOTest, AuditTarget{Type: "sso-provider", ID: provider.ID}, map[string]any{
		"name": provider.Name,
		"ok":   ok,
	})
	c.JSON(http.StatusOK, gin.H{"ok": ok, "provider": provider.Name, "checks": checks})
}

// redirectURIFor is the URI this installation will present to the provider: the
// provider's own configured one when the operator set it (a server behind a proxy
// answers on a name that is not its internal one), and the installation default
// otherwise.
// refusalStatus is the HTTP answer to a named refusal. 401 is "the identity was not
// proven" (a token that does not check out, a code that cannot be exchanged, a state
// that is not this login's); 403 is "the identity was proven, and the provider's
// contract says it may not arrive here" (a domain outside the allow-list, an
// unverified email, a pending identity).
func refusalStatus(reason string) int {
	switch reason {
	case sso.ReasonDomainNotAllowed, sso.ReasonEmailUnverified,
		sso.ReasonNoEmailClaim, sso.ReasonIdentityPending, sso.ReasonAccountPending:
		return http.StatusForbidden
	}
	return http.StatusUnauthorized
}

// auditSSORefusal records a refused SSO login. The actor of a refused login is, by
// construction, not established — the row's value is the door it was refused at, the
// outcome, and the details: **which rule** refused it, whom the provider asserted,
// and (for 2.4-3's pending case) the account the assertion pointed at and the identity
// record the contract created. "SSO login failed" in a server log is not a record; the
// ledger row is what an operator reads when a user says "the door is closed", and what
// an admin reads to answer "someone tried to sign in as me".
// ssoRefusal is the shape of a refused SSO login, as the ledger records it.
type ssoRefusal struct {
	Reason  string
	Subject string
	Email   string
	Target  string
	Pending string
	// Detail is what failed, in the SP's own words: for SAML it is the condition
	// that refused the assertion (which audience, which expiry, which Recipient),
	// which is the sentence an operator acts on at the IdP.
	Detail string
}

func (h *SSOHandler) auditSSORefusal(c *gin.Context, provider *models.SSOProvider, status int, r ssoRefusal) {
	if h.store == nil || h.store.Audit == nil {
		return
	}
	details := map[string]any{"reason": r.Reason, "provider": provider.Name}
	if r.Subject != "" {
		details["subject"] = r.Subject
	}
	if r.Email != "" {
		details["asserted_email"] = r.Email
	}
	if r.Target != "" {
		details["target_user"] = r.Target
	}
	if r.Pending != "" {
		details["pending_identity"] = r.Pending
	}
	if r.Detail != "" {
		details["detail"] = r.Detail
	}
	auditRefusedOnce(c, h.store, models.AuditAuthLoginFailed, status,
		AuditTarget{Type: "sso-provider", ID: provider.Name}, details)
}

// auditSSOLogin records a login that arrived through an identity provider. The actor
// comes from the account the identity mapped to, because the request carries no
// session yet — this is the act that creates one. `id_token_verified` is stated, not
// hidden: false means the provider has no `jwks_uri` (its endpoints were typed by
// hand), so its identity rested on the userinfo call alone.
//
// When 2.4-3's `link_policy=link` stamped the identity onto an account that already
// existed, that is a second row (`sso.link`) naming the account: it is the pre-2.4
// behaviour, reachable only by the operator's explicit choice, and an operator who
// enables it should be able to see every time it happened.
func (h *SSOHandler) auditSSOLogin(c *gin.Context, outcome *sso.LoginOutcome, provider *models.SSOProvider) {
	if h.store == nil || h.store.Audit == nil || outcome == nil || outcome.User == nil {
		return
	}
	roles := ""
	if roleRows, err := h.store.UserRoles.GetUserRoles(outcome.User.ID); err == nil {
		names := make([]string, 0, len(roleRows))
		for _, r := range roleRows {
			names = append(names, r.Name)
		}
		roles = joinRoles(names)
	}
	markAudited(c)
	loginDetails := map[string]any{
		"provider":          provider.Name,
		"protocol":          providerProtocol(provider),
		"subject":           outcome.Subject,
		"email":             outcome.Email,
		"email_verified":    outcome.EmailVerified,
		"id_token_verified": outcome.VerifiedByIDToken,
		// 2.4-7: the SAML half of the same fact. A login that arrived on an
		// assertion signed by the pinned certificate says so in the ledger, exactly
		// as a login whose identity rested on a verified `id_token` says that.
		"assertion_signature_verified": outcome.VerifiedBySignature,
		"roles":                        roles,
	}
	if len(outcome.MappedRoles) > 0 {
		loginDetails["role_mapping_applied"] = true
	}
	_ = h.store.Audit.Record(&models.AuditLog{
		ID:            models.NewAuditID(),
		ActorUserID:   outcome.User.ID,
		ActorUsername: outcome.User.Username,
		ActorRoles:    roles,
		ActorProvider: provider.Name,
		Action:        models.AuditAuthLogin,
		TargetType:    "sso-provider",
		TargetID:      provider.Name,
		Outcome:       models.AuditOutcomeOK,
		Details:       store.EncodeDetails(loginDetails),
		SourceIP:      c.ClientIP(),
	})

	if outcome.LinkedExisting {
		_ = h.store.Audit.Record(&models.AuditLog{
			ID:            models.NewAuditID(),
			ActorUserID:   outcome.User.ID,
			ActorUsername: outcome.User.Username,
			ActorRoles:    roles,
			ActorProvider: provider.Name,
			Action:        models.AuditSSOLink,
			TargetType:    "user",
			TargetID:      outcome.User.ID,
			Outcome:       models.AuditOutcomeOK,
			Details: store.EncodeDetails(map[string]any{
				"provider": provider.Name,
				"subject":  outcome.Subject,
				"email":    outcome.Email,
				"policy":   models.SSOLinkPolicyLink,
			}),
			SourceIP: c.ClientIP(),
		})
	}
}

func (h *SSOHandler) redirectURIFor(provider *models.SSOProvider) string {
	if strings.TrimSpace(provider.RedirectURI) != "" {
		return provider.RedirectURI
	}
	return h.redirect + "/api/auth/sso/" + provider.Name + "/callback"
}

// acsURLFor is the assertion consumer service URL of a SAML provider: the
// destination the AuthnRequest names, the `Recipient` the assertion's
// SubjectConfirmationData must carry, and the location written into the SP metadata.
// They are one value computed in one place, because a provider whose request,
// metadata and validation disagree on the destination is a provider nobody can log
// in to.
func (h *SSOHandler) acsURLFor(provider *models.SSOProvider) string {
	return h.redirect + "/api/auth/sso/saml/" + provider.Name + "/acs"
}

// authorizeSAML sends the browser to the IdP with a deflated AuthnRequest, having
// recorded the request ID server-side (the same state record that holds the OIDC
// flow's nonce and PKCE verifier). IdP-initiated logins arrive without any of this,
// and the ACS accepts them on the audience/recipient/expiry rules alone.
func (h *SSOHandler) authorizeSAML(c *gin.Context, provider *models.SSOProvider) {
	requestID := sso.BuildAuthnRequestID()
	requestXML := sso.SAMLAuthnRequest(h.samlConfig(provider), requestID, time.Now())

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		log.Printf("sso: failed to generate state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	state := hex.EncodeToString(stateBytes)
	h.storeLoginState(state, provider.Name, "", "", requestID)
	c.SetCookie("sso_state", state, 300, "/", "", true, true)

	redirect, err := sso.SPInitiatedRedirect(requestXML, h.samlConfig(provider))
	if err != nil {
		// The request could not be built (an SP key that does not parse): name it,
		// and do not send the browser to an IdP with a request it will refuse.
		h.auditSSORefusal(c, provider, http.StatusBadRequest, ssoRefusal{Reason: sso.ReasonSAMLUnsigned, Detail: err.Error()})
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider is not usable: " + err.Error(), "reason": sso.ReasonSAMLUnsigned})
		return
	}
	c.Redirect(http.StatusFound, redirect)
}

// samlConfig projects a provider row onto the SAML path's config, with this
// installation's ACS URL.
func (h *SSOHandler) samlConfig(provider *models.SSOProvider) sso.SAMLConfig {
	return sso.SAMLConfigOf(provider, h.acsURLFor(provider))
}

// SAMLACS is the assertion consumer service: `POST /api/auth/sso/saml/:provider/acs`.
//
// The order is the security property, and it is the order the SAML Web SSO profile
// prescribes: validate the document **as an identity** (signature against the
// pinned certificate, audience, recipient, destination, expiry, subject
// confirmation, replay), and only then run the identity through the 2.4-3 contract.
// Nothing here trusts an assertion because it arrived: an unsigned, misaddressed,
// expired, foreign-audience, wrapped, or replayed assertion is refused, audited, and
// provisions no account.
func (h *SSOHandler) SAMLACS(c *gin.Context) {
	providerName := c.Param("provider")
	provider, err := h.store.SSOProviders.GetByName(providerName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	if !provider.IsSAML() {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "provider " + providerName + " is not a SAML provider",
			"reason": "provider_not_saml",
		})
		return
	}
	if !provider.Enabled {
		h.auditSSORefusal(c, provider, http.StatusBadRequest, ssoRefusal{Reason: "provider_disabled"})
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider not available", "reason": "provider_disabled"})
		return
	}

	// The form is an unauthenticated browser POST: the body is capped before it is
	// parsed, and only the `SAMLResponse` field is read.
	if err := c.Request.ParseForm(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the assertion form could not be read", "reason": sso.ReasonSAMLUnparseable})
		return
	}
	encoded := c.Request.Form.Get("SAMLResponse")
	if strings.TrimSpace(encoded) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no SAMLResponse in the form", "reason": sso.ReasonSAMLUnparseable})
		return
	}

	doc, err := sso.ParseSAMLResponse(encoded)
	if err != nil {
		h.auditSAMLRefusal(c, provider, err)
		return
	}

	// The server-side half of a login we started. An IdP-initiated post carries no
	// state, and is accepted on the audience/recipient/expiry rules; an
	// SP-initiated login must answer *this* request.
	expectedRequestID := ""
	if state, cookieErr := c.Cookie("sso_state"); cookieErr == nil && state != "" {
		login, ok := h.takeLoginState(state)
		if !ok || login.provider != provider.Name {
			h.auditSSORefusal(c, provider, http.StatusBadRequest, ssoRefusal{Reason: sso.ReasonStateExpired})
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state", "reason": sso.ReasonStateExpired})
			return
		}
		c.SetCookie("sso_state", "", -1, "/", "", true, true)
		expectedRequestID = login.samlRequestID
	}

	identity, refusal := sso.ValidateResponse(doc, h.samlConfig(provider), expectedRequestID, time.Now())
	if refusal != nil {
		h.auditSAMLRefusal(c, provider, refusal)
		return
	}

	// Replay: an assertion is a bearer artifact. A captured POST is valid for its
	// whole window, so a consumed assertion ID is remembered for that window and a
	// second presentation is refused.
	if !h.replay.Mark(identity.AssertionID, time.Now()) {
		h.auditSSORefusal(c, provider, http.StatusUnauthorized, ssoRefusal{
			Reason:  sso.ReasonSAMLReplayed,
			Subject: identity.Info.Subject,
			Email:   identity.Info.Email,
			Detail:  "assertion " + identity.AssertionID + " was already consumed",
		})
		c.JSON(http.StatusUnauthorized, gin.H{"error": "SSO login refused", "reason": sso.ReasonSAMLReplayed})
		return
	}

	outcome, err := h.oauth.IssueSAMLIdentity(c.Request.Context(), provider, identity.Info)
	if err != nil {
		h.auditSAMLRefusal(c, provider, err)
		return
	}

	h.auditSSOLogin(c, outcome, provider)

	loginCode, err := h.storePendingToken(outcome.AccessToken, outcome.RefreshToken)
	if err != nil {
		log.Printf("sso: failed to store pending token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	// Same C6 rule as the OIDC callback: the tokens are parked under a one-time
	// code and redeemed by POST /api/auth/sso/exchange. Nothing a session is made
	// from travels in a URL, and an assertion's POST body is not logged.
	c.Redirect(http.StatusFound, h.redirect+"/login?sso_code="+loginCode)
}

// auditSAMLRefusal records a refused SAML login, mapping the SAML refusal onto the
// ledger shape 2.4-2 established: the named reason, whom the provider asserted, and
// the status that says which half refused.
func (h *SSOHandler) auditSAMLRefusal(c *gin.Context, provider *models.SSOProvider, err error) {
	var iderr *sso.IdentityError
	if errors.As(err, &iderr) {
		status := refusalStatus(iderr.Reason)
		h.auditSSORefusal(c, provider, status, ssoRefusal{
			Reason:  iderr.Reason,
			Subject: iderr.Subject,
			Email:   iderr.Email,
			Target:  iderr.TargetUserID,
			Pending: iderr.PendingUserID,
			Detail:  iderr.Err.Error(),
		})
		c.JSON(status, gin.H{"error": "SSO login refused", "reason": iderr.Reason})
		return
	}
	var samlerr *sso.Refusal
	if errors.As(err, &samlerr) {
		h.auditSSORefusal(c, provider, http.StatusUnauthorized, ssoRefusal{
			Reason: samlerr.Reason,
			Detail: samlerr.Detail,
		})
		c.JSON(http.StatusUnauthorized, gin.H{"error": "SSO login refused", "reason": samlerr.Reason})
		return
	}
	h.auditSSORefusal(c, provider, http.StatusUnauthorized, ssoRefusal{Reason: sso.ReasonSAMLUnparseable, Detail: err.Error()})
	c.JSON(http.StatusUnauthorized, gin.H{"error": "SSO login refused", "reason": sso.ReasonSAMLUnparseable})
}

// SAMLMetadata serves the SP metadata an operator uploads to the IdP:
// `GET /api/auth/sso/saml/:provider/metadata`.
//
// Public, like the login page it belongs to: it names the entity ID, the ACS
// location, the NameID format and the SP's **signing certificate** — all of which
// the operator publishes to the IdP — and it carries no key material, ever.
func (h *SSOHandler) SAMLMetadata(c *gin.Context) {
	providerName := c.Param("provider")
	// The operator-facing lookup: a disabled provider's metadata is exactly the
	// document the operator needs in order to enable it correctly.
	provider, err := h.store.SSOProviders.GetByNameConfigured(providerName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	if !provider.IsSAML() {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "provider " + providerName + " is not a SAML provider",
			"reason": "provider_not_saml",
		})
		return
	}
	doc, err := sso.SPMetadata(h.samlConfig(provider))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "provider metadata is not available: " + err.Error()})
		return
	}
	filename := provider.Name + "-sp-metadata.xml"
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, "application/samlmetadata+xml", []byte(doc))
}

// DiscoverIssuer is the discovery **preview**: what an issuer publishes, shown to
// the operator before a provider row exists (2.4-1 — "show the operator exactly
// what was discovered before they save it").
//
// POST /api/sso/providers/discover, admin only.
//
// It writes nothing and can write nothing: there is no row to half-save, and the
// answer is the same named states the test-connection probe uses, so the console
// renders one vocabulary for "what did the IdP say" everywhere. It is audited
// (`sso.discover`) because it is the server fetching a URL an operator typed.
func (h *SSOHandler) DiscoverIssuer(c *gin.Context) {
	var req struct {
		Issuer       string `json:"issuer"`
		DiscoveryURL string `json:"discovery_url"`
		ProviderName string `json:"provider_name"`
		RedirectURI  string `json:"redirect_uri"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	issuer := strings.TrimSpace(req.Issuer)
	if problem := issuerIsConfigurable(issuer); problem != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": problem})
		return
	}

	// The preview names the redirect URI the operator has to register with the
	// IdP. Without the provider name it can only state the installation default,
	// and saying so is better than showing a URI that is not the one the login
	// will send.
	nameForPreview := strings.TrimSpace(req.ProviderName)
	if !providerNameIsSlug(nameForPreview) {
		nameForPreview = "<provider-name>"
	}
	redirectURI := h.redirectURIFor(&models.SSOProvider{
		Name: nameForPreview, RedirectURI: strings.TrimSpace(req.RedirectURI),
	})

	type check struct {
		Name   string `json:"name"`
		State  string `json:"state"`
		Detail string `json:"detail,omitempty"`
	}
	checks := []check{}
	ok := true
	add := func(name, state, detail string) {
		checks = append(checks, check{Name: name, State: state, Detail: detail})
		if state == "fail" {
			ok = false
		}
	}

	kind := sso.DiscoveryOK
	doc, err := sso.Discover(h.http, issuer, strings.TrimSpace(req.DiscoveryURL))
	if err != nil {
		kind = sso.DiscoveryUnreachable
		var de *sso.DiscoveryError
		if errors.As(err, &de) {
			kind = de.Kind
		}
		add("discovery", "fail", kind+": "+err.Error())
		defer auditForAnswer(c, h.store, models.AuditSSODiscover,
			AuditTarget{Type: "sso-provider", ID: nameForPreview},
			map[string]any{"issuer": issuer, "ok": false, "discovery": kind})
		c.JSON(http.StatusOK, gin.H{
			"ok": false, "issuer": issuer, "discovery": kind,
			"error": err.Error(), "checks": checks, "redirect_uri": redirectURI,
		})
		return
	}

	add("discovery", "pass", doc.Issuer)
	kind = sso.DiscoveryOK

	if doc.PKCESupported() {
		add("pkce", "pass", strings.Join(doc.PKCEMethods, ", "))
	} else {
		add("pkce", "warn", "the provider publishes no code-challenge method")
	}
	if doc.ScopeSupports("email") {
		add("email_scope", "pass", "email")
	} else {
		add("email_scope", "warn", "the provider does not list the email scope")
	}
	if doc.SupportsSigningAlg("RS256") {
		add("id_token_alg", "pass", strings.Join(doc.IDTokenSigningAlgs, ", "))
	} else {
		// Our verifier is RS256-only (2.4-2). An IdP publishing only ES256 is a
		// provider whose logins will be refused, and the preview is where that is
		// heard for the first time.
		add("id_token_alg", "fail", "the provider signs id_tokens with "+
			strings.Join(doc.IDTokenSigningAlgs, ", ")+"; OurWay verifies RS256")
	}
	if strings.TrimSpace(doc.UserInfoEndpoint) == "" {
		add("userinfo", "warn", "no userinfo_endpoint: identity comes from the id_token alone")
	} else {
		add("userinfo", "pass", doc.UserInfoEndpoint)
	}
	if err := h.jwks.Probe(doc.JwksURI); err != nil {
		add("jwks", "fail", err.Error())
	} else {
		add("jwks", "pass", doc.JwksURI)
	}
	if strings.Contains(redirectURI, "://localhost") || strings.Contains(redirectURI, "://127.0.0.1") {
		add("redirect_uri", "warn", "redirect URI is a loopback address: "+redirectURI)
	} else {
		add("redirect_uri", "pass", redirectURI)
	}

	defer auditForAnswer(c, h.store, models.AuditSSODiscover,
		AuditTarget{Type: "sso-provider", ID: nameForPreview},
		map[string]any{"issuer": doc.Issuer, "ok": ok, "discovery": kind})

	c.JSON(http.StatusOK, gin.H{
		"ok":             ok,
		"issuer":         doc.Issuer,
		"discovery":      kind,
		"discovery_url":  strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration",
		"redirect_uri":   redirectURI,
		"endpoints":      gin.H{"authorization": doc.AuthorizationEndpoint, "token": doc.TokenEndpoint, "userinfo": doc.UserInfoEndpoint, "jwks": doc.JwksURI},
		"signing_algs":   doc.IDTokenSigningAlgs,
		"response_types": doc.ResponseTypes,
		"scopes":         doc.ScopesSupported,
		"pkce_methods":   doc.PKCEMethods,
		"checks":         checks,
		"document":       json.RawMessage(doc.Raw),
	})
}

// issuerIsConfigurable is the door check on the issuer an operator types. An
// empty answer means "I am not asking a provider, I typed the endpoints" and is
// the caller's business; anything else must be a URL this server can fetch, and
// not a URL that turns an admin form into a request to a machine on the local
// network over a scheme no OIDC document is served over.
func issuerIsConfigurable(issuer string) string {
	if issuer == "" {
		return "issuer is required: the provider's issuer URL is what its endpoints are discovered from"
	}
	u, err := url.Parse(issuer)
	if err != nil {
		return "issuer is not a URL: " + err.Error()
	}
	if !u.IsAbs() || u.Host == "" {
		return "issuer must be an absolute URL including the scheme, e.g. https://idp.corp/realms/ourway"
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "issuer must be an http or https URL"
	}
	if u.User != nil {
		return "issuer must not carry credentials in the URL"
	}
	return ""
}

// providerRequest is the payload for saving a provider row: create (by name) and
// update (by id) share one shape, so a provider cannot be edited through a door that
// skips a validation the create door applies (2.4-4).
//
// Pointers are "the operator sent this field": an absent field is left alone, and an
// explicit empty string is a real state — no scope override, no domain allow-list, no
// role mapping. `auth_url`/`token_url`/`user_info_url` are plain strings because empty
// *is* their state there: it means "use what the IdP published".
type providerRequest struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`

	Issuer       string `json:"issuer"`
	DiscoveryURL string `json:"discovery_url"`
	RedirectURI  string `json:"redirect_uri"`
	AuthURL      string `json:"auth_url"`
	TokenURL     string `json:"token_url"`
	UserInfoURL  string `json:"user_info_url"`

	Scope          *string `json:"scope"`
	ClaimsEmail    *string `json:"claims_email"`
	ClaimsUsername *string `json:"claims_username"`
	ClaimsName     *string `json:"claims_name"`
	ClaimsGroups   *string `json:"claims_groups"`

	// The 2.4-3 contract's knobs, per provider (2.4-4 puts them in the console).
	AllowedDomains       *string `json:"allowed_domains"`
	RequireVerifiedEmail *bool   `json:"require_verified_email"`
	LinkPolicy           string  `json:"link_policy"`
	RoleMapping          *string `json:"role_mapping"`

	// 2.4-7 · SAML 2.0. Pointers, because a SAML provider is edited field by field
	// like every other provider row (2.4-4's rule): the console's enable toggle must
	// not be able to erase the IdP's certificate.
	IdpEntityID              *string `json:"idp_entity_id"`
	IdpSSOURL                *string `json:"idp_sso_url"`
	IdpLogoutURL             *string `json:"idp_logout_url"`
	IdpCertificate           *string `json:"idp_certificate"`
	RequireResponseSignature *bool   `json:"require_response_signature"`
	NameIDFormat             *string `json:"nameid_format"`
	GenerateSPKey            *bool   `json:"generate_sp_key"`
	AttrEmail                *string `json:"attr_email"`
	AttrUsername             *string `json:"attr_username"`
	AttrName                 *string `json:"attr_name"`
	AttrGroups               *string `json:"attr_groups"`
	VerifiedEmailAttribute   *string `json:"verified_email_attribute"`

	SortOrder *int  `json:"sort_order"`
	Enabled   *bool `json:"enabled"`
}

// CreateProvider creates or updates an SSO provider (admin).
//
// The row is saved only if the provider proved itself: an issuer is discovered at
// save time, and a discovery failure answers a named 400 and writes nothing
// (2.4-1's acceptance criterion — "no half-saved provider row"). The endpoints the
// flow will call are the ones the IdP published, unless the operator named them.
func (h *SSOHandler) CreateProvider(c *gin.Context) {
	var req providerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// The type gate. Since 2.4-7 SAML is implemented, so `saml` is accepted — and
	// the only unsupported case left is an unknown type, which is refused here
	// rather than accepted and silently broken (L8's rule, kept).
	if problem := providerTypeChange(nil, req.Type, true); problem != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": problem})
		return
	}
	samlRequested := strings.EqualFold(strings.TrimSpace(req.Type), models.SSOTypeSAML)
	if strings.TrimSpace(req.ClientID) == "" && !samlRequested {
		// A SAML provider has no client id: its identity is the SP entity ID the
		// assertion's audience must name, derived from the provider row.
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_id is required"})
		return
	}

	// The provider name is a URL path segment (`/api/auth/sso/<name>/callback`),
	// so it is validated as one. A name with a slash, a dot-dot, or a space in it
	// is a provider whose callback URL is not the URL the operator registered
	// with the IdP.
	if !providerNameIsSlug(req.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider name must be a slug: letters, digits and dashes (it is used in the callback URL)"})
		return
	}

	existing, err := h.store.SSOProviders.GetByName(req.Name)
	var provider *models.SSOProvider
	if err == nil && existing != nil {
		provider = existing
	} else {
		provider = &models.SSOProvider{ID: uuid.New().String(), Name: req.Name, Enabled: true}
	}

	if problem, ok := h.applyProviderEdits(provider, &req, true); !ok {
		// A protocol change is a conflict with the row as it stands, not a typo in
		// the body: say so with the status the console can show beside the field.
		status := http.StatusBadRequest
		if strings.Contains(problem, "type_immutable") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": problem, "reason": "type_immutable"})
		return
	}

	if err := h.applyDiscovery(provider, req.AuthURL, req.TokenURL, req.UserInfoURL); err != nil {
		kind := sso.DiscoveryUnreachable
		var de *sso.DiscoveryError
		if errors.As(err, &de) {
			kind = de.Kind
		}
		// Named, at the API, and nothing was written. "SSO provider failed to save"
		// is the message an operator cannot act on; "issuer_mismatch: document says
		// https://idp.corp, configured https://idp.corp/" is.
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider not saved: " + err.Error(), "discovery": kind})
		return
	}

	// An identity provider is the door to every account, so editing one is
	// recorded with its name, type and issuer — and never with its client secret.
	defer auditForAnswer(c, h.store, models.AuditSSOCreate, AuditTarget{Type: "sso-provider", ID: provider.ID}, providerAuditDetails(provider))

	var saveErr error
	if existing != nil && provider.ID == existing.ID {
		saveErr = h.store.SSOProviders.Update(provider)
	} else {
		saveErr = h.store.SSOProviders.Create(provider)
	}
	if saveErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save provider"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"provider": provider})
}

// UpdateProvider edits a saved provider in place (2.4-4): `PUT /api/sso/providers/:id`.
// Field by field, so the console's enable toggle cannot wipe a policy the operator set
// from the API. Discovery re-runs — a provider row that no longer describes a
// reachable IdP is refused with the same named kinds as at create, and the stored row
// is left as it was.
//
// The name is immutable. It is the key stored on every account that signed in through
// this provider (`users.provider`), so renaming a provider is not editing a label: it
// orphans those identities. Create a new provider to change the name.
func (h *SSOHandler) UpdateProvider(c *gin.Context) {
	providerID := c.Param("id")
	provider, err := h.store.SSOProviders.GetByID(providerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	var req providerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// (The type rule is enforced in applyProviderEdits below, where create and
	// update share it — the 2.4-4 rule that no door skips a rule the other
	// enforces. A protocol change answers 409, like the name.)
	if req.Name != "" && req.Name != provider.Name {
		c.JSON(http.StatusConflict, gin.H{
			"error":  "provider name cannot be changed: it is the identity key stored on every account signed in through this provider",
			"reason": "name_immutable",
		})
		return
	}

	if problem, ok := h.applyProviderEdits(provider, &req, false); !ok {
		status := http.StatusBadRequest
		if strings.Contains(problem, "type_immutable") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": problem, "reason": "type_immutable"})
		return
	}

	// Typed endpoint overrides are the stored ones unless this request typed new
	// ones: an update that omits them must not silently drop an override a proxy
	// depends on.
	typedAuth, typedToken, typedUserInfo := provider.AuthURL, provider.TokenURL, provider.UserInfoURL
	if req.AuthURL != "" {
		typedAuth = req.AuthURL
	}
	if req.TokenURL != "" {
		typedToken = req.TokenURL
	}
	if req.UserInfoURL != "" {
		typedUserInfo = req.UserInfoURL
	}
	if err := h.applyDiscovery(provider, typedAuth, typedToken, typedUserInfo); err != nil {
		kind := sso.DiscoveryUnreachable
		var de *sso.DiscoveryError
		if errors.As(err, &de) {
			kind = de.Kind
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider not changed: " + err.Error(), "discovery": kind})
		return
	}

	defer auditForAnswer(c, h.store, models.AuditSSOUpdate, AuditTarget{Type: "sso-provider", ID: provider.ID}, providerAuditDetails(provider))

	if err := h.store.SSOProviders.Update(provider); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save provider"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"provider": provider})
}

// applyProviderEdits validates and applies one request onto a provider row — the
// 2.4-3 contract's knobs included — and answers the operator-actionable reason when a
// value is not admissible. Discovery is the caller's job: the row is only stored once
// discovery has confirmed it.
func (h *SSOHandler) applyProviderEdits(provider *models.SSOProvider, req *providerRequest, creating bool) (string, bool) {
	if req.LinkPolicy != "" && req.LinkPolicy != models.SSOLinkPolicyPending && req.LinkPolicy != models.SSOLinkPolicyLink {
		return "link_policy must be pending or link", false
	}
	if req.Type != "" {
		if problem := providerTypeChange(provider, req.Type, creating); problem != "" {
			return problem, false
		}
	}
	if req.RoleMapping != nil {
		if msg, ok := validRoleMapping(*req.RoleMapping); !ok {
			return msg, false
		}
	}
	if req.AllowedDomains != nil {
		if msg, ok := validDomains(*req.AllowedDomains); !ok {
			return msg, false
		}
	}

	if req.Type != "" {
		provider.Type = req.Type
	}
	if strings.TrimSpace(req.ClientID) != "" {
		provider.ClientID = strings.TrimSpace(req.ClientID)
	}
	if req.ClientSecret != "" {
		provider.ClientSecret = req.ClientSecret
	}

	// The issuer is the one field the operator types; the presets supply it for the
	// big three. It is validated as an HTTPS URL before it is stored, because it is
	// where the server is about to make an outbound request (the SSRF lesson, 2.4-1).
	issuer, presetScope := providerPreset(provider.Name)
	if typed := strings.TrimSpace(req.Issuer); typed != "" {
		if problem := issuerIsConfigurable(typed); problem != "" {
			return problem, false
		}
		provider.Issuer = typed
	} else if strings.TrimSpace(provider.Issuer) == "" {
		provider.Issuer = issuer
	}
	provider.DiscoveryURL = strings.TrimSpace(req.DiscoveryURL)
	provider.RedirectURI = strings.TrimSpace(req.RedirectURI)

	if req.Scope != nil {
		provider.Scope = *req.Scope
	} else if provider.Scope == "" {
		provider.Scope = presetScope
	}
	if req.ClaimsEmail != nil {
		provider.ClaimsEmail = strings.TrimSpace(*req.ClaimsEmail)
	}
	if req.ClaimsUsername != nil {
		provider.ClaimsUsername = strings.TrimSpace(*req.ClaimsUsername)
	}
	if req.ClaimsName != nil {
		provider.ClaimsName = strings.TrimSpace(*req.ClaimsName)
	}
	if req.ClaimsGroups != nil {
		provider.ClaimsGroups = strings.TrimSpace(*req.ClaimsGroups)
	}
	if req.AllowedDomains != nil {
		provider.AllowedDomains = strings.ToLower(strings.TrimSpace(*req.AllowedDomains))
	}
	if req.RequireVerifiedEmail != nil {
		provider.RequireVerifiedEmail = *req.RequireVerifiedEmail
	}
	if req.LinkPolicy != "" {
		provider.LinkPolicy = req.LinkPolicy
	}
	if provider.LinkPolicy == "" {
		// The contract's default, and it is not neutral: `pending` means an IdP
		// cannot claim a local account by asserting its email.
		provider.LinkPolicy = models.SSOLinkPolicyPending
	}
	if req.RoleMapping != nil {
		provider.RoleMapping = strings.TrimSpace(*req.RoleMapping)
	}
	if req.SortOrder != nil {
		provider.SortOrder = *req.SortOrder
	}
	if req.Enabled != nil {
		provider.Enabled = *req.Enabled
	}

	// --- 2.4-7 · SAML. Field presence, as everywhere else in this row.
	if req.IdpEntityID != nil {
		provider.IdpEntityID = strings.TrimSpace(*req.IdpEntityID)
	}
	if req.IdpSSOURL != nil {
		provider.IdpSSOURL = strings.TrimSpace(*req.IdpSSOURL)
	}
	if req.IdpLogoutURL != nil {
		provider.IdpLogoutURL = strings.TrimSpace(*req.IdpLogoutURL)
	}
	if req.IdpCertificate != nil {
		provider.IdpCertificate = strings.TrimSpace(*req.IdpCertificate)
	}
	if req.RequireResponseSignature != nil {
		provider.RequireResponseSignature = *req.RequireResponseSignature
	}
	if req.NameIDFormat != nil {
		provider.NameIDFormat = strings.ToLower(strings.TrimSpace(*req.NameIDFormat))
	}
	if req.AttrEmail != nil {
		provider.AttrEmail = strings.TrimSpace(*req.AttrEmail)
	}
	if req.AttrUsername != nil {
		provider.AttrUsername = strings.TrimSpace(*req.AttrUsername)
	}
	if req.AttrName != nil {
		provider.AttrName = strings.TrimSpace(*req.AttrName)
	}
	if req.AttrGroups != nil {
		provider.AttrGroups = strings.TrimSpace(*req.AttrGroups)
	}
	if req.VerifiedEmailAttribute != nil {
		provider.VerifiedEmailAttribute = strings.TrimSpace(*req.VerifiedEmailAttribute)
	}
	if provider.NameIDFormat == "" {
		provider.NameIDFormat = models.SAMLNameIDFormatEmail
	}
	if provider.IsSAML() {
		// The SP entity ID names **this** installation to the IdP, and the assertion's
		// audience must name it. It is derived from the provider's immutable name and
		// id, not typed, so an operator cannot point two installations' assertions at
		// each other by retyping a string.
		provider.SPEntityID = sso.SPFor(provider)
	}
	if req.GenerateSPKey != nil && *req.GenerateSPKey && provider.IsSAML() {
		certPEM, keyPEM, err := sso.GenerateSPKeyPair(provider.SPEntityID, time.Now())
		if err != nil {
			return "could not generate the SP signing key: " + err.Error(), false
		}
		provider.SPCertPEM = certPEM
		provider.SPKeyPEM = keyPEM
	}
	if problem := h.validateSAMLProvider(provider, creating); problem != "" {
		return problem, false
	}
	return "", true
}

// providerTypeChange refuses a provider row that changes protocol under the
// operator's feet. `oauth2`/`oauth`/`oidc` are names of the one flow, so they may
// be spelled differently; `saml` is a different door: its identities are filed by
// asserted NameID, its endpoints are an IdP's entity ID and SSO location, and a row
// switched from OIDC to SAML would answer logins with rules it never had.
// The provider's name is already immutable for the same reason (2.4-4).
func providerTypeChange(provider *models.SSOProvider, requested string, creating bool) string {
	requested = strings.ToLower(strings.TrimSpace(requested))
	switch requested {
	case "oauth2", "oauth", "oidc", models.SSOTypeSAML:
	default:
		return "type must be oidc, oauth2 or saml"
	}
	if creating {
		return ""
	}
	current := strings.ToLower(strings.TrimSpace(provider.Type))
	if current == models.SSOTypeSAML || requested == models.SSOTypeSAML {
		if current != requested {
			return "type cannot be changed: a SAML provider's accounts are filed by asserted NameID and an OIDC provider's by subject (reason: type_immutable)"
		}
	}
	return ""
}

// validateSAMLProvider states what a SAML provider row must carry before it can
// answer a login, in the words the console puts beside the field. This is the door
// that keeps 2.4-7's "refused at save, loudly" rule: a provider row that cannot
// validate an assertion is not saved, exactly as a provider whose issuer does not
// answer is not saved.
func (h *SSOHandler) validateSAMLProvider(provider *models.SSOProvider, creating bool) string {
	if !provider.IsSAML() {
		return ""
	}
	if strings.TrimSpace(provider.IdpEntityID) == "" {
		return "idp_entity_id is required for a SAML provider: it is who must sign the assertion"
	}
	if problem := samlEndpointIsConfigurable(provider.IdpSSOURL); problem != "" {
		return "idp_sso_url " + problem
	}
	if provider.IdpLogoutURL != "" {
		if problem := samlEndpointIsConfigurable(provider.IdpLogoutURL); problem != "" {
			return "idp_logout_url " + problem
		}
	}
	if problem := sso.ValidateCertificatePEM(provider.IdpCertificate, time.Now()); problem != "" {
		return "idp_certificate is unusable: " + problem
	}
	switch strings.ToLower(provider.NameIDFormat) {
	case models.SAMLNameIDFormatEmail, "persistent", "transient", "unspecified":
	default:
		return "nameid_format must be emailaddress, persistent, transient or unspecified"
	}
	if provider.RequireVerifiedEmail && strings.TrimSpace(provider.VerifiedEmailAttribute) == "" {
		return "a SAML provider that requires a verified email must name the attribute that states it (verified_email_attribute): SAML has no standard one, and OurWay does not guess"
	}
	if provider.SPEntityID == "" {
		provider.SPEntityID = sso.SPFor(provider)
	}
	if creating {
		return ""
	}
	if provider.SPKeyPEM != "" && provider.SPCertPEM == "" {
		return "the provider has an SP signing key with no certificate: the IdP cannot verify a signed request"
	}
	return ""
}

// samlEndpointIsConfigurable is the SSRF rule 2.4-1 established for issuers, applied
// to the endpoints a SAML provider names: https only, and not a loopback, link-local
// or metadata address. The SSO URL is a place the browser is redirected to and the
// assertion is posted back from; a provider that can point it at an internal
// service is a door into that service.
func samlEndpointIsConfigurable(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "is required for a SAML provider"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "must be an https URL"
	}
	// The host first, then the port: an operator who typed `localhost:8443` has the
	// localhost problem, and that is the sentence they need.
	if strings.EqualFold(parsed.Hostname(), "localhost") {
		return "must not name localhost: the browser, not the server, is redirected there"
	}
	if strings.Contains(parsed.Host, ":") {
		return "must not name a port: an SSO endpoint is a published HTTPS service (the port comes from the reverse proxy)"
	}
	if problem := hostIsPublic(parsed.Hostname()); problem != "" {
		return problem
	}
	return ""
}

// hostIsPublic refuses the hosts a *server* must never be pointed at, the rule
// 2.4-1 established for issuers and 2.4-7 applies to every endpoint a provider row
// names: loopback, link-local, the unspecified address, the cloud metadata endpoint,
// and any bare IP literal. An operator who needs a lab IdP on a private address
// publishes it under a name, which is also what the browser needs to reach it.
func hostIsPublic(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return "must name a host"
	}
	if ip := net.ParseIP(host); ip != nil {
		return "must name a host, not an IP address: the IdP's URL is typed into a browser too"
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "must not name localhost"
	}
	if strings.HasSuffix(host, ".local") {
		return "must not name a .local address"
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return "must not name a loopback, link-local or unspecified address"
		}
	}
	if host == "metadata.google.internal" || strings.HasSuffix(host, ".internal") {
		return "must not name an internal service address"
	}
	return ""
}

// providerAuditDetails records enough about a provider to reconstruct the change, and
// never the client secret.
func providerAuditDetails(provider *models.SSOProvider) map[string]any {
	details := map[string]any{
		"name":                   provider.Name,
		"type":                   provider.Type,
		"issuer":                 provider.Issuer,
		"enabled":                provider.Enabled,
		"link_policy":            provider.LinkPolicy,
		"allowed_domains":        provider.AllowedDomains,
		"require_verified_email": provider.RequireVerifiedEmail,
		"role_mapping":           provider.RoleMapping,
		"sort_order":             provider.SortOrder,
	}
	if provider.IsSAML() {
		// The SAML facts an operator changed, and never the key material: the IdP
		// certificate is recorded as its fingerprint, which is the value that says
		// *which* key the installation now trusts.
		details["idp_entity_id"] = provider.IdpEntityID
		details["idp_sso_url"] = provider.IdpSSOURL
		details["sp_entity_id"] = provider.SPEntityID
		details["idp_certificate_sha256"] = sso.CertificateFingerprint(provider.IdpCertificate)
		details["require_response_signature"] = provider.RequireResponseSignature
		details["nameid_format"] = provider.NameIDFormat
		details["sp_signing_key"] = provider.SPKeyPEM != ""
		details["verified_email_attribute"] = provider.VerifiedEmailAttribute
	}
	return details
}

// providerProtocol names the door a provider row is, for the ledger: `oidc` and
// `saml` are the two the product implements, and the login row should say which one
// a person arrived through.
func providerProtocol(provider *models.SSOProvider) string {
	if provider.IsSAML() {
		return models.SSOTypeSAML
	}
	return models.SSOTypeOIDC
}

// providerNameIsSlug accepts what a URL path segment can carry.
func providerNameIsSlug(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return !strings.Contains(name, "..")
}

// validRoleMapping requires a JSON object of claim value -> role name. An empty
// string is "no mapping", which is the safe state, not "everyone is admin".
func validRoleMapping(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	var mapping map[string]string
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		return "role_mapping must be a JSON object of claim value to role name", false
	}
	for claimValue, role := range mapping {
		if strings.TrimSpace(claimValue) == "" || strings.TrimSpace(role) == "" {
			return "role_mapping must not contain empty claim values or roles", false
		}
	}
	return "", true
}

// validDomains accepts a comma-separated list of plain hostnames.
func validDomains(raw string) (string, bool) {
	for _, domain := range strings.Split(raw, ",") {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if strings.ContainsAny(domain, "@ /\\:") || strings.HasPrefix(domain, ".") {
			return "allowed_domains takes plain domain names, comma-separated", false
		}
	}
	return "", true
}

// ListProvidersAdmin returns all SSO providers with details (admin).
func (h *SSOHandler) ListProvidersAdmin(c *gin.Context) {
	providers, err := h.store.SSOProviders.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list providers"})
		return
	}
	// The certificate and the SP key are `json:"-"` on the row: the console sees the
	// **fingerprint** and the expiry, which is what an operator compares with the
	// IdP's published certificate and what says a provider is about to start
	// refusing logins. The PEM the operator pasted is never sent back.
	type samlInfo struct {
		IDPEntityID              string `json:"idp_entity_id"`
		IdpSSOURL                string `json:"idp_sso_url"`
		IdpLogoutURL             string `json:"idp_logout_url"`
		SPEntityID               string `json:"sp_entity_id"`
		ACSURL                   string `json:"acs_url"`
		CertificateSHA256        string `json:"idp_certificate_sha256"`
		CertificateExpires       string `json:"idp_certificate_expires"`
		SignsRequests            bool   `json:"signs_requests"`
		RequireResponseSignature bool   `json:"require_response_signature"`
		NameIDFormat             string `json:"nameid_format"`
		AttrEmail                string `json:"attr_email"`
		AttrUsername             string `json:"attr_username"`
		AttrName                 string `json:"attr_name"`
		AttrGroups               string `json:"attr_groups"`
		VerifiedEmailAttribute   string `json:"verified_email_attribute"`
	}
	type adminProvider struct {
		*models.SSOProvider
		SAML *samlInfo `json:"saml,omitempty"`
	}
	out := make([]adminProvider, 0, len(providers))
	for i := range providers {
		row := adminProvider{SSOProvider: &providers[i]}
		if providers[i].IsSAML() {
			expires := ""
			if cert, certErr := parseProviderCertificate(providers[i].IdpCertificate); certErr == nil {
				expires = cert.NotAfter.UTC().Format(time.RFC3339)
			}
			row.SAML = &samlInfo{
				IDPEntityID:              providers[i].IdpEntityID,
				IdpSSOURL:                providers[i].IdpSSOURL,
				IdpLogoutURL:             providers[i].IdpLogoutURL,
				SPEntityID:               providers[i].SPEntityID,
				ACSURL:                   h.acsURLFor(&providers[i]),
				CertificateSHA256:        sso.CertificateFingerprint(providers[i].IdpCertificate),
				CertificateExpires:       expires,
				SignsRequests:            strings.TrimSpace(providers[i].SPKeyPEM) != "",
				RequireResponseSignature: providers[i].RequireResponseSignature,
				NameIDFormat:             providers[i].NameIDFormat,
				AttrEmail:                providers[i].AttrEmail,
				AttrUsername:             providers[i].AttrUsername,
				AttrName:                 providers[i].AttrName,
				AttrGroups:               providers[i].AttrGroups,
				VerifiedEmailAttribute:   providers[i].VerifiedEmailAttribute,
			}
		}
		out = append(out, row)
	}
	c.JSON(http.StatusOK, gin.H{"providers": out})
}

// parseProviderCertificate reads a provider's pinned IdP certificate for display
// (expiry, fingerprint). It is the same parser the SAML path uses, so the console
// and the login path agree about what a usable certificate is.
func parseProviderCertificate(pemText string) (*x509.Certificate, error) {
	trimmed := strings.TrimSpace(pemText)
	if trimmed == "" {
		return nil, errors.New("no certificate")
	}
	if !strings.HasPrefix(trimmed, "-----BEGIN") {
		trimmed = "-----BEGIN CERTIFICATE-----\n" + strings.Join(strings.Fields(trimmed), "") + "\n-----END CERTIFICATE-----"
	}
	block, _ := pem.Decode([]byte(trimmed))
	if block == nil {
		return nil, errors.New("certificate is not valid PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}

// DeleteProvider removes an SSO provider (admin).
func (h *SSOHandler) DeleteProvider(c *gin.Context) {
	providerID := c.Param("id")

	defer auditForAnswer(c, h.store, models.AuditSSODelete, AuditTarget{Type: "sso-provider", ID: providerID}, nil)
	if err := h.store.SSOProviders.Delete(providerID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "provider deleted"})
}
