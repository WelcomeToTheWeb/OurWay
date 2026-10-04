package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newServer(t *testing.T, newBuild []byte, listed string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/installers", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"installers":[{"name":%q,"sha256":%q}]}`, viewerArtifact, listed)
	})
	mux.HandleFunc("/api/v2/installers/"+viewerArtifact, func(w http.ResponseWriter, r *http.Request) { w.Write(newBuild) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestUpdaterCheckAndApply(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "viewer.exe")
	os.WriteFile(exe, []byte("old build"), 0o755)
	newBuild := []byte("new build")
	ctx := context.Background()

	// Server publishes a different build -> update needed.
	srv := newServer(t, newBuild, sum(newBuild))
	u := NewUpdater(srv.URL, exe)
	want, err := u.Check(ctx)
	if err != nil || want != sum(newBuild) {
		t.Fatalf("Check = %q, %v; want the published hash", want, err)
	}
	if err := u.Apply(ctx, want); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new build" {
		t.Fatalf("exe not replaced: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old build" {
		t.Fatalf("old build not kept aside: %q", b)
	}
	removeLeftovers(exe)
	if _, err := os.Stat(exe + ".old"); err == nil {
		t.Error("leftover .old not removed")
	}

	// Now current -> nothing to do.
	if want, err := u.Check(ctx); err != nil || want != "" {
		t.Fatalf("up-to-date viewer: Check = %q, %v", want, err)
	}
}

func TestUpdaterRejectsCorruptDownload(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "viewer.exe")
	os.WriteFile(exe, []byte("old build"), 0o755)
	// The list promises one hash, the download delivers other bytes.
	srv := newServer(t, []byte("tampered"), sum([]byte("expected")))
	u := NewUpdater(srv.URL, exe)
	if err := u.Apply(context.Background(), sum([]byte("expected"))); err == nil {
		t.Fatal("checksum mismatch must fail")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old build" {
		t.Fatalf("running exe was touched: %q", b)
	}
	if _, err := os.Stat(exe + ".new"); err == nil {
		t.Error("partial download left behind")
	}
}

func TestUpdaterNoViewerPublished(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "viewer.exe")
	os.WriteFile(exe, []byte("x"), 0o755)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"installers":[]}`)) }))
	defer srv.Close()
	if want, err := NewUpdater(srv.URL, exe).Check(context.Background()); err != nil || want != "" {
		t.Fatalf("Check = %q, %v; want no update", want, err)
	}
}
