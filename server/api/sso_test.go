package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"ourway/server/sso/testsaml"
)

// providerTypeForm is the create form for one type, filled the way the type needs:
// an OIDC row needs a client id and secret, a SAML row needs the IdP's entity ID, its
// sign-in location and the certificate that will sign assertions. The gate under test
// is the type, so each row is otherwise complete: a refusal must be about the
// protocol, not about a missing field.
func providerTypeForm(ssoType, slug string) map[string]any {
	form := map[string]any{
		"type":          ssoType,
		"name":          slug,
		"client_id":     "cid",
		"client_secret": "secret",
	}
	if strings.EqualFold(strings.TrimSpace(ssoType), "saml") {
		idp := testsaml.New()
		form["client_id"] = ""
		form["client_secret"] = ""
		form["idp_entity_id"] = idp.EntityID
		form["idp_sso_url"] = idp.SSOURL
		form["idp_certificate"] = idp.CertificatePEM()
	}
	return form
}

// TestSSOProviderTypeGuard pins the type gate, updated for 2.4-7.
//
// L8's rule was "never store a selectable-but-broken provider", and that is the rule
// that survives: an unknown type is refused, and a **SAML provider that cannot answer
// logins** is refused too. What changed is that `saml` is no longer the refused case:
// with the IdP entity ID, the SSO location and the signing certificate it needs, the
// row is a working door, and the create accepts it.
func TestSSOProviderTypeGuard(t *testing.T) {
	ts, _, jwtAuth := newTestServer(t)
	adminTok := mintToken(t, jwtAuth, "admin-id", []string{"admin"})

	tests := []struct {
		name    string
		ssoType string
		want    int
	}{
		{"oauth2 accepted", "oauth2", 200},
		{"oidc accepted", "oidc", 200},
		{"oauth accepted (legacy alias)", "oauth", 200},
		{"saml accepted with its facts", "saml", 200},
		{"SAML accepted case-insensitively", "SAML", 200},
		{"openid-connect rejected", "openid-connect", 400},
		{"empty type rejected", "", 400},
		{"nonsense type rejected", "single-sign-on-v3", 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The provider name is a URL path segment (`/api/auth/sso/<name>/callback`),
			// and 2.4-1 validates it as one. `t.Name()` inside a subtest contains a
			// slash, so the fixture slugifies it rather than leaning on the test
			// framework's naming.
			slug := "provider-" + strings.Map(func(r rune) rune {
				switch {
				case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
					return r
				}
				return '-'
			}, tt.ssoType)

			body, _ := json.Marshal(providerTypeForm(tt.ssoType, slug))
			req, err := http.NewRequest("POST", ts.URL+"/api/sso/providers", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("request build failed: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+adminTok)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.want {
				payload, _ := io.ReadAll(resp.Body)
				t.Errorf("create provider type %q: got %d, want %d\n%s", tt.ssoType, resp.StatusCode, tt.want, payload)
			}
		})
	}
}
