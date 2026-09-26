package auth

import (
	"strings"
	"testing"
)

func TestGenerateAndValidateToken(t *testing.T) {
	jwtAuth := NewJWTAuth("test-secret")

	token, err := jwtAuth.GenerateToken("user-123", "testuser", []string{"admin", "user"})
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if token == "" {
		t.Fatal("Token is empty")
	}

	claims, err := jwtAuth.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if claims.UserID != "user-123" {
		t.Errorf("Expected user_id 'user-123', got '%s'", claims.UserID)
	}
	if claims.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", claims.Username)
	}
	if len(claims.Roles) != 2 {
		t.Errorf("Expected 2 roles, got %d", len(claims.Roles))
	}
}

func TestValidateTokenInvalidSecret(t *testing.T) {
	jwtAuth1 := NewJWTAuth("secret-1")
	jwtAuth2 := NewJWTAuth("secret-2")

	token, err := jwtAuth1.GenerateToken("user-123", "testuser", []string{"admin"})
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = jwtAuth2.ValidateToken(token)
	if err == nil {
		t.Error("Expected error when validating with different secret")
	}
}

func TestValidateTokenInvalidFormat(t *testing.T) {
	jwtAuth := NewJWTAuth("test-secret")

	_, err := jwtAuth.ValidateToken("not-a-valid-token")
	if err == nil {
		t.Error("Expected error for invalid token format")
	}
}

func TestHasRole(t *testing.T) {
	claims := &Claims{
		UserID:   "user-123",
		Username: "testuser",
		Roles:    []string{"admin", "user"},
	}

	if !claims.HasRole("admin") {
		t.Error("Expected HasRole('admin') to return true")
	}
	if claims.HasRole("manager") {
		t.Error("Expected HasRole('manager') to return false")
	}
}

func TestHasAnyRole(t *testing.T) {
	claims := &Claims{
		UserID:   "user-123",
		Username: "testuser",
		Roles:    []string{"admin", "user"},
	}

	if !claims.HasAnyRole([]string{"manager", "admin"}) {
		t.Error("Expected HasAnyRole to return true")
	}
	if claims.HasAnyRole([]string{"manager", "viewer"}) {
		t.Error("Expected HasAnyRole to return false")
	}
}

func TestTokenStructure(t *testing.T) {
	jwtAuth := NewJWTAuth("test-secret")

	token, err := jwtAuth.GenerateToken("user-123", "testuser", []string{"admin"})
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	// JWT tokens have 3 parts separated by dots
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("Expected 3 parts in JWT, got %d", len(parts))
	}
}
