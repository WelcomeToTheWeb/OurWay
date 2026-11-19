package patch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests for the 2.5-3 pacman and zypper paths.
//
// The four output fixtures under testdata/ are hand-built from the output
// formats pacman(8) and zypper(8) document — they are not transcripts
// recorded from real Arch or openSUSE systems, and each file says so at its
// top, along with the real run still owed for it. Everything below that does
// not need an output format (which argv reaches the package manager, which
// rollback state gets recorded) is asserted directly, with the runner
// replaced, and so needs no system either.

// fixture reads one of the testdata output fixtures.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// recordedRun is one package-manager invocation the agent decided to make.
type recordedRun struct {
	name string
	args []string
}

// recordRunner replaces the package-manager runner for one test: it records
// every argv the agent would have handed to a package manager and answers with
// the result registered for that program (a successful empty run for anything
// not registered). This is how "only the approved packages reach the package
// manager" is proven without a package manager.
func recordRunner(t *testing.T, results map[string]pkgResult) *[]recordedRun {
	t.Helper()
	previous := runPkg
	runs := []recordedRun{}
	runPkg = func(timeout time.Duration, name string, args ...string) pkgResult {
		runs = append(runs, recordedRun{name: name, args: append([]string(nil), args...)})
		if res, ok := results[name]; ok {
			return res
		}
		return pkgResult{code: 0}
	}
	t.Cleanup(func() { runPkg = previous })
	return &runs
}

// argsOf renders recorded runs as "program arg arg" lines for assertions.
func argsOf(runs []recordedRun) []string {
	out := make([]string, 0, len(runs))
	for _, r := range runs {
		out = append(out, strings.Join(append([]string{r.name}, r.args...), " "))
	}
	return out
}

// stubVersion pins what the agent believes is installed, so a deployment test
// does not depend on the package database of whatever machine runs it.
func stubVersion(t *testing.T, fn func(source, name string) (string, bool)) {
	t.Helper()
	previous := packageVersion
	packageVersion = fn
	t.Cleanup(func() { packageVersion = previous })
}

// fixedVersions answers from a map of installed versions. A package missing
// from the map is answered as installed with no version, which is what "newly
// installed" means to the rollback.
func fixedVersions(versions map[string]string) func(source, name string) (string, bool) {
	return func(source, name string) (string, bool) {
		return versions[name], true
	}
}

// useCacheDir points pacman's package cache at a directory this test built.
func useCacheDir(t *testing.T, dir string) {
	t.Helper()
	previous := pacmanCacheDir
	pacmanCacheDir = dir
	t.Cleanup(func() { pacmanCacheDir = previous })
}

func TestParsePacmanUpgradesFixtures(t *testing.T) {
	rolling := parsePacmanUpgrades(fixture(t, "pacman_qu_arch_rolling.txt"))
	if len(rolling) != 10 {
		t.Fatalf("rolling fixture: want 10 rows, got %d: %+v", len(rolling), rolling)
	}
	first := rolling[0]
	if first.Source != "pacman" || first.Title != "filesystem" || first.Version != "2026.09-1" || first.ExternalID != "filesystem-2026.09-1" {
		t.Errorf("row must carry the AVAILABLE version, not the installed one: %+v", first)
	}
	if first.KB != "" || first.Severity != "" || first.Category != "" {
		t.Errorf("pacman reports no article number, severity or category; the agent must not invent them: %+v", first)
	}

	aur := parsePacmanUpgrades(fixture(t, "checkupdates_arch_with_aur.txt"))
	if len(aur) != 2 {
		t.Fatalf("AUR fixture: want 2 rows (the AUR packages are absent by design), got %d: %+v", len(aur), aur)
	}
	if aur[0].Title != "pacman" || aur[1].Title != "lib32-nvidia-utils" {
		t.Errorf("warning lines must not become package rows: %+v", aur)
	}
}

func TestParsePacmanUpgradesSkipsAnythingWithoutACandidateVersion(t *testing.T) {
	out := `
coreutils 9.7-1 -> 9.8-1
warning: gcc: ignoring package upgrade (15.2.1+r26-1 => 16.1.1-2)
somepkg 1.0-1
another 1.0-1 -> -> 2.0-1
`
	got := parsePacmanUpgrades(out)
	if len(got) != 1 || got[0].Title != "coreutils" {
		t.Fatalf("want only the well-formed row, got %+v", got)
	}
}

func TestParsePacmanQuery(t *testing.T) {
	if got := parsePacmanQuery("bash 5.3.009-1\n", "bash"); got != "5.3.009-1" {
		t.Errorf("installed version: got %q", got)
	}
	if got := parsePacmanQuery("bash 5.3.009-1 and something else\n", "bash"); got != "" {
		t.Errorf("a line that is not name+version must not be read as a version: %q", got)
	}
	if got := parsePacmanQuery("warning: no package 'bash' is installed\n", "bash"); got != "" {
		t.Errorf("a warning about the package must not be read as its version: %q", got)
	}
	if got := parsePacmanQuery("", "bash"); got != "" {
		t.Errorf("no output: got %q", got)
	}
}

func TestInstalledPackageVersionSeparatesNotFoundFromFailure(t *testing.T) {
	runs := recordRunner(t, map[string]pkgResult{
		"pacman": {code: 1, output: "error: package 'curl' was not found\n"},
	})
	if v, ok := installedPackageVersion("pacman", "curl"); !ok || v != "" {
		t.Errorf("a package the database does not know must be answered as not installed, got %q ok=%v", v, ok)
	}
	if got := argsOf(*runs); len(got) != 1 || got[0] != "pacman -Q curl" {
		t.Fatalf("the installed version must be read with `pacman -Q <pkg>`, got %v", got)
	}

	recordRunner(t, map[string]pkgResult{
		"pacman": {code: 1, output: "error: database 'core' is not installed (or is corrupted)\n"},
	})
	if v, ok := installedPackageVersion("pacman", "curl"); ok || v != "" {
		t.Errorf("a query that failed must not be recorded as 'not installed' — the rollback would then REMOVE the package — got %q ok=%v", v, ok)
	}
}

func TestPacmanCachedPackageMatchesTheExactVersion(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		"curl-8.20.0-1-x86_64.pkg.tar.zst",
		"curl-8.20.0-1-x86_64.pkg.tar.zst.sig",
		"curl-8.20.0-10-any.pkg.tar.zst",
		"curl-8.19.0-1-x86_64.pkg.tar.zst",
		"libcurl-8.20.0-1-x86_64.pkg.tar.zst",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("not a package"), 0o600); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "curl-8.18.0-1-any.pkg.tar.zst"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	useCacheDir(t, dir)

	if got := pacmanCachedPackage("curl", "8.20.0-1"); got != filepath.Join(dir, "curl-8.20.0-1-x86_64.pkg.tar.zst") {
		t.Errorf("exact version: got %q", got)
	}
	if got := pacmanCachedPackage("curl", "8.19.0-1"); got != filepath.Join(dir, "curl-8.19.0-1-x86_64.pkg.tar.zst") {
		t.Errorf("older version: got %q", got)
	}
	if got := pacmanCachedPackage("curl", "8.20.0-10"); got != filepath.Join(dir, "curl-8.20.0-10-any.pkg.tar.zst") {
		t.Errorf("any-architecture package: got %q", got)
	}
	// 8.20.0-1 is a prefix of 8.20.0-10: a recorded version must never
	// restore a different one.
	if got := pacmanCachedPackage("curl", "8.20.0-1x"); got != "" {
		t.Errorf("a version the cache does not have: got %q", got)
	}
	if got := pacmanCachedPackage("curl", "8.18.0-1"); got != "" {
		t.Errorf("a directory that is named like a package is not a package: got %q", got)
	}
	if got := pacmanCachedPackage(filepath.Join(dir, "does-not-exist"), "0.1-1"); got != "" {
		t.Errorf("an unreadable cache dir must answer with nothing, not panic: got %q", got)
	}
}

func TestRollbackPacmanEntryRestoresFromTheCachedFile(t *testing.T) {
	dir := t.TempDir()
	cached := filepath.Join(dir, "curl-8.19.0-1-x86_64.pkg.tar.zst")
	if err := os.WriteFile(cached, []byte("not a package"), 0o600); err != nil {
		t.Fatalf("write cached file: %v", err)
	}
	useCacheDir(t, dir)

	runs := recordRunner(t, map[string]pkgResult{"pacman": {code: 0}})
	if _, err := rollbackPacmanEntry(preDeployEntry{Package: "curl", Source: "pacman", OldVersion: "8.19.0-1"}); err != nil {
		t.Fatalf("a cached package file must be restorable: %v", err)
	}
	if got := argsOf(*runs); len(got) != 1 || got[0] != "pacman -U --noconfirm "+cached {
		t.Fatalf("rollback must reinstall the recorded file with `pacman -U --noconfirm`, got %v", got)
	}
}

func TestRollbackPacmanEntrySaysWhenTheVersionIsGone(t *testing.T) {
	useCacheDir(t, t.TempDir())
	runs := recordRunner(t, nil)

	_, err := rollbackPacmanEntry(preDeployEntry{Package: "curl", Source: "pacman", OldVersion: "8.19.0-1"})
	if err == nil {
		t.Fatal("a version the cache does not have must fail, not report a restore")
	}
	for _, want := range []string{"curl", "8.19.0-1", pacmanCacheDir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure must name the package, the version and where it looked, got: %v", err)
		}
	}
	if len(*runs) != 0 {
		t.Fatalf("nothing may be run when there is nothing to restore from: %v", argsOf(*runs))
	}
}

func TestRollbackPacmanEntryRemovesANewlyInstalledPackage(t *testing.T) {
	useCacheDir(t, t.TempDir())
	runs := recordRunner(t, nil)

	if _, err := rollbackPacmanEntry(preDeployEntry{Package: "yay", Source: "pacman", OldVersion: ""}); err != nil {
		t.Fatalf("remove of a newly installed package: %v", err)
	}
	if got := argsOf(*runs); len(got) != 1 || got[0] != "pacman -R --noconfirm yay" {
		t.Fatalf("want `pacman -R --noconfirm yay`, got %v", got)
	}
}

func TestDeployLinuxUpdatesHandsOnlyApprovedPackagesToThePackageManager(t *testing.T) {
	stubVersion(t, fixedVersions(map[string]string{
		"curl":  "8.19.0-1",
		"glibc": "2.31-150300.9.1",
	}))
	runs := recordRunner(t, map[string]pkgResult{
		"pacman": {code: 0, output: "curl 8.20.0-1 https://geo.mirror.pkgbuild.com/extra/os/x86_64/curl-8.20.0-1-x86_64.pkg.tar.zst\n"},
		"zypper": {code: 0, output: "No update detected.\n"},
	})

	err := deployLinuxUpdates("", []Update{
		{Source: "pacman", Title: "curl", Version: "8.20.0-1"},
		{Source: "zypper", Title: "glibc", Version: "2.31-150300.10.1"},
	})
	if err != nil {
		t.Fatalf("deployment: %v", err)
	}

	want := []string{
		"pacman -S --print --print-format=%n %l curl",
		"pacman -U --noconfirm https://geo.mirror.pkgbuild.com/extra/os/x86_64/curl-8.20.0-1-x86_64.pkg.tar.zst",
		"zypper --non-interactive update glibc",
	}
	got := argsOf(*runs)
	if len(got) != len(want) {
		t.Fatalf("want exactly %d commands, got %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command %d:\n want %s\n got  %s", i, want[i], got[i])
		}
	}
	// `--print-format=%n %l` must reach pacman as ONE argument, or the format
	// string is read as a package name.
	if args := (*runs)[0].args; len(args) != 4 || args[2] != "--print-format=%n %l" {
		t.Errorf("the print format must be one argument, got %#v", args)
	}
	// Neither path may ever widen into a whole-system upgrade.
	for _, line := range got {
		for _, forbidden := range []string{"-Syu", "-Sy", "sysupgrade", "--no-refresh"} {
			if strings.Contains(line, forbidden) {
				t.Fatalf("a deployment of approved packages must never ask for a full upgrade: %q", line)
			}
		}
	}
}

func TestDeployLinuxUpdatesRefusesAnEmptyApprovedSetWithoutRunningAnything(t *testing.T) {
	runs := recordRunner(t, nil)

	err := deployLinuxUpdates("", nil)
	if err == nil || !strings.Contains(err.Error(), "refusing to run a full OS upgrade") {
		t.Fatalf("expected the full-upgrade refusal that apt and yum give, got %v", err)
	}
	if len(*runs) != 0 {
		t.Fatalf("an empty approved set must run no package manager at all: %v", argsOf(*runs))
	}
}

func TestDeployLinuxUpdatesRefusesToDeployWhatItCannotRecord(t *testing.T) {
	stubVersion(t, func(source, name string) (string, bool) { return "", false })
	runs := recordRunner(t, nil)
	deploymentID := "ourway-253-unrecorded"
	t.Cleanup(func() { clearPreDeployState(deploymentID) })

	err := deployLinuxUpdates(deploymentID, []Update{{Source: "zypper", Title: "glibc", Version: "2.31-150300.10.1"}})
	if err == nil || !strings.Contains(err.Error(), "cannot read the installed version of glibc") {
		t.Fatalf("expected a refusal naming the package, got %v", err)
	}
	if len(*runs) != 0 {
		t.Fatalf("a package with no readable previous version must not be deployed: %v", argsOf(*runs))
	}
	state, serr := loadPreDeployState(deploymentID)
	if serr == nil && len(state) != 0 {
		t.Fatalf("no rollback state may be recorded for a package that was not deployed: %+v", state)
	}
}

func TestDeployLinuxUpdatesFailsAStuckPackageManagerInsteadOfHanging(t *testing.T) {
	// What this proves, and what it does not. It proves the **deploy path's**
	// behaviour when a package manager comes back killed at its deadline: the
	// deployment fails, the operator's message says "timed out", and nothing after
	// it is attempted. It does not prove that the deadline **fires** — `runPkg` is
	// replaced here, so the timeout is an input, not a measurement. Firing is
	// `runPkgReal`'s job (`pkgcmd.go`): `exec.CommandContext` plus a capture file
	// rather than a pipe, so `Wait` returns when the process dies instead of
	// waiting on a scriptlet-spawned daemon that inherited the pipe's write end.
	// Proving that end-to-end needs a real hung subprocess, which needs a real
	// package manager host; owed, and named in the commit as owed.
	stubVersion(t, fixedVersions(map[string]string{"curl": "8.19.0-1"}))
	runs := recordRunner(t, map[string]pkgResult{
		"pacman": {code: -1, startErr: errors.New("pacman -U --noconfirm https://geo.mirror.pkgbuild.com/extra/os/x86_64/curl-8.20.0-1-x86_64.pkg.tar.zst timed out after 1h0m0s and was killed")},
	})

	err := deployLinuxUpdates("", []Update{{Source: "pacman", Title: "curl", Version: "8.20.0-1"}})
	if err == nil {
		t.Fatal("a package manager killed at its deadline must fail the deployment")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("the failure the operator sees must say the package manager timed out, got: %v", err)
	}
	// The killed run stops here: nothing after it is attempted, and in
	// particular nothing that would upgrade the rest of the system.
	for _, line := range argsOf(*runs) {
		if strings.Contains(line, "-Syu") || strings.Contains(line, "sysupgrade") {
			t.Fatalf("a timeout must not widen into a full upgrade: %q", line)
		}
	}
}

func TestPacmanCandidateURLUsesTheApprovedVersionOnly(t *testing.T) {
	// `pacman -S --print` prints the whole resolved transaction: the approved
	// package plus dependencies. Only the approved line may be installed.
	runs := recordRunner(t, map[string]pkgResult{
		"pacman": {code: 0, output: strings.Join([]string{
			"glibc 2.42-5 https://geo.mirror.pkgbuild.com/core/os/x86_64/glibc-2.42-5-x86_64.pkg.tar.zst",
			"curl 8.20.0-1 https://geo.mirror.pkgbuild.com/extra/os/x86_64/curl-8.20.0-1-x86_64.pkg.tar.zst",
			"ca-certificates-mozilla 2:3.106-1 https://geo.mirror.pkgbuild.com/core/os/x86_64/ca-certificates-mozilla-3.106-1-any.pkg.tar.zst",
			"",
		}, "\n")},
	})
	url, err := pacmanCandidateURL(Update{Source: "pacman", Title: "curl", Version: "8.20.0-1"})
	if err != nil {
		t.Fatalf("candidate URL: %v", err)
	}
	if url != "https://geo.mirror.pkgbuild.com/extra/os/x86_64/curl-8.20.0-1-x86_64.pkg.tar.zst" {
		t.Fatalf("must pick the approved package's own line, got %q", url)
	}
	if got := argsOf(*runs); len(got) != 1 {
		t.Fatalf("resolving a candidate must run exactly one command, got %v", got)
	}
}

func TestPacmanCandidateURLRefusesAVersionTheDatabasesDoNotHave(t *testing.T) {
	recordRunner(t, map[string]pkgResult{
		"pacman": {code: 0, output: "curl 8.21.0-1 https://geo.mirror.pkgbuild.com/extra/os/x86_64/curl-8.21.0-1-x86_64.pkg.tar.zst\n"},
	})
	_, err := pacmanCandidateURL(Update{Source: "pacman", Title: "curl", Version: "8.20.0-1"})
	if err == nil {
		t.Fatal("installing a different version from the approved one must be refused, not silently accepted")
	}
	if !strings.Contains(err.Error(), "curl-8.20.0-1") {
		t.Errorf("the refusal must name the approved version, got: %v", err)
	}
}

func TestZypperScanResultReadsTheTableNotTheExitCode(t *testing.T) {
	table := "S | Repository | Name | Current Version | New Version\n" +
		"--+------------+------+-------------------+------------\n" +
		"v | openSUSE-Tumbleweed-Oss | curl | 8.19.0-3.1 | 8.20.0-1.1\n"

	// An update listing reports "updates applicable" as an informational
	// non-zero exit, so the parsed table decides and the exit code does not.
	updates, err := zypperScanResult(pkgResult{code: 101, output: table})
	if err != nil {
		t.Fatalf("exit 101 with a table must not be an error: %v", err)
	}
	if len(updates) != 1 || updates[0].Version != "8.20.0-1.1" || updates[0].Source != "zypper" {
		t.Fatalf("table read after an informational exit: %+v", updates)
	}

	if updates, err := zypperScanResult(pkgResult{code: 0}); len(updates) != 0 || err != nil {
		t.Fatalf("zypper saying nothing means no updates: %v %v", updates, err)
	}
	if _, err := zypperScanResult(pkgResult{code: 8, output: "Repo openSUSE-Tumbleweed-Oss is not reachable.\n"}); err == nil {
		t.Fatal("a failed zypper run must be reported, not read as an empty table")
	}
	if _, err := zypperScanResult(pkgResult{code: 0, output: "Loading repository data...\nA table this parser does not know.\n"}); err == nil {
		t.Fatal("unrecognised output on a successful run must be reported, not read as up to date forever")
	}
	if _, err := zypperScanResult(pkgResult{code: -1, startErr: errors.New("zypper lu timed out after 10m0s and was killed")}); err == nil {
		t.Fatal("a zypper killed at its deadline must be reported as a failed scan")
	}
}

func TestParseZypperListUpdatesFixtures(t *testing.T) {
	tumbleweed := mustParseZypper(t, fixture(t, "zypper_lu_tumbleweed.txt"))
	if len(tumbleweed) != 6 {
		t.Fatalf("Tumbleweed fixture: want 6 rows, got %d: %+v", len(tumbleweed), tumbleweed)
	}
	first := tumbleweed[0]
	if first.Source != "zypper" || first.Title != "MozillaFirefox" || first.Version != "142.0-1.1" || first.ExternalID != "MozillaFirefox-142.0-1.1" {
		t.Errorf("row must carry the New Version column: %+v", first)
	}
	if first.KB != "" || first.Severity != "" || first.Category != "" {
		t.Errorf("zypper list-updates reports no article number, severity or category; the agent must not invent them: %+v", first)
	}

	leap := mustParseZypper(t, fixture(t, "zypper_lu_leap.txt"))
	if len(leap) != 6 {
		t.Fatalf("Leap fixture: want 6 rows, got %d: %+v", len(leap), leap)
	}
	// The Leap fixture's table carries an Arch column where the Tumbleweed one
	// carries Status, and ragged padding on one row: reading by column name
	// means neither shifts a version into the wrong field.
	want := map[string]string{
		"kernel-default":   "5.14.21-150500.60.1",
		"glibc":            "2.31-150300.10.1",
		"libopenssl1_1":    "1.1.1w-150400.8.1",
		"sudo":             "1.9.17p1-150400.3.1",
		"zlib":             "1.2.11-11.18",
		"yast2-trans-de":   "4.3.0-150400.4.1",
	}
	seen := map[string]string{}
	for _, u := range leap {
		seen[u.Title] = u.Version
	}
	for name, version := range want {
		if seen[name] != version {
			t.Errorf("%s: want New Version %s, got %q (rows: %v)", name, version, seen[name], seen)
		}
	}
}

func mustParseZypper(t *testing.T, output string) []Update {
	t.Helper()
	updates, err := parseZypperListUpdates(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return updates
}

func TestParseZypperListUpdatesRefusesATableItCannotRead(t *testing.T) {
	if _, err := parseZypperListUpdates("Loading repository data...\nReading installed packages...\n"); err == nil {
		t.Fatal("output with no table header must be an error, not a silent empty answer")
	}
	if _, err := parseZypperListUpdates("S | Repository | Name | Current Version\n--+---+---+---\nv | repo | curl | 8.19.0-3.1\n"); err == nil {
		t.Fatal("a table without a New Version column must be an error: that column is what would be installed")
	}
	if updates, err := parseZypperListUpdates("S | Repository | Name | New Version\n--+---+---+---\nv | repo | curl | 8.20.0-1.1\nv | repo |  | 9.9\n"); err != nil || len(updates) != 1 {
		t.Fatalf("a row with no package name must be skipped, not deployed: %v %+v", err, updates)
	}
}

func TestRollbackLinuxUpdatesReportsEveryFailedPackage(t *testing.T) {
	deploymentID := "ourway-253-rollback-partial"
	state := []preDeployEntry{
		{Package: "glibc", Source: "zypper", OldVersion: "2.31-150300.9.1"},
		{Package: "openssl", Source: "zypper", OldVersion: "1.1.1w-150400.7.1"},
		{Package: "yay", Source: "zypper", OldVersion: ""},
	}
	if err := savePreDeployState(deploymentID, state); err != nil {
		t.Fatalf("record pre-deploy state: %v", err)
	}
	t.Cleanup(func() { clearPreDeployState(deploymentID) })

	runs := recordRunner(t, map[string]pkgResult{
		"zypper": {code: 4, output: "Problem: the requested solvable is no longer available\n"},
	})

	err := rollbackLinuxUpdates(deploymentID)
	if err == nil {
		t.Fatal("a rollback that restored nothing must not report success (buglist.md M14)")
	}
	msg := err.Error()
	for _, want := range []string{"did not restore 2 of 3 packages", "glibc", "2.31-150300.9.1", "openssl", "1.1.1w-150400.7.1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the rollback result must name %q, got: %s", want, msg)
		}
	}
	if strings.Contains(msg, "yay") {
		t.Errorf("a package that rolled back cleanly must not be reported as failed, got: %s", msg)
	}

	want := []string{
		"zypper --non-interactive install --oldpackage glibc=2.31-150300.9.1",
		"zypper --non-interactive install --oldpackage openssl=1.1.1w-150400.7.1",
		"zypper --non-interactive remove yay",
	}
	got := argsOf(*runs)
	if len(got) != len(want) {
		t.Fatalf("want exactly %d commands, got %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command %d:\n want %s\n got  %s", i, want[i], got[i])
		}
	}

	// A rollback that did not restore everything keeps its state, so the
	// operator can try again instead of losing the recorded versions.
	again, lerr := loadPreDeployState(deploymentID)
	if lerr != nil || len(again) != len(state) {
		t.Errorf("a partial rollback must keep its recorded versions: %v %+v", lerr, again)
	}
}

func TestRollbackLinuxUpdatesClearsStateWhenEverythingWasRestored(t *testing.T) {
	deploymentID := "ourway-253-rollback-clean"
	if err := savePreDeployState(deploymentID, []preDeployEntry{
		{Package: "glibc", Source: "zypper", OldVersion: "2.31-150300.9.1"},
	}); err != nil {
		t.Fatalf("record pre-deploy state: %v", err)
	}
	t.Cleanup(func() { clearPreDeployState(deploymentID) })

	runs := recordRunner(t, map[string]pkgResult{"zypper": {code: 0}})
	if err := rollbackLinuxUpdates(deploymentID); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := argsOf(*runs); len(got) != 1 || got[0] != "zypper --non-interactive install --oldpackage glibc=2.31-150300.9.1" {
		t.Fatalf("rollback must restore the recorded version with --oldpackage, got %v", got)
	}
	if _, err := loadPreDeployState(deploymentID); err == nil {
		t.Fatal("a rollback that restored everything must clear the state it consumed")
	}
}

// TestRollbackLinuxUpdatesAptOnlyReportIsUnchanged pins the behaviour the apt
// and yum/dnf paths shipped with in 2.4.x: a failed downgrade is reported as
// the one failed package, in the same words, and the rollback state is kept.
// The 2.5-3 aggregate account is for the new sources only.
//
// This test is the only one here that reaches a real program: the apt path
// goes through aptCmd and exec, not through the runner seam, so the `apt-get`
// on the test machine is actually attempted. It cannot do anything — it is
// refused unprivileged, and the package name is one no repository carries.
func TestRollbackLinuxUpdatesAptOnlyReportIsUnchanged(t *testing.T) {
	deploymentID := "ourway-253-rollback-apt"
	state := []preDeployEntry{{Package: "ourway-not-a-real-package", Source: "apt", OldVersion: "0.0-1"}}
	if err := savePreDeployState(deploymentID, state); err != nil {
		t.Fatalf("record pre-deploy state: %v", err)
	}
	t.Cleanup(func() { clearPreDeployState(deploymentID) })

	err := rollbackLinuxUpdates(deploymentID)
	if err == nil {
		t.Skip("this machine installed a package that does not exist; the assumption of this test is wrong")
	}
	if !strings.Contains(err.Error(), "downgrade ourway-not-a-real-package to 0.0-1 failed") {
		t.Fatalf("apt rollback must report in the words it used before 2.5-3, got: %v", err)
	}
	if strings.Contains(err.Error(), "did not restore") {
		t.Fatalf("the new aggregate wording must not appear for an apt-only rollback: %v", err)
	}
	if again, lerr := loadPreDeployState(deploymentID); lerr != nil || len(again) != 1 {
		t.Errorf("a failed rollback must keep its recorded versions: %v %+v", lerr, again)
	}
}
