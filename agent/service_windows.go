//go:build windows

package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/svc"

	"ourway/agent/config"
	"ourway/agent/install"
	"ourway/agent/session"
)

// agentService is the Windows service handler for the agent. It drives the
// agent loop and responds to SCM control requests (stop/shutdown).
type agentService struct {
	cfg *config.Config
}

// Execute implements svc.Handler. The service runtime calls it at startup;
// it must keep the SCM informed via changes and block until the service is
// stopped.
func (s *agentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	// Record native crashes: a service has no stderr, so an access
	// violation in a Win32 call would otherwise be completely silent.
	session.InstallCrashFilter()

	// Services have no console — route log output to a file or it is
	// silently discarded. A configured LogFile wins; otherwise default
	// to %ProgramData%\OurWay\agent.log.
	logPath := s.cfg.LogFile
	if logPath == "" {
		if err := os.MkdirAll(filepath.Join(os.Getenv("ProgramData"), "OurWay"), 0o755); err != nil {
			log.Printf("Warning: could not create log dir: %v", err)
		}
		logPath = filepath.Join(os.Getenv("ProgramData"), "OurWay", "agent.log")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		log.Printf("Warning: could not create log dir %s: %v", filepath.Dir(logPath), err)
	} else {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Printf("Warning: could not open log file %s: %v", logPath, err)
		} else {
			defer f.Close()
			log.SetOutput(f)
			log.Printf("Starting %s as a service (logging to %s)", install.ServiceName, logPath)
		}
	}

	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAgent(ctx, s.cfg)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				break loop
			default:
				log.Printf("Unexpected service control request #%d", c)
			}
		}
	}

	cancel()
	<-done

	changes <- svc.Status{State: svc.StopPending}
	return false, 0
}

// isWindowsService reports whether the process is running as a Windows
// service (i.e. was started by the SCM).
func isWindowsService() bool {
	inService, err := svc.IsWindowsService()
	if err != nil {
		log.Printf("Warning: could not determine service status: %v", err)
		return false
	}
	return inService
}

// runServiceMain starts the agent as a Windows service. It performs the SCM
// handshake and blocks until the service is stopped.
func runServiceMain(cfg *config.Config) error {
	log.Printf("Starting %s as a Windows service...", install.ServiceName)
	return svc.Run(install.ServiceName, &agentService{cfg: cfg})
}
