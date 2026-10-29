package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
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
}

// loginState is what the authorize redirect committed to before the IdP has seen
// anything: the provider, the PKCE verifier, and the nonce the id_token must carry.
type loginState struct {
	provider string
	verifier string
	nonce    string
	expires  time.Time
}

// ssoStateTTL is how long an authorize redirect may sit open before its verifier
// and nonce are dead. It matches the `sso_state` cookie's 300 seconds: a login that
// was not completed inside that window is restarted, not resumed later.
const ssoStateTTL = 5 * time.Minute

// storeLoginState records the verifier and nonce for one authorize redirect, and
// sweeps the entries whose window has passed. An unpruned map is a memory leak with
// a login form attached (the M11 lesson), so the sweep rides on the write.
func (h *SSOHandler) storeLoginState(state, provider, verifier, nonce string) {
	h.statesMu.Lock()
	defer h.statesMu.Unlock()
	now := time.Now()
	for key, st := range h.states {
		if now.After(st.expires) {
			delete(h.states, key)
		}
	}
	h.states[state] = &loginState{
		provider: provider,
		verifier: verifier,
		nonce:    nonce,
		expires:  now.Add(ssoStateTTL),
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
		Name        string `json:"name"`
		Label       string `json:"label"`
		SortOrder   int    `json:"sort_order"`
		RedirectURI string `json:"redirect_uri,omitempty"`
	}

	var enabled []providerInfo
	for _, p := range providers {
		if p.Enabled {
			enabled = append(enabled, providerInfo{
				ID: p.ID, Type: p.Type, Name: p.Name,
				Label: strings.TrimSpace(p.Issuer), SortOrder: p.SortOrder,
				RedirectURI: p.RedirectURI,
			})
		}
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

	// The flow below is OAuth2/OIDC only; a legacy saml-typed provider
	// row cannot be driven through it (L8).
	if t := strings.ToLower(provider.Type); t != "oauth2" && t != "oauth" && t != "oidc" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported SSO type: only oauth2/oidc is implemented (saml is not supported yet)"})
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
	h.storeLoginState(state, provider.Name, verifier, nonce)

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

	// Same guard as Authorize: the callback is OAuth2/OIDC only (L8).
	if t := strings.ToLower(provider.Type); t != "oauth2" && t != "oauth" && t != "oidc" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported SSO type: only oauth2/oidc is implemented (saml is not supported yet)"})
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
		"subject":           outcome.Subject,
		"email":             outcome.Email,
		"email_verified":    outcome.EmailVerified,
		"id_token_verified": outcome.VerifiedByIDToken,
		"roles":             roles,
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

	// Only the OAuth2/OIDC flow is implemented; SAML is rejected rather than
	// accepted and silently broken (L8).
	switch strings.ToLower(req.Type) {
	case "oauth2", "oauth", "oidc":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported SSO type: only oauth2/oidc is implemented (saml is not supported yet)"})
		return
	}
	if strings.TrimSpace(req.ClientID) == "" {
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

	if problem, ok := h.applyProviderEdits(provider, &req); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": problem})
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
	if req.Type != "" {
		switch strings.ToLower(req.Type) {
		case "oauth2", "oauth", "oidc":
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported SSO type: only oauth2/oidc is implemented (saml is not supported yet)"})
			return
		}
	}
	if req.Name != "" && req.Name != provider.Name {
		c.JSON(http.StatusConflict, gin.H{
			"error":  "provider name cannot be changed: it is the identity key stored on every account signed in through this provider",
			"reason": "name_immutable",
		})
		return
	}

	if problem, ok := h.applyProviderEdits(provider, &req); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": problem})
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
func (h *SSOHandler) applyProviderEdits(provider *models.SSOProvider, req *providerRequest) (string, bool) {
	if req.LinkPolicy != "" && req.LinkPolicy != models.SSOLinkPolicyPending && req.LinkPolicy != models.SSOLinkPolicyLink {
		return "link_policy must be pending or link", false
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
	return "", true
}

// providerAuditDetails records enough about a provider to reconstruct the change, and
// never the client secret.
func providerAuditDetails(provider *models.SSOProvider) map[string]any {
	return map[string]any{
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
	c.JSON(http.StatusOK, gin.H{"providers": providers})
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
