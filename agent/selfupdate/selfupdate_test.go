package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func artifactName() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("ourway-agent-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

func serve(t *testing.T, body []byte, listedHash string) *Checker {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/installers", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"installers":[{"name":%q,"sha256":%q}]}`, artifactName(), listedHash)
	})
	mux.HandleFunc("/api/v2/installers/", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewChecker(srv.URL)
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	body := []byte("new agent binary")
	sum := sha256.Sum256(body)
	good := hex.EncodeToString(sum[:])
	dst := filepath.Join(t.TempDir(), "ourway-agent.new")

	if _, err := serve(t, body, good).download(dst); err != nil {
		t.Fatalf("matching checksum rejected: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != string(body) {
		t.Fatal("downloaded content differs")
	}

	os.Remove(dst)
	if _, err := serve(t, body, "deadbeef").download(dst); err == nil {
		t.Fatal("checksum mismatch must fail the download")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("corrupt download must not be left on disk")
	}
}
