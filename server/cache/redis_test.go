package cache

import (
	"context"
	"testing"
	"time"
)

// Note: These tests require a running Redis instance at localhost:6379
// Run with: docker run -d -p 6379:6379 redis:alpine

func getTestClient(t *testing.T) *RedisClient {
	t.Helper()
	ctx := context.Background()
	rc, err := NewRedisClient(ctx, "redis://localhost:6379/0")
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	t.Cleanup(func() {
		rc.Close()
	})
	return rc
}

func TestSetAndGet(t *testing.T) {
	rc := getTestClient(t)

	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	val := testStruct{Name: "test", Value: 42}
	err := rc.Set("test:key", val, 10*time.Second)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	var result testStruct
	err = rc.Get("test:key", &result)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if result.Name != "test" || result.Value != 42 {
		t.Errorf("Expected {test, 42}, got {%s, %d}", result.Name, result.Value)
	}
}

func TestGetMissing(t *testing.T) {
	rc := getTestClient(t)

	var result string
	err := rc.Get("test:missing", &result)
	if err == nil {
		t.Error("Expected error for missing key")
	}
}

func TestDelete(t *testing.T) {
	rc := getTestClient(t)

	err := rc.Set("test:del", "value", 10*time.Second)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	err = rc.Delete("test:del")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var result string
	err = rc.Get("test:del", &result)
	if err == nil {
		t.Error("Expected error after delete")
	}
}

func TestIncr(t *testing.T) {
	rc := getTestClient(t)

	count, err := rc.Incr("test:incr", 10*time.Second)
	if err != nil {
		t.Fatalf("Incr failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	count, err = rc.Incr("test:incr", 10*time.Second)
	if err != nil {
		t.Fatalf("Incr failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}
}
