package patch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// This file, and the pacman.go and zypper.go beside it, carry the 2.5-3
// Linux patch parity work (docs/phase2.5-tasks.md).
//
// They are deliberately NOT build-tagged. agent/patch is the one package in
// the agent that keeps every OS path in untagged files and picks between
// them at runtime (runtime.GOOS in ScanUpdates, DeployUpdates and
// RollbackUpdates) — compare agent/collector/software_linux.go and
// agent/session/input_linux.go, which are tagged. There is no `_linux.go`
// split here to hide behind, and the switches that would call a tagged file
// are untagged, so tagging this code would break the `GOOS=windows` and
// `GOOS=darwin` builds the release gate runs. Everything here is portable
// stdlib: it compiles on every platform and runs only where the package
// manager exists (each scan is gated on exec.LookPath).

// Deadlines for the package managers added in 2.5-3.
//
// A stuck package manager must fail the deployment, not hang the agent. The
// handler already runs on its own goroutine (agent/client/client.go), so the
// heartbeat keeps running while a deploy is in progress; what an unbounded
// command costs is an agent that never reports this deployment at all, and a
// deployment an operator watches forever.
//
// Deviation, stated plainly: the apt and yum/dnf paths in patch.go still run
// with no deadline, as they did before 2.5-3. This change does not touch
// them, because their behaviour is what shipped in 2.4.x and the release
// review of this change is about pacman and zypper. They need the same
// treatment in their own change.
const (
	pkgScanTimeout    = 10 * time.Minute
	pkgDeployTimeout  = time.Hour
	pkgVersionTimeout = 30 * time.Second
)

// pkgResult is one package-manager run.
type pkgResult struct {
	// code is the command's exit status, or -1 when it never started or was
	// killed at its deadline.
	code int
	// output is everything the command wrote to stdout and stderr.
	output string
	// startErr is set only when the command could not be run at all: the
	// program is missing, or it was killed for running past its deadline.
	startErr error
}

// runPkg runs a Linux package manager with a deadline and returns its
// combined output and exit code. It is a package-level variable so a test can
// record exactly what would be handed to pacman or zypper — the whole point
// of "only the approved packages" — without a package manager and without
// touching a system.
var runPkg = runPkgReal

// runPkgReal is runPkg's implementation.
func runPkgReal(timeout time.Duration, name string, args ...string) pkgResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Combined output goes to a temporary file rather than a pipe, on
	// purpose. exec.Cmd kills the process at the deadline and then waits for
	// its own copy goroutines to see EOF — and a scriptlet-spawned daemon
	// that inherited the pipe's write end holds that EOF off indefinitely.
	// Wait would then block past the deadline and the timeout would bound
	// nothing. A file has no reader-side wait: Wait returns as soon as the
	// process itself dies.
	out, err := os.CreateTemp("", "ourway-pkg-")
	if err != nil {
		return pkgResult{code: -1, startErr: fmt.Errorf("capture %s output: %w", name, err)}
	}
	defer os.Remove(out.Name())

	cmd := exec.CommandContext(ctx, name, args...)
	// A locale is part of the output format: pacman translates its messages
	// and zypper translates its table headers. The parsers below read
	// English, and packageVersion below matches English failure text, so the
	// run is pinned to C.
	cmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
	cmd.Stdout = out
	cmd.Stderr = out

	runErr := cmd.Run()
	out.Close()

	res := pkgResult{code: -1}
	if cmd.ProcessState != nil {
		res.code = cmd.ProcessState.ExitCode()
	}
	if b, rerr := os.ReadFile(out.Name()); rerr == nil {
		res.output = string(b)
	}
	if ctx.Err() == context.DeadlineExceeded {
		// Killed at the deadline. Reported as a failure the caller turns into
		// a named deploy/rollback error — never as a hang, and never as a
		// success.
		res.code = -1
		res.startErr = fmt.Errorf("%s %s timed out after %s and was killed", name, strings.Join(args, " "), timeout)
		return res
	}
	if runErr != nil && res.code == -1 {
		// Killed by a signal, or never started (the program is missing).
		res.startErr = fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), runErr)
	}
	return res
}

// pkgFailure turns a package-manager run into an error when it did not
// succeed, naming the command and the tail of what it printed, so the message
// an operator reads in the console says which command failed and what it
// said about it.
func pkgFailure(what, target string, res pkgResult) error {
	if res.startErr != nil {
		return fmt.Errorf("%s %s: %w", what, target, res.startErr)
	}
	if res.code == 0 {
		return nil
	}
	return fmt.Errorf("%s %s failed (exit %d): %s", what, target, res.code, truncate(strings.TrimSpace(res.output), 300))
}

// packageVersion reports the version of a package as installed now, and
// whether the package manager could answer at all.
//
// The two answers are not the same and the difference is a safety property:
// rollbackLinuxUpdates reads an empty recorded previous version as "this
// package was newly installed, so a rollback removes it". A lookup that
// merely failed — a locked or corrupt database — must not be recorded as
// "not installed", because that turns a failed rollback into the removal of a
// package the host still needs.
//
// It is a package-level variable so a test can pin the recorded previous
// versions without a dpkg, rpm or pacman database on the test machine.
var packageVersion = installedPackageVersion

// installedPackageVersion is packageVersion's implementation.
func installedPackageVersion(source, name string) (string, bool) {
	switch source {
	case "pacman":
		res := runPkg(pkgVersionTimeout, "pacman", "-Q", name)
		if res.startErr != nil {
			return "", false
		}
		if res.code == 0 {
			return parsePacmanQuery(res.output, name), true
		}
		// The one failure that is itself an answer, and needs no version: the
		// package is not in the local database, so it was newly installed by
		// the deployment. Pinned to the C locale by runPkg, which is what
		// makes this the message pacman actually prints.
		if strings.Contains(res.output, "was not found") {
			return "", true
		}
		return "", false
	case "zypper":
		// openSUSE is RPM-based, so the installed version is rpm's — the same
		// query the yum/dnf path uses — and it is exactly the two numbers a
		// `name=VERSION-RELEASE` zypper capability is built from.
		res := runPkg(pkgVersionTimeout, "rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}", name)
		if res.startErr != nil {
			return "", false
		}
		if res.code == 0 {
			return strings.TrimSpace(res.output), true
		}
		if strings.Contains(res.output, "is not installed") {
			return "", true
		}
		return "", false
	default:
		// apt and yum/dnf keep the lookup they had before 2.5-3, and the
		// behaviour that goes with it.
		return currentPackageVersion(source, name), true
	}
}
