package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/auth"
	"ourway/server/files"
	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

func setupFileTest(t *testing.T) (*httptest.Server, *store.Store, string, *files.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	// In-memory SQLite is per-connection: pin the pool to a single connection
	// so migrations and queries share the same database.
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}

	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	jwtAuth := auth.NewJWTAuth("test-secret")
	hub := ws.NewHub()
	fileService := files.NewService(st, hub, "")
	fileHandler := NewFileHandler(st, fileService)
	agentFileHandler := NewAgentFileHandler(st, fileService)

	router := gin.Default()

	// Agent file endpoints
	agentGroup := router.Group("/api/agent/files")
	agentGroup.POST("/status", agentFileHandler.ReportStatus)

	// User file endpoints
	fileGroup := router.Group("/api/files", AuthMiddleware(jwtAuth))
	{
		fileGroup.POST("/upload", fileHandler.UploadFile)
		fileGroup.GET("/transfers", fileHandler.ListTransfers)
		fileGroup.GET("/transfers/:id", fileHandler.GetTransfer)
	}

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	// Create test user
	user := &models.User{
		ID:           "user-1",
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "hashed",
	}
	st.Users.Create(user)

	token, err := jwtAuth.GenerateToken("user-1", "testuser", []string{"admin"})
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	return ts, st, token, fileService
}

func TestUploadFile(t *testing.T) {
	ts, _, token, _ := setupFileTest(t)

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	part.Write([]byte("Hello, World!"))

	writer.Close()

	req, err := http.NewRequest("POST", ts.URL+"/api/files/upload", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestListTransfers(t *testing.T) {
	ts, _, token, _ := setupFileTest(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/files/transfers", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["transfers"] == nil {
		t.Error("expected transfers in response")
	}
}

func TestAgentReportStatus(t *testing.T) {
	ts, st, _, _ := setupFileTest(t)

	// Create a file transfer first
	transfer := &models.FileTransfer{
		ID:          uuid.New().String(),
		DeviceID:    "device-1",
		Filename:    "test.txt",
		Destination: "/tmp/test.txt",
		Status:      "pending",
		Direction:   "push",
		Progress:    0,
	}
	st.FileTransfers.Create(transfer)

	// Report status update from agent
	payload := map[string]interface{}{
		"transfer_id": transfer.ID,
		"status":      "transferring",
		"progress":    50,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", ts.URL+"/api/agent/files/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verify the transfer was updated
	updated, err := st.FileTransfers.GetByID(transfer.ID)
	if err != nil {
		t.Fatalf("failed to get transfer: %v", err)
	}
	if updated.Status != "transferring" {
		t.Errorf("expected status transferring, got %s", updated.Status)
	}
	if updated.Progress != 50 {
		t.Errorf("expected progress 50, got %d", updated.Progress)
	}
}

func TestGetTransfer(t *testing.T) {
	ts, st, token, _ := setupFileTest(t)

	// Create a transfer
	transfer := &models.FileTransfer{
		ID:          uuid.New().String(),
		DeviceID:    "device-1",
		Filename:    "test.txt",
		Destination: "/tmp/test.txt",
		Status:      "pending",
		Direction:   "push",
		Progress:    0,
	}
	st.FileTransfers.Create(transfer)

	req, _ := http.NewRequest("GET", ts.URL+"/api/files/transfers/"+transfer.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["transfer"] == nil {
		t.Error("expected transfer in response")
	}
}