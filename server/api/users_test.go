package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestUserList(t *testing.T) {
	ts, _, jwtAuth := newTestServer(t)
	
	regResp := registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
	regBody, _ := io.ReadAll(regResp.Body)
	regResp.Body.Close()
	t.Logf("Register testuser: %d %s", regResp.StatusCode, string(regBody))

	token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})

	// Create another user
	regResp2 := registerUser(t, ts.URL, "user2", "user2@example.com", "password123")
	regBody2, _ := io.ReadAll(regResp2.Body)
	regResp2.Body.Close()
	t.Logf("Register user2: %d %s", regResp2.StatusCode, string(regBody2))

	req, _ := http.NewRequest("GET", ts.URL+"/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("Users list raw: %s", string(respBody))
	
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Email    string `json:"email"`
		} `json:"users"`
	}
	json.Unmarshal(respBody, &result)
	users := result.Users

	t.Logf("Users list response: %d users", len(users))
	for _, u := range users {
		t.Logf("  - %s (%s)", u.Username, u.Email)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestUserUpdate(t *testing.T) {
	ts, _, jwtAuth := newTestServer(t)
	registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
	token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})

	// List users to get the ID
	req, _ := http.NewRequest("GET", ts.URL+"/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var result struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"users"`
	}
	json.Unmarshal(respBody, &result)
	users := result.Users

	if len(users) == 0 {
		t.Fatal("expected at least 1 user")
	}

	// Update user roles
	body := `{"roles":["admin"]}`
	req, _ = http.NewRequest("PUT", ts.URL+"/api/users/"+users[0].ID+"/roles", bytes.NewBufferString(body))
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
}

func TestUserDelete(t *testing.T) {
	ts, _, jwtAuth := newTestServer(t)
	registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
	token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})

	// Create user to delete
	registerUser(t, ts.URL, "tempuser", "temp@example.com", "password123")

	// Find the temp user
	req, _ := http.NewRequest("GET", ts.URL+"/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var result struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"users"`
	}
	json.Unmarshal(respBody, &result)
	users := result.Users

	var tempUserID string
	for _, u := range users {
		if u.Username == "tempuser" {
			tempUserID = u.ID
			break
		}
	}

	if tempUserID == "" {
		t.Fatal("temp user not found")
	}

	// Delete the user
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/users/"+tempUserID, nil)
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
