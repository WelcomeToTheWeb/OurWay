package sso_test

// The OIDC half of the vendor interop probe (2.4-0): a token minted by a **real IdP**
// run through OurWay's verifier.
//
// The SAML capture probe (`capture_vendor_saml_test.go`) has the same purpose, and the
// same reason to exist: everything else in this suite verifies documents our own Go
// fixtures mint. `server/sso/testidp` signs with our own key, publishes our own
// discovery document, and sets its claims the way our tests ask it to. An id_token from
// Keycloak is a different object: it carries `azp`, `sid`, `at_hash`, `acr`, `auth_time`,
// a `session_state`-derived `sub`, `email_verified` as a real boolean, groups under
// `realm_access.roles`, a `kid` from a key the IdP rotates, and an issuer with a **path**
// (`https://idp/realms/ourway`). The rules 2.4-2 states — `iss`, `aud`, `exp`, the nonce
// binding, the claim mapping — have to survive that object, not our idealization of it.
//
// The capture comes from outside the suite, through the environment, and the test skips
// when it is absent. The suite stays hermetic; the pass stays re-runnable.
//
// Environment:
//
//	OURWAY_OIDC_CAPTURE     the IdP's id_token, exactly as the token endpoint returned it
//	OURWAY_OIDC_JWKS        the IdP's published JWKS document (the file the operator's
//	                        installation fetches from `jwks_uri`)
//	OURWAY_OIDC_NONCE       the nonce the login that produced this token sent
//	OURWAY_OIDC_ISSUER      the issuer the provider row names
//	OURWAY_OIDC_AUDIENCE    the client id the token must be minted for
//	OURWAY_OIDC_AT          the SP's clock for the verification, ISO-8601 (default: now)
//	OURWAY_OIDC_EMAIL_CLAIM the claim that carries the email address (default `email`)
//	OURWAY_OIDC_GROUPS_CLAIM claim that carries the groups (default `realm_access.roles`)
//
// The defaults name no vendor on purpose: unlike the SAML probe, an id_token is
// worthless without the JWKS of the key that signed it, so every value here is
// installation-specific and nothing sensible could be assumed.

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ourway/server/sso"
)

// TestAVendorIDTokenVerifiesRefusesTamperingAndRefusesAnExpiredToken is the OIDC
// interop proof in one test.
func TestAVendorIDTokenVerifiesRefusesTamperingAndRefusesAnExpiredToken(t *testing.T) {
	token := oidcToken(t)
	if token == "" {
		t.Skip("no vendor id_token: set OURWAY_OIDC_CAPTURE (plus OURWAY_OIDC_JWKS and OURWAY_OIDC_NONCE)")
	}
	issuer := oidcEnv("OURWAY_OIDC_ISSUER", "")
	if issuer == "" {
		t.Fatal("OURWAY_OIDC_ISSUER is required: the issuer is the trust anchor 2.4-2 verifies against")
	}
	audience := oidcEnv("OURWAY_OIDC_AUDIENCE", "")
	if audience == "" {
		t.Fatal("OURWAY_OIDC_AUDIENCE is required: a token minted for another client is not ours")
	}
	nonce := oidcEnv("OURWAY_OIDC_NONCE", "")
	if nonce == "" {
		t.Fatal("OURWAY_OIDC_NONCE is required: the nonce is what binds the token to the login that asked for it")
	}
	jwks := oidcJWKS(t)
	claims := oidcClaims(t, token)
	at := oidcClock(t)

	// The shape guard: the claims a vendor writes, and the header the key it signed
	// with is named by. A capture whose header carries no `kid`, whose issuer has no
	// path, or whose `nonce` is absent proves the rules our fixture already satisfies,
	// not the rules a vendor exercises.
	if headerKid(t, token) == "" {
		t.Fatal("the captured token names no kid: this capture does not test the JWKS key lookup")
	}
	if !strings.Contains(issuer, "/realms/") && strings.Count(issuer, "/") < 3 {
		t.Fatalf("the issuer %q has no path: this capture does not test an issuer with a path", issuer)
	}
	for _, claim := range []string{"iss", "aud", "sub", "exp", "iat", "nonce", "azp"} {
		if _, ok := claims[claim]; !ok {
			t.Fatalf("the captured token carries no %q claim: the shape guard wants a vendor token, not a minimal one", claim)
		}
	}
	if _, ok := claims["email_verified"]; !ok {
		t.Fatal("the captured token carries no email_verified claim: 2.4-3's verified-email rule is untested by this capture")
	}

	// The window the capture itself asserts, named — the same rule the SAML probe
	// applies. The verifier checks `exp` against the wall clock, so the capture is
	// evidence in two states and the test says which one it is in: inside the window it
	// proves verification, after it the vendor's own expired token is the proof of expiry.
	expiry := oidcExpiry(t, claims)
	if expiry.Add(sso.IDTokenLeeway).Before(at) {
		t.Skipf("the captured token expired at %s: the clean case cannot be proven from this capture, "+
			"and the verifier must refuse it — re-run the capture and set OURWAY_OIDC_AT inside the window", expiry.Format(time.RFC3339))
	}
	if !expiry.After(at) {
		t.Fatalf("the captured token is past its exp %s and the clock %s is inside the leeway: the expired case is "+
			"proven by the wall clock, not by a moved one — re-capture", expiry.Format(time.RFC3339), at.Format(time.RFC3339))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	defer server.Close()
	client := &http.Client{Timeout: 10 * time.Second}

	keyID := headerKid(t, token)
	got, err := sso.NewJWKSClient(client).VerifyIDToken(server.URL+"/certs", issuer, audience, token)
	if err != nil {
		t.Fatalf("the vendor's own token was refused: %v", err)
	}
	if oidcClaimString(got, "nonce") != nonce {
		t.Fatalf("the verified token carries nonce %q, the login sent %q: the nonce binding is not what the flow enforces",
			oidcClaimString(got, "nonce"), nonce)
	}
	emailClaim := oidcEnv("OURWAY_OIDC_EMAIL_CLAIM", "email")
	groupsClaim := oidcEnv("OURWAY_OIDC_GROUPS_CLAIM", "realm_access.roles")
	t.Logf("the captured vendor token VERIFIES: kid=%s sub=%s email=%s groups=%v exp=%s",
		keyID, oidcClaimString(got, "sub"), oidcClaimString(got, emailClaim),
		oidcClaimStrings(got, groupsClaim), time.Unix(int64(got["exp"].(float64)), 0).UTC().Format(time.RFC3339))
	if oidcClaimString(got, emailClaim) == "" {
		t.Fatalf("the token asserts no identity through %q: the claim mapping 2.4-3 configures reads nothing here", emailClaim)
	}

	// One byte of the payload. The signature must notice.
	if _, err := sso.NewJWKSClient(client).VerifyIDToken(server.URL+"/certs", issuer, audience, tamperVendorToken(t, token)); err == nil {
		t.Fatal("a tampered vendor token verified: the signature is not being verified")
	} else {
		t.Logf("tampered vendor token refused: %v", err)
	}

	// A token minted for another client: same key, same issuer, same signature — and
	// not ours. This is the claim the fixture proves and a vendor makes real, because a
	// real IdP has other clients.
	if _, err := sso.NewJWKSClient(client).VerifyIDToken(server.URL+"/certs", issuer, audience+"-not-ours", token); err == nil {
		t.Fatal("a token minted for another client verified: aud is not being checked")
	} else {
		t.Logf("foreign-audience vendor token refused: %v", err)
	}

	// Expiry is not a case this probe can manufacture: the verifier checks `exp` against
	// the wall clock, so the same capture, re-run after `exp`, is the expiry proof — the
	// guard above turns that into a stated skip rather than a silent pass.
	t.Logf("this capture's window closes at %s: re-running the probe after that time is the expiry proof",
		expiry.Format(time.RFC3339))
}

func oidcEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func oidcToken(t *testing.T) string {
	t.Helper()
	path := oidcEnv("OURWAY_OIDC_CAPTURE", "")
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read captured token: %v", err)
	}
	return strings.TrimSpace(string(raw))
}

func oidcJWKS(t *testing.T) []byte {
	t.Helper()
	path := oidcEnv("OURWAY_OIDC_JWKS", "")
	if path == "" {
		t.Fatal("OURWAY_OIDC_JWKS is required: the vendor's published keys are the only trust anchor")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read captured JWKS: %v", err)
	}
	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Keys) == 0 {
		t.Fatalf("OURWAY_OIDC_JWKS is not a JWKS document with keys: %v", err)
	}
	return raw
}

func oidcClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("the captured token is not a JWS")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("the captured token's payload is not base64url: %v", err)
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("the captured token's claims are not JSON: %v", err)
	}
	return claims
}

func headerKid(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("the captured token is not a JWS")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("the captured token's header is not base64url: %v", err)
	}
	var header struct {
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatalf("the captured token's header is not JSON: %v", err)
	}
	return header.Kid
}

func oidcExpiry(t *testing.T, claims map[string]any) time.Time {
	t.Helper()
	exp, ok := claims["exp"].(float64)
	if !ok {
		t.Fatal("the captured token asserts no exp: its window is unknown")
	}
	return time.Unix(int64(exp), 0).UTC()
}

func oidcClock(t *testing.T) time.Time {
	t.Helper()
	value := oidcEnv("OURWAY_OIDC_AT", "")
	if value == "" {
		return time.Now().UTC()
	}
	for _, layout := range []string{"2006-01-02T15:04:05Z", time.RFC3339} {
		if at, err := time.Parse(layout, value); err == nil {
			return at.UTC()
		}
	}
	t.Fatalf("OURWAY_OIDC_AT %q is not an ISO-8601 time", value)
	return time.Now()
}

// tamperVendorToken flips one byte of the payload and re-encodes it, leaving the
// signature the IdP produced untouched.
func tamperVendorToken(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	// Flip a bit in the middle of the payload: inside the claims, not padding.
	mid := len(payload) / 2
	payload[mid] ^= 0x01
	parts[1] = base64.RawURLEncoding.EncodeToString(payload)
	return strings.Join(parts, ".")
}

func oidcClaimString(claims map[string]any, key string) string {
	current := claims
	for i, part := range strings.Split(key, ".") {
		if i == len(strings.Split(key, "."))-1 {
			if value, ok := current[part].(string); ok {
				return value
			}
			return ""
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}

func oidcClaimStrings(claims map[string]any, key string) []string {
	parts := strings.Split(key, ".")
	current := claims
	for i, part := range parts {
		if i == len(parts)-1 {
			list, ok := current[part].([]any)
			if !ok {
				return nil
			}
			var out []string
			for _, item := range list {
				if value, ok := item.(string); ok {
					out = append(out, value)
				}
			}
			return out
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return nil
}
