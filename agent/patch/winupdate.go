package patch

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Windows updates are enumerated and installed through the Windows Update
// Agent COM API (Microsoft.Update.Session), driven from PowerShell. This
// needs no extra module and lets us install exactly the approved update
// IDs rather than everything pending.

// wuScanScript lists available, visible software updates as a JSON array.
const wuScanScript = `
$ErrorActionPreference = 'Stop'
$session = New-Object -ComObject Microsoft.Update.Session
$result = $session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
$list = @()
foreach ($u in $result.Updates) {
  $cats = @(); foreach ($c in $u.Categories) { $cats += $c.Name }
  $kbs = @(); foreach ($k in $u.KBArticleIDs) { $kbs += $k }
  $list += [pscustomobject]@{
    Id = $u.Identity.UpdateID
    Title = $u.Title
    KB = ($kbs -join ',')
    Severity = [string]$u.MsrcSeverity
    Size = [int64]$u.MaxDownloadSize
    Category = ($cats -join ', ')
  }
}
ConvertTo-Json -InputObject @($list) -Compress
`

// wuInstallScript installs the update IDs in $env:OURWAY_UPDATE_IDS
// (comma separated) and prints a JSON result.
const wuInstallScript = `
$ErrorActionPreference = 'Stop'
$want = $env:OURWAY_UPDATE_IDS -split ','
$session = New-Object -ComObject Microsoft.Update.Session
$search = $session.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0")
$coll = New-Object -ComObject Microsoft.Update.UpdateColl
foreach ($u in $search.Updates) {
  if ($want -contains $u.Identity.UpdateID) {
    if (-not $u.EulaAccepted) { $u.AcceptEula() }
    [void]$coll.Add($u)
  }
}
if ($coll.Count -eq 0) {
  ConvertTo-Json -Compress @{ ok = $false; error = 'none of the approved updates are available on this device' }
  exit 0
}
$dl = $session.CreateUpdateDownloader(); $dl.Updates = $coll; [void]$dl.Download()
$inst = $session.CreateUpdateInstaller(); $inst.Updates = $coll
$r = $inst.Install()
$failed = @()
for ($i = 0; $i -lt $coll.Count; $i++) {
  $code = $r.GetUpdateResult($i).ResultCode
  if ($code -ne 2) { $failed += $coll.Item($i).Title }
}
ConvertTo-Json -Compress @{
  ok = ($r.ResultCode -eq 2)
  resultCode = [int]$r.ResultCode
  rebootRequired = [bool]$r.RebootRequired
  installed = $coll.Count - $failed.Count
  failed = @($failed)
}
`

type wuScanItem struct {
	ID       string `json:"Id"`
	Title    string `json:"Title"`
	KB       string `json:"KB"`
	Severity string `json:"Severity"`
	Size     int64  `json:"Size"`
	Category string `json:"Category"`
}

// parseWindowsScan converts the scan script's JSON into updates. A bare
// object (older PowerShell collapses one-element arrays) is accepted too.
func parseWindowsScan(out string) ([]Update, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	var items []wuScanItem
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		var one wuScanItem
		if err2 := json.Unmarshal([]byte(out), &one); err2 != nil {
			return nil, fmt.Errorf("parse windows scan output: %w", err)
		}
		items = []wuScanItem{one}
	}
	updates := make([]Update, 0, len(items))
	for _, it := range items {
		if it.Title == "" {
			continue
		}
		updates = append(updates, Update{
			Source:     "windows-update",
			Title:      it.Title,
			ExternalID: it.ID,
			KB:         it.KB,
			Severity:   strings.ToLower(it.Severity),
			Category:   it.Category,
			SizeBytes:  it.Size,
		})
	}
	return updates, nil
}

// wuInstallResult is the install script's JSON output.
type wuInstallResult struct {
	OK             bool     `json:"ok"`
	Error          string   `json:"error"`
	ResultCode     int      `json:"resultCode"`
	RebootRequired bool     `json:"rebootRequired"`
	Installed      int      `json:"installed"`
	Failed         []string `json:"failed"`
}

// parseWindowsInstall interprets the install script's output. It returns
// whether a reboot is required and a non-nil error when any update failed.
func parseWindowsInstall(out string) (reboot bool, err error) {
	out = strings.TrimSpace(out)
	// PowerShell may print warnings before the JSON line.
	if i := strings.LastIndex(out, "{"); i > 0 {
		out = out[i:]
	}
	var r wuInstallResult
	if jerr := json.Unmarshal([]byte(out), &r); jerr != nil {
		return false, fmt.Errorf("unexpected windows install output: %q", truncate(out, 200))
	}
	if r.Error != "" {
		return false, fmt.Errorf("%s", r.Error)
	}
	if !r.OK {
		msg := fmt.Sprintf("windows update result code %d", r.ResultCode)
		if len(r.Failed) > 0 {
			msg += ": failed: " + strings.Join(r.Failed, "; ")
		}
		return r.RebootRequired, fmt.Errorf("%s", msg)
	}
	return r.RebootRequired, nil
}

var updateIDRe = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// windowsUpdateIDs extracts the update IDs to install, rejecting anything
// that is not a GUID (the value is handed to PowerShell via the
// environment, but it must still never be attacker-shaped).
func windowsUpdateIDs(updates []Update) ([]string, error) {
	ids := make([]string, 0, len(updates))
	for _, u := range updates {
		if u.ExternalID == "" {
			return nil, fmt.Errorf("update %q has no Windows Update ID; rescan the device", u.Title)
		}
		if !updateIDRe.MatchString(u.ExternalID) {
			return nil, fmt.Errorf("update %q has an invalid Windows Update ID", u.Title)
		}
		ids = append(ids, u.ExternalID)
	}
	return ids, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
