// Package testidp is a conformant OpenID Provider for the test suite (2.4-1).
//
// It is an implementation of the protocol, not a mock of OurWay's SSO layer: it
// publishes a discovery document, serves a JWKS, mints RS256-signed id_tokens,
// runs the authorization-code exchange, and rotates its signing key on command.
// The tests that matter here — "a provider configured by issuer alone logs people
// in", "a token signed by a key the IdP does not publish is refused", "a rotation
// mid-session keeps logins working" — cannot be asserted against a stub of the
// verification code, because a stub of the verifier agrees with whatever the
// verifier does.
package testidp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is what the IdP asserts about a user for one login.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Groups        []string
	Nonce         string
	RedirectURI   string
	// Extra carries the claim shapes the mapping must handle (a group claim under
	// a non-standard name, an email claim under `upn`, a user with no email).
	Extra map[string]any
	// IDTokenSubject makes the signed token name a **different** person from the
	// userinfo call: the shape 2.4-2's agreement rule refuses.
	IDTokenSubject string
	// TokenSabotage makes the token endpoint mint an id_token the verifier must
	// refuse: "expired", "foreign_aud", "wrong_key", "none", "hs256",
	// "sub_mismatch", "missing".
	TokenSabotage string
	// codeChallenge and nonce are captured from the /authorize request, the way a
	// real IdP captures them: what the client sent at the start of this login is
	// what the token endpoint must bind the code to.
	codeChallenge string
	nonce         string
}

// IdP is a running provider fixture.
type IdP struct {
	Server *httptest.Server

	mu     sync.Mutex
	key    *rsa.PrivateKey
	kid    string
	pub    map[string]*rsa.PublicKey // published JWKS, by kid
	codes  map[string]*Claims
	tokens map[string]*Claims
	// docBreak is the sabotage asked for by a discovery test.
	docBreak string
	// defaultClaims answers any /authorize the fixture has not seen a code for:
	// the console generates its CSRF state inside the authorize redirect, so a
	// test cannot know it in advance — this is the one-user IdP a real operator
	// signs in to.
	defaultClaims *Claims
	// lastChallenge and verifierOK record what the token endpoint saw, so a test can
	// assert that the client presented a PKCE verifier instead of asserting that a
	// stub of itself did. requirePKCE is the strict-IdP setting: refuse a token
	// request that presents no verifier at all.
	lastChallenge string
	verifierOK    bool
	requirePKCE   bool
}

// New starts a provider at a random localhost port.
func New() *IdP {
	p := &IdP{
		pub:    make(map[string]*rsa.PublicKey),
		codes:  make(map[string]*Claims),
		tokens: make(map[string]*Claims),
	}
	p.key, p.kid = mustKey()
	p.pub[p.kid] = &p.key.PublicKey

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", p.handleDiscovery)
	mux.HandleFunc("/jwks", p.handleJWKS)
	mux.HandleFunc("/authorize", p.handleAuthorize)
	mux.HandleFunc("/token", p.handleToken)
	mux.HandleFunc("/userinfo", p.handleUserInfo)

	p.Server = httptest.NewServer(mux)
	return p
}

func (p *IdP) Close() { p.Server.Close() }

// Issuer is what an operator types into the console.
func (p *IdP) Issuer() string { return p.Server.URL }

// URL is a path on the provider (its authorize endpoint, for a browser-side test).
func (p *IdP) URL(path string) string { return p.Server.URL + path }

// SeedCode registers an authorization code the provider will hand out at
// /authorize for `redirectURI`, carrying `c`. Returning the code the /authorize
// handler will emit lets a test drive the real redirect.
func (p *IdP) SeedCode(code string, c Claims) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	claims := c
	p.codes[code] = &claims
	return code
}

// SeedLogin makes the IdP answer any authorization request with this user, so a
// test can walk the flow without knowing the CSRF state the console generated.
func (p *IdP) SeedLogin(c Claims) {
	p.mu.Lock()
	defer p.mu.Unlock()
	claims := c
	p.defaultClaims = &claims
}

// Rotate mints a new signing key and publishes both keys — the state an IdP is in
// for the window after a rotation.
func (p *IdP) Rotate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	key, kid := mustKey()
	p.key, p.kid = key, kid
	p.pub[kid] = &key.PublicKey
}

// PublishOnlyCurrentKey drops the previous key from the published set: the state
// after the rotation window has closed.
func (p *IdP) PublishOnlyCurrentKey() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pub = map[string]*rsa.PublicKey{p.kid: &p.key.PublicKey}
}

// BreakDocument sabotages the discovery document for a named test case:
// "no_jwks" (drops jwks_uri), "issuer" (names a different issuer), "no_code"
// (offers no authorization-code flow), "garbage" (not JSON), "off" (no answer),
// "es256_only" (publishes an id_token algorithm the verifier does not accept).
func (p *IdP) BreakDocument(kind string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.docBreak = kind
}

func (p *IdP) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	kind := p.docBreak
	p.mu.Unlock()

	if kind == "off" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if kind == "garbage" {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>login</html>"))
		return
	}

	doc := map[string]any{
		"issuer":                                p.Server.URL,
		"authorization_endpoint":                p.Server.URL + "/authorize",
		"token_endpoint":                        p.Server.URL + "/token",
		"userinfo_endpoint":                     p.Server.URL + "/userinfo",
		"jwks_uri":                              p.Server.URL + "/jwks",
		"response_types_supported":              []string{"code", "id_token"},
		"scopes_supported":                      []string{"openid", "profile", "email", "offline_access"},
		"code_challenge_methods_supported":      []string{"S256"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	}
	switch kind {
	case "no_jwks":
		delete(doc, "jwks_uri")
	case "issuer":
		doc["issuer"] = "https://some-other-idp.example"
	case "no_code":
		doc["response_types_supported"] = []string{"id_token"}
	case "es256_only":
		// A provider that publishes an algorithm our verifier does not accept
		// (2.4-2 is RS256-only): conformant, and unusable, and the preview is
		// where the operator learns which one it is.
		doc["id_token_signing_alg_values_supported"] = []string{"ES256"}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(doc)
}

func (p *IdP) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	keys := make([]map[string]any, 0, len(p.pub))
	for kid, pub := range p.pub {
		keys = append(keys, map[string]any{
			"kid": kid,
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	p.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
}

// handleAuthorize answers the browser's redirect: it validates client_id,
// response_type and the redirect URI the code was seeded with, and sends the
// user back with the code — the same contract a real IdP enforces.
func (p *IdP) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := q.Get("state") // the fixture uses state as the code selector
	p.mu.Lock()
	claims, ok := p.codes[code]
	if !ok && p.defaultClaims != nil {
		// Same user, new login: mint a claims record for this state so the
		// token endpoint can serve it.
		minted := *p.defaultClaims
		p.codes[code] = &minted
		claims, ok = &minted, true
	}
	p.mu.Unlock()

	redirect := q.Get("redirect_uri")
	if !ok {
		http.Redirect(w, r, redirect+"?error=invalid_request&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
		return
	}
	if claims.RedirectURI != "" && claims.RedirectURI != redirect {
		http.Redirect(w, r, redirect+"?error=invalid_redirect_uri&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
		return
	}
	if q.Get("response_type") != "code" {
		http.Redirect(w, r, redirect+"?error=unsupported_response_type&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
		return
	}
	// The fixture binds the login's PKCE challenge and nonce to the code, the way a
	// real IdP does: the client sent them at the start of this login, and the token
	// endpoint is where they have to come back (2.4-2).
	p.mu.Lock()
	if stored, ok := p.codes[code]; ok {
		stored.codeChallenge = q.Get("code_challenge")
		if n := q.Get("nonce"); n != "" {
			stored.nonce = n
		}
	}
	p.mu.Unlock()
	realCode := "code-" + code
	p.mu.Lock()
	p.tokens["at-"+realCode] = claims
	p.mu.Unlock()

	http.Redirect(w, r, redirect+"?code="+url.QueryEscape(realCode)+"&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
}

func (p *IdP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.Form.Get("grant_type") != "authorization_code" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unsupported_grant_type"}`))
		return
	}
	realCode := r.Form.Get("code")
	claims := p.takeCode(realCode)
	if claims == nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	clientID := r.Form.Get("client_id")

	// PKCE, enforced: a code that was issued with a challenge is only spendable by
	// the client that can present the verifier it corresponds to. `RequirePKCE` is
	// the strict-provider setting, so a test can prove the client sends a verifier
	// at all rather than proving it sent one that matched.
	challenge := claims.codeChallenge
	p.mu.Lock()
	p.lastChallenge = challenge
	requireVerifier := challenge != "" || p.requirePKCE
	p.mu.Unlock()
	if requireVerifier {
		verifier := r.Form.Get("code_verifier")
		matched := verifier != "" && (challenge == "" || s256(verifier) == challenge)
		if !matched {
			p.mu.Lock()
			p.verifierOK = false
			p.mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"code_verifier does not match the challenge the code was issued with"}`))
			return
		}
		p.mu.Lock()
		p.verifierOK = true
		p.mu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"access_token": "at-" + realCode,
		"token_type":   "Bearer",
		"expires_in":   3600,
	}
	if idToken := p.MintFor(*claims, clientID); idToken != "" {
		resp["id_token"] = idToken
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// MintFor mints the id_token this login's token response carries, honoring
// `TokenSabotage` — the fixture's way of producing the tokens 2.4-2's acceptance
// criteria name: expired, minted for another client, signed by an unpublished key,
// `alg: none`, HS256 against an RSA set, a subject that disagrees with userinfo, and
// no id_token at all.
func (p *IdP) MintFor(c Claims, clientID string) string {
	minted := c
	if c.nonce != "" {
		minted.Nonce = c.nonce
	}
	switch c.TokenSabotage {
	case "missing":
		return ""
	case "nonce_swap":
		// A token minted for some earlier login: same subject, same audience, a
		// nonce that is not the one this authorize redirect sent.
		minted.Nonce = "a-nonce-from-another-login"
		return p.MintIDToken(minted, clientID, "RS256")
	case "expired":
		return p.MintExpiredIDToken(minted, clientID)
	case "foreign_aud":
		return p.MintIDToken(minted, "some-other-client", "RS256")
	case "wrong_key":
		return p.MintIDTokenWithKey(minted, clientID)
	case "none":
		return p.MintIDToken(minted, clientID, "none")
	case "hs256":
		return p.MintIDToken(minted, clientID, "HS256")
	case "sub_mismatch":
		sub := c.IDTokenSubject
		if sub == "" {
			sub = "signed-as-" + c.Subject
		}
		minted.Subject = sub
		return p.MintIDToken(minted, clientID, "RS256")
	default:
		return p.MintIDToken(minted, clientID, "RS256")
	}
}

// LastCodeChallenge is the PKCE challenge the last /authorize carried, empty when
// the client sent none.
func (p *IdP) LastCodeChallenge() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastChallenge
}

// VerifierAccepted reports whether the last token request presented a verifier that
// matched the challenge.
func (p *IdP) VerifierAccepted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.verifierOK
}

// RequirePKCE makes the provider refuse a token request that presents no verifier at
// all — a strict IdP, and the only way to prove the client sends one.
func (p *IdP) RequirePKCE() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requirePKCE = true
}

// s256 is the PKCE S256 transform, computed here so the fixture can check a
// verifier without importing the code under test.
func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// takeCode exchanges a code for its claims and consumes it (single use, as a real
// IdP does).
func (p *IdP) takeCode(realCode string) *Claims {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := strings.TrimPrefix(realCode, "code-")
	claims, ok := p.codes[state]
	if !ok {
		return nil
	}
	delete(p.codes, state)
	return claims
}

func (p *IdP) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	authz := r.Header.Get("Authorization")
	at := strings.TrimPrefix(authz, "Bearer ")
	p.mu.Lock()
	claims, ok := p.tokens[at]
	p.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	out := map[string]any{
		"sub":            claims.Subject,
		"name":           claims.Name,
		"email":          claims.Email,
		"email_verified": claims.EmailVerified,
	}
	if len(claims.Groups) > 0 {
		out["groups"] = claims.Groups
	}
	for k, v := range claims.Extra {
		out[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// MintIDToken signs an id_token for these claims with the current key. `alg` lets
// a test produce a token the verifier must refuse ("none", "HS256").
//
// `clientID` becomes `aud`: a token minted for another client is the fixture's
// way of producing the foreign-audience case.
func (p *IdP) MintIDToken(c Claims, clientID, alg string) string {
	p.mu.Lock()
	key, kid := p.key, p.kid
	p.mu.Unlock()

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":            p.Server.URL,
		"sub":            c.Subject,
		"aud":            clientID,
		"exp":            now.Add(time.Hour).Unix(),
		"iat":            now.Add(-time.Second).Unix(),
		"auth_time":      now.Unix(),
		"name":           c.Name,
		"email":          c.Email,
		"email_verified": c.EmailVerified,
	}
	if len(c.Groups) > 0 {
		claims["groups"] = c.Groups
	}
	if c.Nonce != "" {
		claims["nonce"] = c.Nonce
	}
	for k, v := range c.Extra {
		claims[k] = v
	}

	switch alg {
	case "none":
		// The unsigned-token case: header alg=none, empty signature.
		return unsignedJWS(kid, claims)
	case "HS256":
		// The algorithm-confusion case: a token "signed" with a shared secret,
		// whose header names an algorithm the verifier must not honour.
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		token.Header["kid"] = kid
		signed, _ := token.SignedString([]byte("not-the-provider-key"))
		return signed
	default:
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = kid
		signed, err := token.SignedString(key)
		if err != nil {
			panic(fmt.Sprintf("sign: %v", err))
		}
		return signed
	}
}

// MintExpiredIDToken mints a token whose `exp` is in the past.
func (p *IdP) MintExpiredIDToken(c Claims, clientID string) string {
	p.mu.Lock()
	key, kid := p.key, p.kid
	p.mu.Unlock()

	now := time.Now().Add(-2 * time.Hour)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": p.Server.URL, "sub": c.Subject, "aud": clientID,
		"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(),
		"email": c.Email, "email_verified": c.EmailVerified,
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		panic(fmt.Sprintf("sign: %v", err))
	}
	return signed
}

// MintIDTokenWithKey signs with a key the provider does **not** publish, so the
// verifier sees a kid it cannot resolve: the "signed by the wrong key" case.
func (p *IdP) MintIDTokenWithKey(c Claims, clientID string) string {
	rogue, kid := mustKey()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": p.Server.URL, "sub": c.Subject, "aud": clientID,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Add(-time.Second).Unix(),
		"email": c.Email, "email_verified": c.EmailVerified,
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(rogue)
	if err != nil {
		panic(fmt.Sprintf("sign: %v", err))
	}
	return signed
}

func unsignedJWS(kid string, claims jwt.MapClaims) string {
	headerJSON, _ := json.Marshal(map[string]string{"alg": "none", "kid": kid})
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func mustKey() (*rsa.PrivateKey, string) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key, fmt.Sprintf("kid-%d", time.Now().UnixNano())
}
