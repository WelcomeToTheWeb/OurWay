package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// What the installer and self-update put in an install directory. Only
// these are removed on uninstall, so a stray file the user keeps there
// (or an unrelated directory the agent was run from) is never deleted.
var (
	managedFiles    = []string{"ourway-agent", "ourway-agent.exe", "ourway-remote", "ourway-remote.exe", "uninstall.cmd", "uninstall.sh"}
	managedDirs     = []string{"config", "logs"}
	managedSuffixes = []string{".old", ".new", ".download"} // self-update leftovers
)

// RemoveInstallFiles deletes the agent's files from dir except selfExe
// (the running binary, which the caller removes last or via a helper),
// then removes dir itself if nothing else is left in it. It returns the
// paths it removed. dir must be the directory of an agent binary.
func RemoveInstallFiles(dir, selfExe string) ([]string, error) {
	if !strings.HasPrefix(filepath.Base(selfExe), "ourway-agent") || filepath.Dir(selfExe) != dir {
		return nil, fmt.Errorf("%s is not an OurWay install directory", dir)
	}
	var removed []string
	rm := func(p string, all bool) {
		if p == selfExe {
			return
		}
		var err error
		if all {
			err = os.RemoveAll(p)
		} else {
			err = os.Remove(p)
		}
		if err == nil {
			removed = append(removed, p)
		}
	}
	for _, n := range managedFiles {
		if p := filepath.Join(dir, n); fileExists(p) {
			rm(p, false)
		}
	}
	for _, n := range managedDirs {
		if p := filepath.Join(dir, n); fileExists(p) {
			rm(p, true)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		for _, suf := range managedSuffixes {
			if !e.IsDir() && strings.HasSuffix(e.Name(), suf) && strings.HasPrefix(e.Name(), "ourway-") {
				rm(filepath.Join(dir, e.Name()), false)
			}
		}
	}
	// Remove the directory itself only when it is now empty (the
	// running binary may still be in it).
	if left, err := os.ReadDir(dir); err == nil && len(left) == 0 {
		if os.Remove(dir) == nil {
			removed = append(removed, dir)
		}
	}
	return removed, nil
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// ErrElevating is returned by Uninstall when it relaunched itself with
// administrator rights; the elevated copy does the work.
var ErrElevating = errors.New("continuing in an elevated window")

// removeSelfAndDir deletes the running binary and then its directory if
// that leaves it empty. It works on Unix, where a running binary can be
// unlinked.
func removeSelfAndDir(dir, selfExe string) {
	os.Remove(selfExe)
	if left, err := os.ReadDir(dir); err == nil && len(left) == 0 {
		os.Remove(dir)
	}
}
