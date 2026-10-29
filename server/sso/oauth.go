package sso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/store"
)

// OAuthHandler handles OAuth 2.0 authentication flows.
type OAuthHandler struct {
	store   *store.Store
	jwtAuth *auth.JWTAuth
	client  *http.Client
	// jwks is what an `id_token` is verified against (2.4-2): the key set comes
	// from the provider's published `jwks_uri`, never from a config file.
	jwks *JWKSClient
}

// NewOAuthHandler creates a new OAuth handler.
func NewOAuthHandler(store *store.Store, jwtAuth *auth.JWTAuth) *OAuthHandler {
	client := DefaultHTTPClient()
	return &OAuthHandler{
		store:   store,
		jwtAuth: jwtAuth,
		client:  client,
		jwks:    NewJWKSClient(client),
	}
}

// Identity refusal reasons (2.4-2). They are named because the console and the
// audit ledger have to say **which rule** refused a login: "SSO login failed" is
// the message an operator cannot act on, and the five cases below are five
// different fixes on the IdP side.
const (
	ReasonCodeExchange        = "code_exchange_failed"
	ReasonUserInfoFailed      = "userinfo_failed"
	ReasonMissingIDToken      = "missing_id_token"
	ReasonIDTokenVerification = "id_token_verification_failed"
	ReasonSubjectMismatch     = "subject_mismatch"
	ReasonNonceMismatch       = "nonce_mismatch"
	ReasonNoIdentity          = "no_identity_claim"
	ReasonStateMismatch       = "state_mismatch"
	ReasonStateExpired        = "state_expired"
)

// IdentityError carries the refusal reason beside the message, so the callback
// answers a named state and writes it into the ledger. `Subject`/`Email` are filled
// when the refusal happened *after* an identity was asserted — the audit question
// 2.4-3's headline test asks is not "was a login refused" but "who tried to arrive
// as whom".
type IdentityError struct {
	Reason  string
	Subject string
	Email   string
	Err     error
}

func (e *IdentityError) Error() string { return e.Reason + ": " + e.Err.Error() }
func (e *IdentityError) Unwrap() error { return e.Err }

// LoginOutcome is what a completed callback produces: the session tokens, the
// account the identity maps to, and the claims the product recorded. `Verified`
// is not "the provider sent an email claim" — it is "the email came from a
// verified `id_token`", which is the fact 2.4-3's contract acts on.
type LoginOutcome struct {
	AccessToken   string
	RefreshToken  string
	User          *models.User
	Subject       string
	Email         string
	EmailVerified bool
	// VerifiedByIDToken says the identity was checked against the provider's
	// published keys. False means the provider has no `jwks_uri` (no issuer: an
	// operator typed the endpoints), and identity rests on the userinfo call.
	VerifiedByIDToken bool
}

// NewCodeVerifier builds a PKCE code verifier: 32 random bytes, base64url, inside
// the 43–128 character range the spec sets.
func NewCodeVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// CodeChallenge is the S256 challenge a verifier corresponds to.
func CodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthorizeURL generates the OAuth 2.0 authorization URL. `nonce` and
// `codeChallenge` are added when the flow uses them (2.4-2): the nonce binds the
// `id_token` to *this* login, and the challenge is the PKCE half of the code flow.
func (h *OAuthHandler) AuthorizeURL(provider *models.SSOProvider, redirectURI, state, nonce, codeChallenge string) string {
	scope := provider.Scope
	if scope == "" {
		scope = "openid profile email"
	}

	params := url.Values{}
	params.Set("client_id", provider.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", scope)
	params.Set("state", state)
	if codeChallenge != "" {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", "S256")
	}
	if nonce != "" {
		params.Set("nonce", nonce)
	}

	return provider.AuthURL + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for an access token. The verifier
// is sent when the authorization request carried a challenge: an IdP that was
// given one must refuse a token request without it, which is the whole point.
func (h *OAuthHandler) ExchangeCode(provider *models.SSOProvider, code, redirectURI, codeVerifier string) (*OAuthTokens, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", provider.ClientID)
	form.Set("client_secret", provider.ClientSecret)
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}

	resp, err := h.client.PostForm(provider.TokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token request returned %d", resp.StatusCode)
	}

	var tokenResp OAuthTokens
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &tokenResp, nil
}

// GetUserInfo retrieves user info from the OAuth provider.
func (h *OAuthHandler) GetUserInfo(provider *models.SSOProvider, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequest("GET", provider.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user info request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("user info request returned %d", resp.StatusCode)
	}

	// The userinfo body is another machine's answer about a person: size-capped, and
	// kept, because the mapped group claim is read out of it as published.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading user info: %w", err)
	}

	var info UserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	// Normalize fields based on provider type
	switch provider.Type {
	case "google":
		if info.Name == "" {
			info.Name = info.Email
		}
	case "microsoft":
		if info.Email == "" {
			info.Email = info.UserPrincipalName
		}
		if info.Name == "" {
			info.Name = info.Email
		}
	}
	// The mapped group claim, from the source that answered. The userinfo shape is
	// not typed, so it is decoded here the same way the claims are.
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err == nil {
		info.Groups = claimStrings(raw, groupsClaim(provider))
	}

	return &info, nil
}

// OAuthTokens represents OAuth 2.0 tokens.
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
}

// UserInfo represents user info from an OAuth provider.
type UserInfo struct {
	Subject           string `json:"sub"`
	Name              string `json:"name"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Picture           string `json:"picture"`
	UserPrincipalName string `json:"upn"`
	// Groups is the mapped group claim. It is what 2.4-3's role mapping reads, so
	// it is carried from both identity sources, not invented from a name.
	Groups []string `json:"groups,omitempty"`
}

// HandleCallback completes the authorization-code login: exchange the code, verify
// the `id_token` the provider signed, require that the userinfo call and the verified
// token agree on who the person is, and only then hand the identity to the account
// lookup.
//
// The order is the security property. Before 2.4-2 identity came from the userinfo
// call alone — a bearer token whose subject nobody had checked against anything the
// provider signed. `nonce` and `codeVerifier` come from the server-side state record
// the authorize redirect created; an empty `nonce` means this login carries no
// id_token binding, which only a provider with no `jwks_uri` gets away with.
func (h *OAuthHandler) HandleCallback(ctx context.Context, provider *models.SSOProvider, code, redirectURI, nonce, codeVerifier string) (*LoginOutcome, error) {
	tokens, err := h.ExchangeCode(provider, code, redirectURI, codeVerifier)
	if err != nil {
		return nil, &IdentityError{Reason: ReasonCodeExchange, Err: err}
	}

	// A provider with an issuer is an OIDC provider, and 2.4-2's rule is that an
	// identity claim is trusted only after `iss`/`aud`/`exp` and the signature have
	// been checked against the keys that issuer publishes. No issuer means the
	// operator typed the endpoints by hand: there is no key set to check against,
	// and the code says so in the login record rather than pretending otherwise.
	verifies := strings.TrimSpace(provider.Issuer) != "" && strings.TrimSpace(provider.JwksURI) != ""

	// userinfo first, verification before trust. Nothing asserted here becomes an
	// identity until the signed token has been checked: the order is what lets the
	// refusal name *who tried to arrive as whom*, which is the question the ledger is
	// read for. A verification failure that records only "verification failed" is a
	// row nobody can act on.
	var info *UserInfo
	var userinfoErr error
	if strings.TrimSpace(provider.UserInfoURL) != "" && tokens.AccessToken != "" {
		info, userinfoErr = h.GetUserInfo(provider, tokens.AccessToken)
	}

	refuse := func(reason string, cause error, claims map[string]any) (*LoginOutcome, error) {
		verr := &IdentityError{Reason: reason, Err: cause}
		if info != nil {
			verr.Subject, verr.Email = info.Subject, info.Email
		}
		if verr.Subject == "" && claims != nil {
			verr.Subject = claimString(claims, "sub")
			verr.Email = claimString(claims, emailClaim(provider))
		}
		return nil, verr
	}

	var claims map[string]any
	if verifies {
		if tokens.IDToken == "" {
			return refuse(ReasonMissingIDToken,
				fmt.Errorf("provider %s published a jwks_uri and answered the token request with no id_token", provider.Name), nil)
		}
		verified, err := h.jwks.VerifyIDToken(provider.JwksURI, provider.Issuer, provider.ClientID, tokens.IDToken)
		if err != nil {
			return refuse(ReasonIDTokenVerification, err, nil)
		}
		claims = verified
		if nonce != "" {
			if got := claimString(claims, "nonce"); got != nonce {
				// A token minted for an earlier login must not satisfy this one.
				return refuse(ReasonNonceMismatch,
					fmt.Errorf("id_token nonce is %q, this login sent %q", got, nonce), claims)
			}
		}
	}

	if info == nil && userinfoErr != nil && claims == nil {
		return refuse(ReasonUserInfoFailed, userinfoErr, nil)
	}
	if info == nil {
		// Either there is no userinfo endpoint, or the call failed and the verified
		// `id_token` stands on its own — which is what the OIDC code flow allows,
		// and the reason the token is verified before anything is trusted.
		if claims == nil {
			return refuse(ReasonNoIdentity,
				fmt.Errorf("provider %s published no usable identity: userinfo %v", provider.UserInfoURL, userinfoErr), nil)
		}
		info = userInfoFromClaims(provider, claims)
	} else if claims != nil {
		// Both sources name a subject. Agreement is required: a provider whose
		// userinfo says one person and whose signed token says another is either
		// misconfigured or under someone's control, and OurWay does not pick which.
		if info.Subject == "" {
			info.Subject = claimString(claims, "sub")
		} else if claimString(claims, "sub") != "" && info.Subject != claimString(claims, "sub") {
			return nil, &IdentityError{Reason: ReasonSubjectMismatch, Subject: info.Subject, Email: info.Email,
				Err: fmt.Errorf("userinfo says %q, the signed id_token says %q", info.Subject, claimString(claims, "sub"))}
		}
		// The signed token is the stronger source for the email facts: userinfo is
		// a call, the id_token is a claim the provider signed.
		if email := claimString(claims, emailClaim(provider)); email != "" {
			info.Email = email
		}
		info.EmailVerified = claimBool(claims, "email_verified")
		if info.Name == "" {
			info.Name = claimString(claims, nameClaim(provider))
		}
		if len(info.Groups) == 0 {
			info.Groups = claimStrings(claims, groupsClaim(provider))
		}
	}

	if info.Subject == "" && info.Email == "" {
		return nil, &IdentityError{Reason: ReasonNoIdentity, Email: info.Email,
			Err: fmt.Errorf("the provider asserted neither a subject nor an email")}
	}

	user, err := h.findOrCreateUser(ctx, provider, info)
	if err != nil {
		return nil, err
	}

	roles := h.rolesOf(user)

	token, err := h.jwtAuth.GenerateToken(user.ID, user.Username, roles)
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT: %w", err)
	}

	refreshToken, err := h.jwtAuth.GenerateRefreshToken(user.ID, user.Username, roles)
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return &LoginOutcome{
		AccessToken:       token,
		RefreshToken:      refreshToken,
		User:              user,
		Subject:           info.Subject,
		Email:             info.Email,
		EmailVerified:     info.EmailVerified,
		VerifiedByIDToken: claims != nil,
	}, nil
}

// rolesOf is the role set a session is issued with. A user with no role rows is
// the `viewer` floor and nothing more — the floor is recorded on JIT accounts, and
// a missing row must never widen access.
func (h *OAuthHandler) rolesOf(user *models.User) []string {
	if len(user.Roles) == 0 {
		return []string{"viewer"}
	}
	roles := make([]string, 0, len(user.Roles))
	for _, role := range user.Roles {
		roles = append(roles, role.Name)
	}
	return roles
}

// emailClaim, nameClaim and usernameClaim are the claim mapping the provider row
// carries. IdPs do not agree on the claim names — Entra's UPN is not Google's
// `email`, and a home IdP's group claim is whatever its admin typed — so the
// operator names them, and the default is the OIDC standard name.
func emailClaim(provider *models.SSOProvider) string {
	if strings.TrimSpace(provider.ClaimsEmail) != "" {
		return strings.TrimSpace(provider.ClaimsEmail)
	}
	return "email"
}

func nameClaim(provider *models.SSOProvider) string {
	if strings.TrimSpace(provider.ClaimsName) != "" {
		return strings.TrimSpace(provider.ClaimsName)
	}
	return "name"
}

func usernameClaim(provider *models.SSOProvider) string {
	if strings.TrimSpace(provider.ClaimsUsername) != "" {
		return strings.TrimSpace(provider.ClaimsUsername)
	}
	return "preferred_username"
}

func groupsClaim(provider *models.SSOProvider) string {
	if strings.TrimSpace(provider.ClaimsGroups) != "" {
		return strings.TrimSpace(provider.ClaimsGroups)
	}
	return "groups"
}

// claimStrings reads a claim that is a string array (a group claim), tolerating the
// single-string shape some IdPs publish for a one-member group.
func claimStrings(claims map[string]any, key string) []string {
	if key == "" {
		return nil
	}
	switch v := claims[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	}
	return nil
}

func claimString(claims map[string]any, key string) string {
	if key == "" {
		return ""
	}
	if v, ok := claims[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func claimBool(claims map[string]any, key string) bool {
	v, ok := claims[key]
	if !ok {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// userInfoFromClaims builds the identity record from a **verified** id_token, for
// the providers that answer the code flow with a token and no (or a failed)
// userinfo call. `email_verified` is read from the claims the provider signed.
func userInfoFromClaims(provider *models.SSOProvider, claims map[string]any) *UserInfo {
	info := &UserInfo{
		Subject:       claimString(claims, "sub"),
		Name:          claimString(claims, nameClaim(provider)),
		Email:         claimString(claims, emailClaim(provider)),
		EmailVerified: claimBool(claims, "email_verified"),
		Groups:        claimStrings(claims, groupsClaim(provider)),
	}
	if info.Name == "" {
		info.Name = claimString(claims, usernameClaim(provider))
	}
	return info
}

// HandleCallback's predecessor lived in the API layer's shape: a token pair and
// nothing about the identity behind it. `findOrCreateUser` implements JIT
// provisioning.
func (h *OAuthHandler) findOrCreateUser(ctx context.Context, provider *models.SSOProvider, info *UserInfo) (*models.User, error) {
	// Try to find by provider ID first
	if info.Subject != "" {
		user, err := h.store.Users.FindByProvider(provider.Name, info.Subject)
		if err == nil && user != nil {
			return user, nil
		}
	}

	// Try to find by email
	if info.Email != "" {
		user, err := h.store.Users.FindByEmail(info.Email)
		if err == nil && user != nil {
			// Update with provider info
			user.Provider = provider.Name
			user.ProviderID = info.Subject
			h.store.Users.Update(user)
			return user, nil
		}
	}

	// Create new user (JIT provisioning)
	email := info.Email
	if email == "" {
		email = info.Subject + "@" + provider.Name + ".sso"
	}

	username := email
	if idx := strings.Index(email, "@"); idx > 0 {
		username = email[:idx]
	}

	user := &models.User{
		ID:           uuid.New().String(),
		Username:     username,
		Email:        email,
		PasswordHash: "",
		Provider:     provider.Name,
		ProviderID:   info.Subject,
	}

	// The raw `id_token` is not stored (2.4-2). It is an access credential to
	// another system — one that expires, and that a dump of `users` would hand to
	// anyone holding a database read. What the product needs from it is the claim
	// summary, written as JSON the code can read back, not a string-concatenated
	// fragment of a token.
	user.SSOAttributes = ssoAttributeSummary(provider.Name, info)

	// The JIT floor is a role, recorded on the account: `HandleCallback` has always
	// issued a `viewer` token for a new SSO user, and an account with no role rows
	// is an account whose access is decided at login time by whoever calls it
	// next. Recording the floor is what 2.4-3's pending state means.
	if err := h.store.Users.Create(user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	if err := h.grantFloorRole(user.ID); err != nil {
		return nil, fmt.Errorf("failed to record the JIT floor role: %w", err)
	}

	return user, nil
}

// ssoAttributeSummary is the claim record the product keeps: who the provider
// asserted, and what it said about the email. Structured JSON, so a reader can
// tell a verified email from an asserted one.
func ssoAttributeSummary(providerName string, info *UserInfo) string {
	type summary struct {
		Provider      string   `json:"provider"`
		ProviderID    string   `json:"provider_id"`
		Name          string   `json:"name,omitempty"`
		Email         string   `json:"email,omitempty"`
		EmailVerified bool     `json:"email_verified"`
		Groups        []string `json:"groups,omitempty"`
	}
	encoded, err := json.Marshal(summary{
		Provider:      providerName,
		ProviderID:    info.Subject,
		Name:          info.Name,
		Email:         info.Email,
		EmailVerified: info.EmailVerified,
		Groups:        info.Groups,
	})
	if err != nil {
		return ""
	}
	return string(encoded)
}

// grantFloorRole gives a JIT user the `viewer` role — the access the console has
// always given a new SSO account, now on the account itself.
func (h *OAuthHandler) grantFloorRole(userID string) error {
	role, err := h.store.Roles.GetRoleByName("viewer")
	if err != nil {
		return err
	}
	return h.store.UserRoles.AssignRole(userID, role.ID)
}
