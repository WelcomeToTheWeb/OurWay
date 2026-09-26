package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestWebhookCreate(t *testing.T) {
	ts, _, jwtAuth := newTestServer(t)
	token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})

	body := `{
		"name": "Slack Alerts",
		"url": "https://hooks.slack.com/services/T00/B00/XXX",
		"events": ["alert_created", "alert_resolved"],
		"enabled": true
	}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/webhooks", bytes.NewBufferString(body))
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
		Webhook struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"webhook"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Webhook.Name != "Slack Alerts" {
		t.Errorf("expected name 'Slack Alerts', got '%s'", result.Webhook.Name)
	}
}

func TestWebhookList(t *testing.T) {
	ts, _, jwtAuth := newTestServer(t)
	token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})

	// Create a webhook first
	body := `{"name":"Test","url":"https://example.com/hook","events":["alert_created"]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/webhooks", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	resp.Body.Close()

	// List webhooks
	req, _ = http.NewRequest("GET", ts.URL+"/api/v2/webhooks", nil)
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
		Webhooks []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"webhooks"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Webhooks) != 1 {
		t.Errorf("expected 1 webhook, got %d", len(result.Webhooks))
	}
}

func TestWebhookDelete(t *testing.T) {
	ts, _, jwtAuth := newTestServer(t)
	token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})

	// Create a webhook
	body := `{"name":"Test","url":"https://example.com/hook","events":["alert_created"]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/webhooks", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()

	var createResult struct {
		Webhook struct {
			ID string `json:"id"`
		} `json:"webhook"`
	}
	json.NewDecoder(resp.Body).Decode(&createResult)

	// Delete the webhook
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/v2/webhooks/"+createResult.Webhook.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWebhookUpdate(t *testing.T) {
	ts, _, jwtAuth := newTestServer(t)
	token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})

	// Create a webhook
	body := `{"name":"Old Name","url":"https://example.com/hook","events":["alert_created"]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/webhooks", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	defer resp.Body.Close()

	var createResult struct {
		Webhook struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"webhook"`
	}
	json.NewDecoder(resp.Body).Decode(&createResult)

	// Update the webhook name
	updateBody := `{"name":"New Name"}`
	req, _ = http.NewRequest("PUT", ts.URL+"/api/v2/webhooks/"+createResult.Webhook.ID, bytes.NewBufferString(updateBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	var updateResult struct {
		Webhook struct {
			Name string `json:"name"`
		} `json:"webhook"`
	}
	json.NewDecoder(resp.Body).Decode(&updateResult)

	if updateResult.Webhook.Name != "New Name" {
		t.Errorf("expected name 'New Name', got '%s'", updateResult.Webhook.Name)
	}
}
