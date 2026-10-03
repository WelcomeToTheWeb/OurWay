package sessions

import (
	"context"
	"log"
	"sync"
	"time"

	"ourway/server/store"
)

// staleSessionTTL is how long a session may sit in "pending" state (no
// frame has ever arrived from the agent) before the reaper ends it.
// Browsers that close before the agent starts capturing leave such rows
// behind; without the reaper they show up as phantom sessions forever
// (H6).
const staleSessionTTL = 5 * time.Minute

// Gateway owns the background reaper that closes stale session rows in
// the database.
//
// Frames travel over HTTP (ReportFrame) and input over the device
// WebSocket, so the gateway no longer holds WebRTC peer connections.
// The Session row in the store is the source of truth for a session's
// lifetime; the reaper is what keeps abandoned sessions from leaking.
type Gateway struct {
	store *store.Store

	stop     chan struct{}
	stopOnce sync.Once
}

// NewGateway creates a new session gateway and starts the background
// reaper that ends abandoned sessions. The reaper stops when ctx is
// cancelled, so callers that build a router repeatedly (tests) don't
// accumulate leaked goroutines (H6).
func NewGateway(ctx context.Context, st *store.Store) *Gateway {
	g := &Gateway{
		store: st,
		stop:  make(chan struct{}),
	}
	go g.reap(ctx)
	return g
}

// Stop shuts down the gateway's background reaper. Safe to call multiple
// times.
func (g *Gateway) Stop() {
	g.stopOnce.Do(func() { close(g.stop) })
}

// reap periodically ends sessions that the reaper's rules consider stale.
func (g *Gateway) reap(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-g.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.reapStaleSessions()
		}
	}
}

// reapStaleSessions ends:
//  1. sessions that stayed "pending" past the TTL — the agent never sent
//     a single frame (abandoned browser, agent with no display, session
//     start that never reached the device);
//  2. "active" sessions whose device has since gone offline — no frames
//     or input can cross the link, so the session is over even if the
//     browser tab is still open.
func (g *Gateway) reapStaleSessions() {
	// Rule 1: stale pending sessions.
	pending, err := g.store.Sessions.ListStalePending(time.Now().Add(-staleSessionTTL))
	if err != nil {
		log.Printf("sessions: failed to list stale pending sessions: %v", err)
	}
	for _, s := range pending {
		log.Printf("sessions: reaping pending session %s (created %s)", s.ID, s.CreatedAt.Format(time.RFC3339))
		if err := g.store.Sessions.EndSession(s.ID); err != nil {
			log.Printf("sessions: failed to reap session %s: %v", s.ID, err)
		}
	}

	// Rule 2: active sessions with an offline device.
	active, err := g.store.Sessions.ListActiveAll()
	if err != nil {
		log.Printf("sessions: failed to list active sessions: %v", err)
		return
	}
	for _, s := range active {
		if s.Status != "active" {
			continue
		}
		dev, err := g.store.Devices.GetByID(s.DeviceID)
		if err != nil {
			// Device row gone: the session is a phantom; end it.
			log.Printf("sessions: reaping session %s: device %s no longer exists", s.ID, s.DeviceID)
			if err := g.store.Sessions.EndSession(s.ID); err != nil {
				log.Printf("sessions: failed to reap session %s: %v", s.ID, err)
			}
			continue
		}
		if dev.Status == "offline" {
			log.Printf("sessions: reaping active session %s: device %s is offline", s.ID, dev.Name)
			if err := g.store.Sessions.EndSession(s.ID); err != nil {
				log.Printf("sessions: failed to reap session %s: %v", s.ID, err)
			}
		}
	}
}
