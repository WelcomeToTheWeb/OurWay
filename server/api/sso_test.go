package api

import (
	"bytes"
	"encoding/json"
	"net/http"
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
			body, _ := json.Marshal(map[string]string{
				"type":          tt.ssoType,
				"name":          "provider-" + tt.ssoType + "-" + t.Name(),
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
				t.Errorf("create provider type %q: got %d, want %d", tt.ssoType, resp.StatusCode, tt.want)
			}
		})
	}
}
