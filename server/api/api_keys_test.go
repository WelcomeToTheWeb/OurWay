package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestAPIKeyCreate(t *testing.T) {
	ts, _, _ := newTestServer(t)

	// Register and login
	registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
	token := loginUser(t, ts.URL, "testuser", "password123")

	// Create API key
	body := `{"name":"Test Key","scopes":["read","write"]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/api-keys", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 201, got %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Key struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Scopes string `json:"scopes"`
		} `json:"key"`
		KeyValue string `json:"key_value"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Key.Name != "Test Key" {
		t.Errorf("expected name 'Test Key', got '%s'", result.Key.Name)
	}
	if result.KeyValue == "" {
		t.Error("expected non-empty key_value")
	}
}

func TestAPIKeyList(t *testing.T) {
	ts, _, _ := newTestServer(t)
	registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
	token := loginUser(t, ts.URL, "testuser", "password123")

	// Create a key first
	body := `{"name":"Test Key","scopes":["read"]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/api-keys", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	resp.Body.Close()

	// List keys
	req, _ = http.NewRequest("GET", ts.URL+"/api/v2/api-keys", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Keys []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"keys"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(result.Keys))
	}
}

func TestAPIKeyAuth(t *testing.T) {
	ts, _, _ := newTestServer(t)
	registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
	token := loginUser(t, ts.URL, "testuser", "password123")

	// Create API key
	body := `{"name":"Test Key","scopes":["read"]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/api-keys", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()

	var createResult struct {
		Key      struct{} `json:"key"`
		KeyValue string   `json:"key_value"`
	}
	json.NewDecoder(resp.Body).Decode(&createResult)

	// Use API key to access protected endpoint
	req, _ = http.NewRequest("GET", ts.URL+"/api/devices", nil)
	req.Header.Set("X-API-Key", createResult.KeyValue)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200 with API key auth, got %d: %s", resp.StatusCode, string(respBody))
	}
}

func TestAPIKeyRevoke(t *testing.T) {
	ts, _, _ := newTestServer(t)
	registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
	token := loginUser(t, ts.URL, "testuser", "password123")

	// Create API key
	body := `{"name":"Test Key","scopes":["read"]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/api-keys", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()

	var createResult struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
		KeyValue string `json:"key_value"`
	}
	json.NewDecoder(resp.Body).Decode(&createResult)

	// Revoke the key
	req, _ = http.NewRequest("POST", ts.URL+"/api/v2/api-keys/"+createResult.Key.ID+"/revoke", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("revoke request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 on revoke, got %d", resp.StatusCode)
	}

	// Try to use revoked key
	req, _ = http.NewRequest("GET", ts.URL+"/api/devices", nil)
	req.Header.Set("X-API-Key", createResult.KeyValue)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with revoked key, got %d", resp.StatusCode)
	}
}

func TestAPIKeyRotate(t *testing.T) {
	ts, _, _ := newTestServer(t)
	registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
	token := loginUser(t, ts.URL, "testuser", "password123")

	// Create API key
	body := `{"name":"Test Key","scopes":["read"]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/api-keys", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()

	var createResult struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
		KeyValue string `json:"key_value"`
	}
	json.NewDecoder(resp.Body).Decode(&createResult)
	oldKey := createResult.KeyValue

	// Rotate the key
	req, _ = http.NewRequest("POST", ts.URL+"/api/v2/api-keys/"+createResult.Key.ID+"/rotate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rotate request failed: %v", err)
	}
	defer resp.Body.Close()

	var rotateResult struct {
		Key      struct{} `json:"key"`
		KeyValue string   `json:"key_value"`
	}
	json.NewDecoder(resp.Body).Decode(&rotateResult)
	newKey := rotateResult.KeyValue

	if oldKey == newKey {
		t.Error("expected different key after rotation")
	}

	// Old key should fail
	req, _ = http.NewRequest("GET", ts.URL+"/api/devices", nil)
	req.Header.Set("X-API-Key", oldKey)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with old key, got %d", resp.StatusCode)
	}

	// New key should work
	req, _ = http.NewRequest("GET", ts.URL+"/api/devices", nil)
	req.Header.Set("X-API-Key", newKey)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with new key, got %d", resp.StatusCode)
	}
}

func TestAPIKeyRotateWithExpiration(t *testing.T) {
	ts, _, _ := newTestServer(t)
	registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
	token := loginUser(t, ts.URL, "testuser", "password123")

	// Create API key
	body := `{"name":"Test Key","scopes":["read"]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/api-keys", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()

	var createResult struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
	}
	json.NewDecoder(resp.Body).Decode(&createResult)

	// Rotate with an expiration — the server must honour the expires
	// field the web client sends (previously it was silently ignored).
	req, _ = http.NewRequest("POST", ts.URL+"/api/v2/api-keys/"+createResult.Key.ID+"/rotate", bytes.NewBufferString(`{"expires":"7d"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rotate request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from rotate, got %d", resp.StatusCode)
	}

	var rotateResult struct {
		Key struct {
			ExpiresAt *string `json:"expires_at"`
		} `json:"key"`
		KeyValue string `json:"key_value"`
	}
	json.NewDecoder(resp.Body).Decode(&rotateResult)
	if rotateResult.KeyValue == "" {
		t.Error("expected key_value in rotate response")
	}
	if rotateResult.Key.ExpiresAt == nil || *rotateResult.Key.ExpiresAt == "" {
		t.Error("expected expires_at to be set after rotating with expires=7d")
	}

	// Body-less rotate (older clients) must still work and clear nothing.
	req, _ = http.NewRequest("POST", ts.URL+"/api/v2/api-keys/"+createResult.Key.ID+"/rotate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rotate without body failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for body-less rotate, got %d", resp.StatusCode)
	}
}

func TestAPIKeyExpiration(t *testing.T) {
	ts, _, _ := newTestServer(t)
	registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
	token := loginUser(t, ts.URL, "testuser", "password123")

	// Create API key that expires in 1 hour
	body := `{"name":"Temp Key","scopes":["read"],"expires":"1h"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/api-keys", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()

	var createResult struct {
		Key struct {
			ExpiresAt string `json:"expires_at"`
		} `json:"key"`
	}
	json.NewDecoder(resp.Body).Decode(&createResult)

	if createResult.Key.ExpiresAt == "" {
		t.Error("expected expires_at to be set")
	}
}
