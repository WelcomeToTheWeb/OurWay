package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// viewerArtifact is the file the server publishes for this build.
const viewerArtifact = "ourway-viewer-windows-amd64.exe"

// Updater keeps the viewer current with the server it is launched
// from. The server publishes the viewer's SHA-256 in its public
// installer list; when that differs from the running executable, the
// new build is downloaded, verified and swapped in. Comparing hashes
// (rather than version strings) means a locally built or stale viewer
// is replaced too, with no version numbering to keep in step.
type Updater struct {
	Server   string
	Exe      string // path of the running executable
	Artifact string
	Client   *http.Client
}

// NewUpdater returns an updater for the running executable.
func NewUpdater(server, exe string) *Updater {
	return &Updater{Server: strings.TrimRight(server, "/"), Exe: exe, Artifact: viewerArtifact, Client: &http.Client{Timeout: 60 * time.Second}}
}

// Check returns the published SHA-256 when it differs from the running
// executable, or "" when the viewer is current or the server does not
// publish a viewer.
func (u *Updater) Check(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u.Server+"/api/v2/installers", nil)
	if err != nil {
		return "", err
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("installer list returned %d", resp.StatusCode)
	}
	var body struct {
		Installers []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"installers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	want := ""
	for _, e := range body.Installers {
		if e.Name == u.Artifact {
			want = e.SHA256
		}
	}
	if want == "" {
		return "", nil
	}
	have, err := fileSHA256(u.Exe)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(have, want) {
		return "", nil
	}
	return want, nil
}

// Apply downloads the build whose SHA-256 is want and replaces the
// running executable with it. Windows cannot overwrite a running exe
// but can rename it, so the old file is moved aside (and removed on the
// next start) before the new one takes its place. On failure the
// original is restored.
func (u *Updater) Apply(ctx context.Context, want string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u.Server+"/api/v2/installers/"+u.Artifact, nil)
	if err != nil {
		return err
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}
	tmp := u.Exe + ".new"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		os.Remove(tmp)
		return fmt.Errorf("checksum mismatch (got %s, want %s)", got, want)
	}
	old := u.Exe + ".old"
	os.Remove(old)
	if err := os.Rename(u.Exe, old); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("move current viewer aside: %w", err)
	}
	if err := os.Rename(tmp, u.Exe); err != nil {
		os.Rename(old, u.Exe)
		os.Remove(tmp)
		return fmt.Errorf("install new viewer: %w", err)
	}
	return nil
}

// removeLeftovers deletes the previous build and any partial download
// left by an earlier update.
func removeLeftovers(exe string) {
	os.Remove(exe + ".old")
	os.Remove(exe + ".new")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
