//go:build windows

package main

import (
	"context"
	"log"
	"os"
	"strings"

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
