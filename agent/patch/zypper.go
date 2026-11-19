package patch

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// Zypper updates (openSUSE Tumbleweed and Leap, and SLES).

// scanZypperUpdates lists the updates `zypper lu` would install.
//
// `zypper list-updates` lists only installable updates — those with no
// dependency problem and no vendor change — which is exactly what `zypper
// update` would offer, so the scan and the deployment agree about what is
// installable on this host instead of disagreeing about what merely exists.
//
// `--non-interactive` is a global option (it goes before the command) and it
// is here for the same reason `--noconfirm` is on the pacman commands: a
// repository whose signing key is not yet in the keyring otherwise asks a
// question of an agent that has no terminal. Non-interactively zypper refuses
// it, which is a scan that fails and says why, not a hang.
func scanZypperUpdates() []Update {
	if _, err := exec.LookPath("zypper"); err != nil {
		return nil
	}
	res := runPkg(pkgScanTimeout, "zypper", "--non-interactive", "-q", "lu")
	updates, err := zypperScanResult(res)
	if err != nil {
		log.Printf("scan zypper: %v", err)
	}
	return updates
}

// zypperScanResult turns one `zypper lu` run into update rows, or explains why
// the run produced none.
//
// A non-zero exit is not by itself a failure of the scan: zypper reports
// "updates applicable" as an informational exit code (101) beside 0, so the
// parsed table decides and the exit code is only reported when nothing parsed.
// Printing nothing at all is the genuine "no updates" answer; printing
// something this parser does not recognise is a bug worth a log line, because
// silence here would look like an up-to-date host forever.
func zypperScanResult(res pkgResult) ([]Update, error) {
	if res.startErr != nil {
		return nil, res.startErr
	}
	updates, err := parseZypperListUpdates(res.output)
	if err == nil {
		return updates, nil
	}
	if res.code != 0 {
		return nil, fmt.Errorf("zypper lu exited %d: %s", res.code, truncate(strings.TrimSpace(res.output), 200))
	}
	if strings.TrimSpace(res.output) == "" {
		return nil, nil
	}
	return nil, fmt.Errorf("%v: %s", err, truncate(strings.TrimSpace(res.output), 200))
}

// parseZypperListUpdates turns `zypper lu` table output into update rows.
//
// zypper prints a ruled table whose columns depend on the zypper version and
// on the options passed (older builds add an Arch column, newer ones a Status
// column), so the columns are located by name in the header row rather than by
// position:
//
//	S | Repository          | Name   | Current Version  | New Version
//	--+---------------------+--------+------------------+---------------
//	v | openSUSE-Leap-15.5  | curl   | 8.3.0-150500.1.1 | 8.4.0-150500.2.1
//
// `New Version` is what a deployment installs, so a table that has no Name or
// no New Version column is reported as an error rather than guessed at.
//
// The table carries no severity and no category: those belong to zypper's
// patch listing (`zypper list-patches`), where a row is a patch and not an
// installable package, and this scan deploys packages. Those columns — and KB,
// which names a Windows Update article — stay empty rather than being filled
// with something the host never said. ExternalID is `name-version`, the
// identity the server dedups a re-scan against.
func parseZypperListUpdates(output string) ([]Update, error) {
	var updates []Update
	var header []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "|") {
			continue
		}
		cells := zypperCells(line)
		if isZypperRule(cells) {
			continue
		}
		if zypperCol(cells, "New Version") >= 0 && zypperCol(cells, "Name") >= 0 {
			header = cells
			continue
		}
		if len(header) == 0 {
			continue
		}
		name := zypperCell(cells, header, "Name")
		version := zypperCell(cells, header, "New Version")
		if name == "" || version == "" {
			continue
		}
		updates = append(updates, Update{
			Source:     "zypper",
			Title:      name,
			Version:    version,
			ExternalID: name + "-" + version,
		})
	}
	if len(header) == 0 {
		return nil, fmt.Errorf("zypper list-updates output has no recognised table header")
	}
	return updates, nil
}

// zypperCells splits one ruled row into its columns. Interior whitespace runs
// are collapsed because the column separators are padded by eye, and a header
// cell such as `New  Version` must still match the name being looked for. No
// cell this parser uses — a package name, a version, a repository alias —
// contains a space of its own.
func zypperCells(line string) []string {
	parts := strings.Split(line, "|")
	for i, p := range parts {
		parts[i] = strings.Join(strings.Fields(p), " ")
	}
	return parts
}

// isZypperRule reports the separator row printed under a table header
// (`--+----------------+------`).
func isZypperRule(cells []string) bool {
	for _, c := range cells {
		if c == "" {
			continue
		}
		if !strings.Contains(c, "-") {
			return false
		}
		for _, r := range c {
			if r != '-' && r != '+' && r != ' ' {
				return false
			}
		}
	}
	return true
}

// zypperCol returns the index of a named column in a header row, or -1.
func zypperCol(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}

// zypperCell reads one named column out of a data row. A row that is shorter
// than the header — a cell truncated by zypper's own column fitting — yields
// "" and is skipped by the caller rather than read from the wrong column.
func zypperCell(cells, header []string, name string) string {
	i := zypperCol(header, name)
	if i < 0 || i >= len(cells) {
		return ""
	}
	return cells[i]
}

// installZypperUpdate updates exactly the approved package with
// `zypper --non-interactive update <name>`, the command the plan names.
// `zypper update` with no package name is a whole-system update, which is what
// the guard in deployLinuxUpdates exists to refuse.
//
// `--non-interactive` answers zypper's prompts with their default instead of
// waiting for a terminal, and it is deliberately NOT joined by
// `--auto-agree-with-licenses`: an agent must not accept a third-party licence
// on an operator's behalf. A package whose licence has not been accepted fails
// the deployment with zypper's own message, and the operator accepts it once,
// in a terminal, on purpose.
//
// Difference from the pacman path, stated because it is a real one: this names
// the package, not the scanned version, so zypper installs the newest
// installable version it has — which for a package whose repository moved
// forward between the scan and the deployment is newer than what was approved,
// never older. Pinning `name=VERSION-RELEASE` here would instead fail the
// moment the repository rolled past the scanned solvable, and a failed
// deployment on a patched host is the worse outcome of the two.
func installZypperUpdate(u Update) (string, error) {
	res := runPkg(pkgDeployTimeout, "zypper", "--non-interactive", "update", u.Title)
	return res.output, pkgFailure("zypper update", u.Title, res)
}

// rollbackZypperEntry restores the recorded `name=VERSION-RELEASE`.
//
// `--oldpackage` is zypper's own documented rollback switch: it permits
// replacing an installed item with an older one and, unlike `--force`, leaves
// the host alone when the requested version is already installed. Leap keeps
// every released update in its update repositories, so the recorded version is
// usually still resolvable there; a rolling Tumbleweed snapshot may have
// dropped it, and then this fails with zypper's message instead of reporting a
// restore that did not happen.
//
// A new package (no recorded previous version) is removed again, as the apt
// and yum/dnf paths do for the same case.
func rollbackZypperEntry(e preDeployEntry) (string, error) {
	if e.OldVersion == "" {
		res := runPkg(pkgDeployTimeout, "zypper", "--non-interactive", "remove", e.Package)
		return res.output, pkgFailure("zypper remove", e.Package, res)
	}
	capability := e.Package + "=" + e.OldVersion
	res := runPkg(pkgDeployTimeout, "zypper", "--non-interactive", "install", "--oldpackage", capability)
	return res.output, pkgFailure("zypper install --oldpackage", capability, res)
}
