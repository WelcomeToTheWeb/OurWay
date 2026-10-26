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
		// The bare agent is only listed with ?all=1 (the installer
		// shadows it otherwise), so the checker must ask for it.
		if r.URL.Query().Get("all") != "1" {
			fmt.Fprint(w, `{"installers":[]}`)
			return
		}
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

func TestParseVersion(t *testing.T) {
	good := map[string][3]uint64{
		"2.0.3":               {2, 0, 3},
		"v2.0.3":              {2, 0, 3},
		"2.0.4-dev.1+5f03d77": {2, 0, 4},
		" 10.20.30 ":          {10, 20, 30},
	}
	for raw, want := range good {
		v := parseVersion(raw)
		if !v.ok {
			t.Fatalf("%q must parse", raw)
		}
		if v.major != want[0] || v.minor != want[1] || v.patch != want[2] {
			t.Fatalf("%q parsed as %d.%d.%d", raw, v.major, v.minor, v.patch)
		}
	}
	for _, raw := range []string{"", "   ", "dev", "5f03d774c5511ffd3f0083631175d30994bb0bbe", "2.0", "v2.0", "2.0.3-", "2.0.x"} {
		if parseVersion(raw).ok {
			t.Fatalf("%q must not parse", raw)
		}
	}
	if v := parseVersion("2.0.4-dev.1+abc"); len(v.pre) != 2 || v.pre[0] != "dev" || v.pre[1] != "1" {
		t.Fatalf("prerelease parsed as %#v", v.pre)
	}
}

func TestCompareOrdersByCoreThenPrerelease(t *testing.T) {
	ordered := []string{
		"2.0.0", "2.0.1-dev.1", "2.0.1-dev.2", "2.0.1", "2.0.10", "2.1.0", "10.0.0",
	}
	for i := 0; i < len(ordered)-1; i++ {
		cmp, ok := parseVersion(ordered[i]).compare(parseVersion(ordered[i+1]))
		if !ok || cmp != -1 {
			t.Fatalf("%s must order below %s (got %d,%v)", ordered[i], ordered[i+1], cmp, ok)
		}
		if rev, _ := parseVersion(ordered[i+1]).compare(parseVersion(ordered[i])); rev != 1 {
			t.Fatalf("%s must order above %s", ordered[i+1], ordered[i])
		}
	}
	if cmp, _ := parseVersion("2.0.3+aaaa").compare(parseVersion("2.0.3+bbbb")); cmp != 0 {
		t.Fatal("build metadata must not order")
	}
	if _, ok := parseVersion("dev").compare(parseVersion("2.0.3")); ok {
		t.Fatal("an unstamped build must not compare")
	}
}

func TestShouldUpdateFollowsChannelAndOrder(t *testing.T) {
	cases := []struct {
		server, local, channel string
		want                   bool
		why                    string
	}{
		{"2.0.4", "2.0.3", "stable", true, "newer patch"},
		{"2.1.0", "2.0.9", "stable", true, "newer minor"},
		{"2.0.3", "2.0.3", "stable", false, "already on it"},
		{"2.0.2", "2.0.3", "stable", false, "downgrade is never automatic"},
		{"2.0.4-dev.1+5f03d77", "2.0.3", "stable", false, "stable ignores edge"},
		{"2.0.4-dev.1+5f03d77", "2.0.4-dev.0+aaaa", "edge", true, "edge follows main"},
		{"2.0.4-dev.1+5f03d77", "2.0.4", "edge", false, "a prerelease is older than its release"},
		{"dev", "2.0.3", "edge", false, "an unstamped server never triggers an update"},
		{"5f03d774c5511ffd3f0083631175d30994bb0bbe", "2.0.3", "edge", false, "a raw sha carries no ordering"},
		{"2.0.4", "dev", "stable", false, "an unstamped agent never chases a stamp"},
		{"2.0.4", "", "stable", false, "an empty local version is not a version"},
	}
	for _, c := range cases {
		if got := shouldUpdate(c.server, c.local, c.channel); got != c.want {
			t.Fatalf("%s: shouldUpdate(%s, %s, %s) = %v", c.why, c.server, c.local, c.channel, got)
		}
	}
}

func TestUpdateChannelDefaultsToStable(t *testing.T) {
	for _, raw := range []string{"", "STABLE", "stable ", "typo", "main"} {
		t.Setenv("OURWAY_UPDATE_CHANNEL", raw)
		if got := updateChannel(); got != channelStable {
			t.Fatalf("OURWAY_UPDATE_CHANNEL=%q resolved to %q", raw, got)
		}
	}
	t.Setenv("OURWAY_UPDATE_CHANNEL", "EDGE")
	if got := updateChannel(); got != channelEdge {
		t.Fatalf("edge channel not recognised: %q", got)
	}
}
