package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
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

	// Store state in session cookie. Secure=true: this is a CSRF state
	// cookie on a flow that must not be replayable over plaintext HTTP.
	c.SetCookie("sso_state", state, 300, "/", "", true, true)

	redirectURI := h.redirectURIFor(provider)
	authorizeURL := h.oauth.AuthorizeURL(provider, redirectURI, state)

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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}
	c.SetCookie("sso_state", "", -1, "/", "", true, true)

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing authorization code"})
		return
	}

	redirectURI := h.redirectURIFor(provider)
	token, refreshToken, err := h.oauth.HandleCallback(c.Request.Context(), provider, code, redirectURI)
	if err != nil {
		log.Printf("sso: callback failed for provider %s: %v", providerName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "SSO login failed"})
		return
	}

	loginCode, err := h.storePendingToken(token, refreshToken)
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
	if strings.TrimSpace(provider.Issuer) == "" {
		// No issuer means no discovery, which is the pre-2.4 shape: an operator
		// who typed every endpoint by hand. Accepted, and stated as such by the
		// empty `discovered_at`.
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

	set := func(target *string, discovered, requestedValue string) {
		if strings.TrimSpace(requestedValue) != "" {
			*target = requestedValue
			return
		}
		if strings.TrimSpace(discovered) != "" {
			*target = discovered
		}
	}
	set(&provider.AuthURL, doc.AuthorizationEndpoint, requestedAuthURL)
	set(&provider.TokenURL, doc.TokenEndpoint, requestedTokenURL)
	set(&provider.UserInfoURL, doc.UserInfoEndpoint, requestedUserInfoURL)
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
func (h *SSOHandler) redirectURIFor(provider *models.SSOProvider) string {
	if strings.TrimSpace(provider.RedirectURI) != "" {
		return provider.RedirectURI
	}
	return h.redirect + "/api/auth/sso/" + provider.Name + "/callback"
}

// CreateProvider creates or updates an SSO provider (admin).
//
// The row is saved only if the provider proved itself: an issuer is discovered at
// save time, and a discovery failure answers a named 400 and writes nothing
// (2.4-1's acceptance criterion — "no half-saved provider row"). The endpoints the
// flow will call are the ones the IdP published, unless the operator named them.
func (h *SSOHandler) CreateProvider(c *gin.Context) {
	var req struct {
		Type                 string `json:"type" binding:"required"`
		Name                 string `json:"name" binding:"required"`
		ClientID             string `json:"client_id" binding:"required"`
		ClientSecret         string `json:"client_secret"`
		Issuer               string `json:"issuer"`
		DiscoveryURL         string `json:"discovery_url"`
		RedirectURI          string `json:"redirect_uri"`
		AuthURL              string `json:"auth_url"`
		TokenURL             string `json:"token_url"`
		UserInfoURL          string `json:"user_info_url"`
		Scope                string `json:"scope"`
		ClaimsEmail          string `json:"claims_email"`
		ClaimsUsername       string `json:"claims_username"`
		ClaimsName           string `json:"claims_name"`
		ClaimsGroups         string `json:"claims_groups"`
		AllowedDomains       string `json:"allowed_domains"`
		RequireVerifiedEmail *bool  `json:"require_verified_email"`
		LinkPolicy           string `json:"link_policy"`
		RoleMapping          string `json:"role_mapping"`
		SortOrder            *int   `json:"sort_order"`
		Enabled              *bool  `json:"enabled"`
	}

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

	// The provider name is a URL path segment (`/api/auth/sso/<name>/callback`),
	// so it is validated as one. A name with a slash, a dot-dot, or a space in it
	// is a provider whose callback URL is not the URL the operator registered
	// with the IdP.
	if !providerNameIsSlug(req.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider name must be a slug: letters, digits and dashes (it is used in the callback URL)"})
		return
	}

	// 2.4-3's knobs, validated at the door rather than at the callback.
	if req.LinkPolicy != "" && req.LinkPolicy != models.SSOLinkPolicyPending && req.LinkPolicy != models.SSOLinkPolicyLink {
		c.JSON(http.StatusBadRequest, gin.H{"error": "link_policy must be pending or link"})
		return
	}
	if msg, ok := validRoleMapping(req.RoleMapping); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	if msg, ok := validDomains(req.AllowedDomains); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}

	existing, err := h.store.SSOProviders.GetByName(req.Name)
	var provider *models.SSOProvider
	if err == nil && existing != nil {
		provider = existing
	} else {
		provider = &models.SSOProvider{ID: uuid.New().String(), Name: req.Name, Enabled: true}
	}

	provider.Type = req.Type
	provider.ClientID = req.ClientID
	if req.ClientSecret != "" {
		provider.ClientSecret = req.ClientSecret
	}
	issuer, presetScope := providerPreset(req.Name)
	if strings.TrimSpace(req.Issuer) != "" {
		provider.Issuer = strings.TrimSpace(req.Issuer)
	} else if strings.TrimSpace(provider.Issuer) == "" {
		provider.Issuer = issuer
	}
	provider.DiscoveryURL = strings.TrimSpace(req.DiscoveryURL)
	provider.RedirectURI = strings.TrimSpace(req.RedirectURI)
	if req.Scope != "" {
		provider.Scope = req.Scope
	} else if provider.Scope == "" {
		provider.Scope = presetScope
	}
	provider.ClaimsEmail = req.ClaimsEmail
	provider.ClaimsUsername = req.ClaimsUsername
	provider.ClaimsName = req.ClaimsName
	provider.ClaimsGroups = req.ClaimsGroups
	provider.AllowedDomains = strings.ToLower(strings.TrimSpace(req.AllowedDomains))
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
	provider.RoleMapping = req.RoleMapping
	if req.SortOrder != nil {
		provider.SortOrder = *req.SortOrder
	}
	if req.Enabled != nil {
		provider.Enabled = *req.Enabled
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
	defer auditForAnswer(c, h.store, models.AuditSSOCreate, AuditTarget{Type: "sso-provider", ID: provider.ID}, map[string]any{
		"name":   provider.Name,
		"type":   provider.Type,
		"issuer": provider.Issuer,
	})

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
