//go:build linux

package session

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// x11Session describes a running X server and the user who owns it.
type x11Session struct {
	uid        uint32
	display    string
	xauthority string
}

// findX11Session locates a running Xorg and returns its display name,
// auth file and owner by reading /proc. Wayland-only sessions are not
// found (xdotool and scrot are X11 tools).
func findX11Session() (*x11Session, error) {
	entries, err := filepath.Glob("/proc/[0-9]*")
	if err != nil {
		return nil, err
	}
	for _, dir := range entries {
		comm, err := os.ReadFile(filepath.Join(dir, "comm"))
		if err != nil {
			continue
		}
		if name := strings.TrimSpace(string(comm)); name != "Xorg" && name != "X" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			continue
		}
		fi, err := os.Stat(dir)
		if err != nil {
			continue
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		s := &x11Session{uid: st.Uid, display: ":0"}
		args := bytes.Split(raw, []byte{0})
		for i, a := range args {
			t := string(a)
			switch {
			case strings.HasPrefix(t, ":") && len(t) > 1:
				s.display = t
			case t == "-auth" && i+1 < len(args):
				s.xauthority = string(args[i+1])
			}
		}
		return s, nil
	}
	return nil, fmt.Errorf("no X11 session found (Wayland is not supported)")
}

// spawnRemoteProc launches the remote-control exe as the user who owns
// the X display, with that display's environment.
func spawnRemoteProc(bin string, args []string) (*os.Process, error) {
	sess, err := findX11Session()
	if err != nil {
		return nil, err
	}
	cred, home, username, err := credentialFor(sess.uid)
	if err != nil {
		return nil, err
	}
	xauth := sess.xauthority
	// The display manager's auth file (-auth) is root-only for some
	// setups; the user's own file is the fallback.
	if xauth == "" || !readableBy(xauth, sess.uid) {
		xauth = filepath.Join(home, ".Xauthority")
	}
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	cmd.Env = []string{
		"DISPLAY=" + sess.display,
		"XAUTHORITY=" + xauth,
		"HOME=" + home,
		"USER=" + username,
		"PATH=/usr/local/bin:/usr/bin:/bin",
	}
	return startRemoteCmd(cmd)
}

// readableBy reports whether path exists and is world-readable or owned
// by uid.
func readableBy(path string, uid uint32) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if fi.Mode().Perm()&0o004 != 0 {
		return true
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uid
}
