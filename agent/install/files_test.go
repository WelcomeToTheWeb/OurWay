package install

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveInstallFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "OurWay")
	self := filepath.Join(dir, "ourway-agent")
	for _, f := range []string{"ourway-agent", "ourway-remote", "uninstall.sh", "ourway-agent.old", "ourway-remote.download", "config/device.json", "logs/agent.log"} {
		write(t, filepath.Join(dir, f))
	}
	write(t, filepath.Join(dir, "keep-me.txt"))

	removed, err := RemoveInstallFiles(dir, self)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"ourway-remote", "uninstall.sh", "ourway-agent.old", "ourway-remote.download", "config", "logs"} {
		if fileExists(filepath.Join(dir, gone)) {
			t.Errorf("%s should have been removed", gone)
		}
	}
	for _, kept := range []string{"ourway-agent", "keep-me.txt"} {
		if !fileExists(filepath.Join(dir, kept)) {
			t.Errorf("%s must be kept (self binary / user file)", kept)
		}
	}
	if len(removed) == 0 {
		t.Error("nothing reported removed")
	}

	// With only the running binary left, the directory is left for the
	// caller's delayed self-delete; it is not removed while non-empty.
	os.Remove(filepath.Join(dir, "keep-me.txt"))
	RemoveInstallFiles(dir, self)
	if !fileExists(dir) {
		t.Error("directory holding the running binary must remain")
	}
}

func TestRemoveInstallFilesRefusesForeignDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "important.txt"))
	if _, err := RemoveInstallFiles(dir, filepath.Join(dir, "some-other-tool")); err == nil {
		t.Fatal("must refuse when selfExe is not an ourway-agent binary")
	}
	if !fileExists(filepath.Join(dir, "important.txt")) {
		t.Fatal("foreign file deleted")
	}
}
