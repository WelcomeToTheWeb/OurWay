package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// installersDir is the directory containing the built agent installer
// artifacts (installers, agent binaries, install scripts). It mirrors
// config.Config.InstallersDir (INSTALLERS_DIR, default ./dist/agents) and is
// read at package init so tests can point it at a temp dir.
var installersDir = func() string {
	if v, ok := os.LookupEnv("INSTALLERS_DIR"); ok && v != "" {
		return v
	}
	return "./dist/agents"
}()

// SetInstallersDir overrides the installers directory. main calls this with
// cfg.InstallersDir so the loaded config is the single source of truth; the
// package-level default above keeps the package usable (and testsable)
// without a server bootstrap.
func SetInstallersDir(dir string) {
	if dir != "" {
		installersDir = dir
	}
}

// installerFileRe matches ourway-{kind}-{os}-{arch}[.exe] filenames so the
// list endpoint can report os and arch per file.
var installerFileRe = regexp.MustCompile(`^ourway-(installer|agent|remote)-(linux|darwin|windows)-(amd64|arm64|arm)(\.exe)?$`)

// installerInfo describes one downloadable installer artifact.
type installerInfo struct {
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

// osOrder ranks platforms for display: Windows first, then Linux, macOS,
// then anything unrecognized (scripts such as install.sh).
var osOrder = map[string]int{"windows": 0, "linux": 1, "darwin": 2}

// installerKind classifies an artifact: "installer" (self-contained
// ourway-installer-* CLI), "agent" (bare agent binary), or "script"
// (install.sh / install.ps1 and anything else).
func installerKind(name string) string {
	switch {
	case strings.HasPrefix(name, "ourway-installer-"):
		return "installer"
	case strings.HasPrefix(name, "ourway-agent-"):
		return "agent"
	case strings.HasPrefix(name, "ourway-remote-"):
		return "remote"
	default:
		return "script"
	}
}

// kindRank prefers the self-contained installer over the bare agent and
// over plain scripts.
func kindRank(kind string) int {
	switch kind {
	case "installer":
		return 0
	case "agent":
		return 1
	default:
		return 2
	}
}

// lessInstallers orders the list for display: OS rank, then arch (amd64
// before arm64), then kind (installer before agent), then name.
func lessInstallers(a, b installerInfo) bool {
	oa, ob := osOrder[a.OS], osOrder[b.OS]
	if oa != ob {
		return oa < ob
	}
	if a.Arch != b.Arch {
		if a.Arch == "amd64" {
			return true
		}
		if b.Arch == "amd64" {
			return false
		}
		return a.Arch < b.Arch
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Name < b.Name
}

// InstallerHandler serves the built installer artifacts. The routes are
// public (no auth middleware): onboarding happens before a tech has an
// account, so the files must be downloadable without credentials.
type InstallerHandler struct {
	dir string
}

// NewInstallerHandler creates an InstallerHandler rooted at dir.
func NewInstallerHandler(dir string) *InstallerHandler {
	return &InstallerHandler{dir: dir}
}

// List handles GET /api/v2/installers. It returns every regular file in the
// installers directory; a missing directory yields an empty list.
func (h *InstallerHandler) List(c *gin.Context) {
	installers := make([]installerInfo, 0)

	entries, err := os.ReadDir(h.dir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			name := entry.Name()
			kind := installerKind(name)
			osName, arch := parseInstallerName(name)
			installers = append(installers, installerInfo{
				Name:   name,
				OS:     osName,
				Arch:   arch,
				Kind:   kind,
				Size:   info.Size(),
				SHA256: sha256OfFile(filepath.Join(h.dir, name)),
				URL:    "v2/installers/" + name,
			})
		}
	}
	// For each platform, prefer the self-contained installer over the bare
	// agent binary so the list shows one entry per os/arch. Scripts (no
	// parsed OS) are kept as-is.
	seen := make(map[string]int, len(installers))
	deduped := make([]installerInfo, 0, len(installers))
	for _, inst := range installers {
		if inst.OS == "" {
			continue
		}
		key := inst.OS + "/" + inst.Arch
		if idx, ok := seen[key]; ok {
			// Keep the self-contained installer over a bare agent binary
			// regardless of directory iteration order.
			if kindRank(installers[idx].Kind) <= kindRank(inst.Kind) {
				continue
			}
			deduped[idx] = inst
			continue
		}
		seen[key] = len(deduped)
		deduped = append(deduped, inst)
	}
	for _, inst := range installers {
		if inst.OS == "" {
			deduped = append(deduped, inst)
		}
	}
	installers = deduped
	sort.Slice(installers, func(i, j int) bool { return lessInstallers(installers[i], installers[j]) })

	c.JSON(http.StatusOK, gin.H{"installers": installers})
}

// Download handles GET /api/v2/installers/{name}. It streams the file as an
// attachment; names containing /, \, .., or starting with . are rejected to
// prevent path traversal.
func (h *InstallerHandler) Download(c *gin.Context) {
	name := c.Param("name")
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") || strings.HasPrefix(name, ".") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid installer name", "name": name})
		return
	}

	file, err := os.Open(filepath.Join(h.dir, name))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "installer not found", "name": name})
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "installer not found", "name": name})
		return
	}

	contentType := "application/octet-stream"
	if ext := strings.ToLower(filepath.Ext(name)); ext == ".sh" || ext == ".ps1" {
		contentType = "text/plain"
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	c.Header("Content-Length", strconv.FormatInt(fileInfo.Size(), 10))
	c.DataFromReader(http.StatusOK, fileInfo.Size(), contentType, file, nil)
}

// parseInstallerName extracts os and arch from an
// ourway-{kind}-{os}-{arch}[.exe] filename. Both are "" when the name does
// not match the pattern.
func parseInstallerName(name string) (osName, arch string) {
	m := installerFileRe.FindStringSubmatch(name)
	if m == nil {
		return "", ""
	}
	return m[2], m[3]
}

// sha256OfFile computes the hex-encoded SHA-256 of a file, returning "" on
// read errors.
func sha256OfFile(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return ""
	}
	return hex.EncodeToString(hasher.Sum(nil))
}
