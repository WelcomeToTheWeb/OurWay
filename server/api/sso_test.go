package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestSSOProviderTypeGuard pins the L8 decision: only oauth2/oidc are
// implemented; SAML (and any other type) must be rejected at create time,
// never stored as a selectable-but-broken provider.
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
		{"saml rejected", "saml", 400},
		{"SAML rejected case-insensitively", "SAML", 400},
		{"openid-connect rejected", "openid-connect", 400},
		{"empty type rejected", "", 400},
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

			body, _ := json.Marshal(map[string]string{
				"type":          tt.ssoType,
				"name":          slug,
				"client_id":     "cid",
				"client_secret": "secret",
			})
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
