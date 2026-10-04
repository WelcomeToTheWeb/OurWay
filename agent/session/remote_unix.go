//go:build linux || darwin

package session

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// remoteSplitNeeded reports whether the exe must be launched into a
// user's graphical session: true when the agent runs as root (the
// service), whose own environment has no display.
func remoteSplitNeeded() bool { return os.Geteuid() == 0 }

const remoteLogPath = "/var/log/ourway-remote.log"

// startRemoteCmd starts cmd with its output appended to the remote log.
func startRemoteCmd(cmd *exec.Cmd) (*os.Process, error) {
	if f, err := os.OpenFile(remoteLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start remote exe: %w", err)
	}
	// The exe is supervised through its process handle and exits when
	// the session ends; reap it so it never lingers as a zombie.
	go cmd.Wait()
	return cmd.Process, nil
}

// credentialFor returns the syscall credential, home directory and
// username for uid.
func credentialFor(uid uint32) (*syscall.Credential, string, string, error) {
	u, err := user.LookupId(strconv.Itoa(int(uid)))
	if err != nil {
		return nil, "", "", fmt.Errorf("lookup uid %d: %w", uid, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse gid %q: %w", u.Gid, err)
	}
	return &syscall.Credential{Uid: uid, Gid: uint32(gid)}, u.HomeDir, u.Username, nil
}
