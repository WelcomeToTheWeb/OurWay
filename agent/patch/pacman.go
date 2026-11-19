package patch

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Pacman updates (Arch Linux and Arch derivatives).

// pacmanCacheDir is pacman's default package cache — the only place a
// recorded older version can still be found on Arch (see
// rollbackPacmanEntry). It is a variable so a test can point it at a directory
// it built; it is not a knob for an operator, and a pacman.conf with extra
// CacheDir entries is not consulted.
var pacmanCacheDir = "/var/cache/pacman/pkg"

// scanPacmanUpdates lists the packages pacman considers out of date.
//
// `checkupdates` (pacman-contrib) is preferred and `pacman -Qu` is the
// fallback: both print `name installed -> available`, and checkupdates reads
// the sync databases from a temporary copy, so it takes neither
// /var/lib/pacman's lock nor root privileges and writes nothing — the
// `+ checkupdates where available` in the plan is that tool, and it is the
// better of the two for an agent that only wants to look.
//
// Unlike the apt path, this scan does NOT refresh the sync databases first
// (there is no `pacman -Sy` here). Arch treats a refreshed database with
// packages still on their old versions as the partial-upgrade state that
// breaks the next install, so an agent monitoring a host must not put it into
// that state as a side effect of looking. The scan reports what the operator's
// own last refresh made visible — a deliberate difference from apt, and worth
// knowing when a scan looks empty.
func scanPacmanUpdates() []Update {
	for _, tool := range []string{"checkupdates", "pacman"} {
		var args []string
		if tool == "pacman" {
			args = []string{"-Qu"}
		}
		if _, err := exec.LookPath(tool); err != nil {
			continue
		}
		res := runPkg(pkgScanTimeout, tool, args...)
		if res.startErr != nil {
			log.Printf("scan pacman: %v", res.startErr)
			continue
		}
		if res.code != 0 {
			log.Printf("scan pacman: %s exited %d: %s", tool, res.code, truncate(strings.TrimSpace(res.output), 200))
			continue
		}
		return parsePacmanUpgrades(res.output)
	}
	return nil
}

// parsePacmanUpgrades turns `pacman -Qu` / `checkupdates` output into update
// rows. The one line format is:
//
//	openssl 3.5.1-1 -> 3.6.0-1
//
// Every other line is skipped, not coerced. Both commands write warnings and
// errors into the same stream a deployment captures (`warning: gcc: ignoring
// package upgrade (13.2.1-6 => 14.2.1-2)` for an IgnorePkg entry, `error:
// database 'core' is not installed` for a missing one), and a line that is not
// `name installed -> available` names no version to install.
//
// `checkupdates` and `pacman -Qu` both answer from the sync databases, so an
// AUR-built package — which is in neither — is simply absent from this output.
// That is the honest reading of a mixed Arch system, not a gap to paper over:
// there is no row to approve for a package the repository does not provide.
//
// Pacman has no severity, category or article number for an update, so those
// columns stay empty; the server maps an empty severity to "unspecified"
// (server/api/agent_patch.go normalizeSeverity) rather than the agent
// inventing one here. ExternalID is the package identity pacman itself uses,
// `name-version`, which is what UpsertDetected dedups a re-scan against.
func parsePacmanUpgrades(output string) []Update {
	var updates []Update
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) != 4 || parts[2] != "->" {
			continue
		}
		name, available := parts[0], parts[3]
		updates = append(updates, Update{
			Source:     "pacman",
			Title:      name,
			Version:    available,
			ExternalID: name + "-" + available,
		})
	}
	return updates
}

// installPacmanUpdate installs exactly the approved candidate with
// `pacman -U --noconfirm <package file or URL>`, the command the plan names
// and the reason it names it: `pacman -Syu` is a full-system upgrade, and
// `pacman -S <name>` installs whatever the sync database holds at the moment
// of the install, which is not necessarily the version the operator approved.
// `-U` takes one concrete package, so the candidate version seen at scan time
// is the version that lands on the host.
//
// pacman takes files and URLs, not package names, so the candidate's URL is
// resolved from the local sync databases first (pacmanCandidateURL).
//
// `--noconfirm` is also the anti-hang switch, and this is what the plan's "no
// GPG-prompt hangs" means in practice for pacman: it never asks, and what it
// would have asked about — an unknown PGP key, a file conflict — fails the
// install instead of waiting for a terminal nobody is going to type into.
//
// Like `-S`, `-U` still resolves missing dependencies from the sync
// repositories. It does not upgrade packages that are already current, so the
// approved set stays the only thing that moves on the host.
func installPacmanUpdate(u Update) (string, error) {
	url, err := pacmanCandidateURL(u)
	if err != nil {
		return "", err
	}
	res := runPkg(pkgDeployTimeout, "pacman", "-U", "--noconfirm", url)
	return res.output, pkgFailure("pacman -U", filepath.Base(url), res)
}

// pacmanCandidateURL resolves an approved update to the URL of the .pkg.tar
// file the configured mirrors serve for that exact version.
//
// `pacman -S --print` prints the whole resolved transaction, one target per
// line as `name location` (the documented %n and %l of --print-format), so a
// dependency can appear here too. Only the line naming the approved package is
// used, which is also what keeps a dependency from riding along under `-U`.
func pacmanCandidateURL(u Update) (string, error) {
	res := runPkg(pkgVersionTimeout, "pacman", "-S", "--print", "--print-format=%n %l", u.Title)
	if err := pkgFailure("pacman -S --print", u.Title, res); err != nil {
		return "", err
	}
	want := u.Title + "-" + u.Version
	for _, line := range strings.Split(res.output, "\n") {
		name, location, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found || name != u.Title {
			continue
		}
		// Without a scanned version there is no candidate to pin, and the
		// line pacman printed is the newest it knows about; install that.
		if u.Version != "" && !strings.Contains(filepath.Base(location), want) {
			continue
		}
		return location, nil
	}
	target := u.Title
	if u.Version != "" {
		target += "-" + u.Version
	}
	return "", fmt.Errorf("pacman cannot resolve an installable file for %s in the sync databases (refresh the databases and rescan)", target)
}

// rollbackPacmanEntry restores the version recorded before the deployment.
//
// Arch's mirrors serve only the current release of a package, so the recorded
// version can only come back from the local package cache: /var/cache/pacman/pkg
// keeps what was downloaded, and `pacman -U` installs that file again. When
// the cache has been cleaned (`pacman -Scc`) the operator is left holding a
// recorded version and no file, and this says so — naming the version and
// where to get it — rather than reporting a restore that did not happen.
//
// A new package (no recorded previous version) is removed again, as the apt
// and yum/dnf paths do for the same case.
//
// Full transaction rollback is deferred out of 2.5 (docs/phase2.5-tasks.md);
// this restores a version, not a transaction.
func rollbackPacmanEntry(e preDeployEntry) (string, error) {
	if e.OldVersion == "" {
		res := runPkg(pkgDeployTimeout, "pacman", "-R", "--noconfirm", e.Package)
		return res.output, pkgFailure("pacman -R", e.Package, res)
	}
	path := pacmanCachedPackage(e.Package, e.OldVersion)
	if path == "" {
		return "", fmt.Errorf("pacman cannot roll back %s to %s: that version is not in %s, and Arch mirrors serve only the current release (restore it manually from the Arch Linux Archive)", e.Package, e.OldVersion, pacmanCacheDir)
	}
	res := runPkg(pkgDeployTimeout, "pacman", "-U", "--noconfirm", path)
	return res.output, pkgFailure("pacman -U", filepath.Base(path), res)
}

// pacmanCachedPackage finds the cached package file for a name at an exact
// version, or "" when the cache has no such file. Cached files are named
// `<name>-<version>-<arch>.pkg.tar.<compression>` —
// `bash-5.2.026-2-x86_64.pkg.tar.zst` — and the architecture matters to the
// match because a version can be a prefix of another version: recording
// `1.0-1` must not find the file for `1.0-10`. Requiring the segment between
// the version and `.pkg.tar.` to hold no `-` is what rejects that prefix case,
// and detached signatures (`.pkg.tar.zst.sig`, sitting beside the package they
// sign) are rejected by suffix.
func pacmanCachedPackage(name, version string) string {
	entries, err := os.ReadDir(pacmanCacheDir)
	if err != nil {
		return ""
	}
	prefix := name + "-" + version + "-"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		n := entry.Name()
		if !strings.HasPrefix(n, prefix) || strings.HasSuffix(n, ".sig") {
			continue
		}
		arch, _, cut := strings.Cut(strings.TrimPrefix(n, prefix), ".pkg.tar.")
		if !cut || arch == "" || strings.Contains(arch, "-") {
			continue
		}
		return filepath.Join(pacmanCacheDir, n)
	}
	return ""
}

// parsePacmanQuery picks the version out of `pacman -Q <pkg>` output, which is
// `<name> <version>` on one line. A line that carries no version yields ""
// rather than a guess: an empty recorded version is the signal that tells a
// rollback to remove the package, so inventing one — or leaving one out
// through a loose match — both change what a rollback does to a host.
func parsePacmanQuery(output, name string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, name) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, name))
		if rest == "" || strings.ContainsAny(rest, " \t") {
			continue
		}
		return rest
	}
	return ""
}
