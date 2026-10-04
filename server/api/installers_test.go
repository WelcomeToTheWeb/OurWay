package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// withInstallersDir points installersDir at a temp dir containing the given
// files (name -> content) and restores it after the test.
func withInstallersDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}
	oldDir := installersDir
	installersDir = dir
	t.Cleanup(func() { installersDir = oldDir })
	return dir
}

func TestInstallerList(t *testing.T) {
	withInstallersDir(t, map[string]string{
		"ourway-installer-windows-amd64.exe": "dummy-windows-installer",
		"install.sh":                         "#!/bin/sh\necho install\n",
	})

	ts, _, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v2/installers")
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Installers []struct {
			Name   string `json:"name"`
			OS     string `json:"os"`
			Arch   string `json:"arch"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
			URL    string `json:"url"`
		} `json:"installers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Installers) != 2 {
		t.Fatalf("expected 2 installers, got %d", len(result.Installers))
	}

	byName := make(map[string]struct {
		Name   string `json:"name"`
		OS     string `json:"os"`
		Arch   string `json:"arch"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
		URL    string `json:"url"`
	}, len(result.Installers))
	for _, inst := range result.Installers {
		byName[inst.Name] = inst
	}

	exe, ok := byName["ourway-installer-windows-amd64.exe"]
	if !ok {
		t.Fatal("expected ourway-installer-windows-amd64.exe in list")
	}
	if exe.OS != "windows" || exe.Arch != "amd64" {
		t.Errorf("expected os=windows arch=amd64, got os=%q arch=%q", exe.OS, exe.Arch)
	}
	if exe.Size != int64(len("dummy-windows-installer")) {
		t.Errorf("expected size %d, got %d", len("dummy-windows-installer"), exe.Size)
	}
	wantSum := sha256.Sum256([]byte("dummy-windows-installer"))
	if exe.SHA256 != hex.EncodeToString(wantSum[:]) {
		t.Errorf("sha256 mismatch: got %q", exe.SHA256)
	}
	if exe.URL != "v2/installers/ourway-installer-windows-amd64.exe" {
		t.Errorf("unexpected url %q (want API-relative path)", exe.URL)
	}

	sh, ok := byName["install.sh"]
	if !ok {
		t.Fatal("expected install.sh in list")
	}
	if sh.OS != "" || sh.Arch != "" {
		t.Errorf("expected empty os/arch for install.sh, got os=%q arch=%q", sh.OS, sh.Arch)
	}
}

func TestInstallerListMissingDir(t *testing.T) {
	oldDir := installersDir
	installersDir = filepath.Join(t.TempDir(), "does-not-exist")
	t.Cleanup(func() { installersDir = oldDir })

	ts, _, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v2/installers")
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Installers []struct{} `json:"installers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Installers == nil || len(result.Installers) != 0 {
		t.Errorf("expected empty installers array, got %v", result.Installers)
	}
}

func TestInstallerDownload(t *testing.T) {
	content := "dummy-windows-installer"
	withInstallersDir(t, map[string]string{
		"ourway-installer-windows-amd64.exe": content,
		"install.sh":                         "#!/bin/sh\necho install\n",
	})

	ts, _, _ := newTestServer(t)

	t.Run("binary file streams as octet-stream attachment", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v2/installers/ourway-installer-windows-amd64.exe")
		if err != nil {
			t.Fatalf("download request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("expected application/octet-stream, got %q", got)
		}
		if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename="ourway-installer-windows-amd64.exe"` {
			t.Errorf("unexpected Content-Disposition %q", got)
		}
		if got := resp.Header.Get("Content-Length"); got != "23" {
			t.Errorf("expected Content-Length 23, got %q", got)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		if string(body) != content {
			t.Errorf("body mismatch: got %q", string(body))
		}
	})

	t.Run("shell script streams as text/plain", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v2/installers/install.sh")
		if err != nil {
			t.Fatalf("download request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); got != "text/plain" {
			t.Errorf("expected text/plain, got %q", got)
		}
	})

	t.Run("missing file returns 404", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/v2/installers/missing.exe")
		if err != nil {
			t.Fatalf("download request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

func TestInstallerDownloadTraversal(t *testing.T) {
	withInstallersDir(t, map[string]string{
		"ourway-agent-linux-amd64": "binary",
	})

	ts, _, _ := newTestServer(t)

	for _, name := range []string{".hidden", "..%5Csecret", "a..b"} {
		resp, err := http.Get(ts.URL + "/api/v2/installers/" + name)
		if err != nil {
			t.Fatalf("request for %q failed: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for %q, got %d", name, resp.StatusCode)
		}
	}
}

func TestInstallerListDedupesAndSorts(t *testing.T) {
	withInstallersDir(t, map[string]string{
		"ourway-agent-linux-amd64":           "agent-linux",
		"ourway-installer-linux-amd64":       "installer-linux",
		"ourway-installer-windows-amd64.exe": "installer-windows",
		"ourway-agent-darwin-arm64":          "agent-darwin",
		"ourway-installer-darwin-arm64":      "installer-darwin",
	})
	ts, _, _ := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v2/installers")
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Installers []struct {
			Name string `json:"name"`
			OS   string `json:"os"`
			Arch string `json:"arch"`
			Kind string `json:"kind"`
		} `json:"installers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	var got []string
	for _, inst := range result.Installers {
		got = append(got, inst.Name)
	}
	// One entry per platform (installer preferred over the bare agent),
	// ordered Windows, Linux, macOS; amd64 before arm64.
	want := []string{
		"ourway-installer-windows-amd64.exe",
		"ourway-installer-linux-amd64",
		"ourway-installer-darwin-arm64",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d installers, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: expected %q, got %q", i, want[i], got[i])
		}
	}
	for _, inst := range result.Installers {
		if inst.Kind != "installer" {
			t.Errorf("expected kind=installer for %q, got %q", inst.Name, inst.Kind)
		}
	}
}
