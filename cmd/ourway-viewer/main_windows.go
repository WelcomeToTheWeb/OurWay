//go:build windows

package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"gioui.org/app"
	"golang.org/x/sys/windows"
)

func msgBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	windows.MessageBox(0, m, t, windows.MB_OK)
}

func main() {
	args := os.Args[1:]
	if len(args) == 1 {
		switch args[0] {
		case "--register":
			if err := registerProtocol(); err != nil {
				msgBox("OurWay Viewer", "Could not register the ourway:// handler: "+err.Error())
				os.Exit(1)
			}
			return
		case "--unregister":
			if err := unregisterProtocol(); err != nil {
				msgBox("OurWay Viewer", "Could not remove the ourway:// handler: "+err.Error())
				os.Exit(1)
			}
			return
		}
	}

	// Self-heal the protocol registration (per-user, no elevation).
	if err := registerProtocol(); err != nil {
		log.Printf("register protocol: %v", err)
	}

	if len(args) != 1 || !strings.HasPrefix(args[0], "ourway://") {
		msgBox("OurWay Viewer", "The viewer is registered.\n\nStart a remote session from the OurWay console and choose \"Open in viewer\".")
		return
	}
	launch, err := ParseLaunchURL(args[0])
	if err != nil {
		msgBox("OurWay Viewer", "Invalid launch link: "+err.Error())
		os.Exit(1)
	}

	if relaunched := autoUpdate(launch, args[0]); relaunched {
		return
	}

	v := newViewer(launch)
	ctx, cancel := context.WithCancel(context.Background())
	v.connect(ctx)
	go func() {
		defer cancel()
		if err := v.run(); err != nil {
			log.Printf("viewer: %v", err)
		}
		os.Exit(0)
	}()
	app.Main()
}

// autoUpdate brings the viewer up to date with the server it was
// launched from, then starts the new build with the same launch URL. It
// reports true when a new process took over. Any failure is logged and
// the current build carries on: a session must never be blocked by an
// update problem.
func autoUpdate(l Launch, launchURL string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	removeLeftovers(exe)
	if os.Getenv("OURWAY_VIEWER_NO_UPDATE") != "" || os.Getenv("OURWAY_VIEWER_UPDATED") != "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	u := NewUpdater(l.Server, exe)
	want, err := u.Check(ctx)
	if err != nil {
		log.Printf("viewer update check: %v", err)
		return false
	}
	if want == "" {
		return false
	}
	log.Printf("viewer: updating to build %s", want[:12])
	if err := u.Apply(ctx, want); err != nil {
		log.Printf("viewer update failed: %v", err)
		return false
	}
	cmd := exec.Command(exe, launchURL)
	cmd.Env = append(os.Environ(), "OURWAY_VIEWER_UPDATED=1") // no update loop
	if err := cmd.Start(); err != nil {
		log.Printf("viewer: could not start the updated build: %v", err)
		return false
	}
	return true
}
