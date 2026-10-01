package sessions

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
)

// SessionState tracks the state of a single WebRTC session.
type SessionState struct {
	ID        string
	DeviceID  string
	UserID    string
	PC        *webrtc.PeerConnection
	Status    string
	DataCh    *webrtc.DataChannel
	CreatedAt time.Time
	mu        sync.RWMutex
}

// pendingSessionTTL is how long a session may stay in "pending" state
// before the reaper closes it. Browsers that close before answering leave
// a live PeerConnection behind; this reclaims it.
const pendingSessionTTL = 5 * time.Minute

// Gateway manages WebRTC peer connections for remote sessions.
type Gateway struct {
	sessions map[string]*SessionState
	mu       sync.RWMutex
	stop     chan struct{}
	stopOnce sync.Once
}

// NewGateway creates a new session gateway and starts the background
// reaper that closes abandoned pending sessions.
func NewGateway() *Gateway {
	g := &Gateway{
		sessions: make(map[string]*SessionState),
		stop:     make(chan struct{}),
	}
	go g.reap()
	return g
}

// Stop shuts down the gateway's background reaper. Safe to call multiple
// times.
func (g *Gateway) Stop() {
	g.stopOnce.Do(func() { close(g.stop) })
}

// reap periodically closes sessions that have been in "pending" state
// for longer than pendingSessionTTL.
func (g *Gateway) reap() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-g.stop:
			return
		case <-ticker.C:
			g.reapStaleSessions()
		}
	}
}

// reapStaleSessions finds and closes sessions that are still "pending"
// past the TTL. Session IDs are collected under the gateway lock first so
// CloseSession (which also takes the lock) cannot deadlock.
func (g *Gateway) reapStaleSessions() {
	now := time.Now()

	g.mu.Lock()
	var stale []*SessionState
	for _, s := range g.sessions {
		s.mu.RLock()
		pending := s.Status == "pending"
		created := s.CreatedAt
		s.mu.RUnlock()
		if pending && now.Sub(created) > pendingSessionTTL {
			stale = append(stale, s)
		}
	}
	g.mu.Unlock()

	for _, s := range stale {
		log.Printf("sessions: reaping pending session %s older than %s", s.ID, pendingSessionTTL)
		if err := g.CloseSession(s.ID); err != nil {
			log.Printf("sessions: failed to reap session %s: %v", s.ID, err)
		}
	}
}

// CreatePeerConnection creates a new WebRTC peer connection for a session.
func (g *Gateway) CreatePeerConnection(sessionID, deviceID, userID string) (*SessionState, error) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	state := &SessionState{
		ID:        sessionID,
		DeviceID:  deviceID,
		UserID:    userID,
		PC:        pc,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	// Setup data channel for input events and screen frames
	pc.OnDataChannel(func(ch *webrtc.DataChannel) {
		state.mu.Lock()
		state.DataCh = ch
		state.mu.Unlock()

		log.Printf("sessions: data channel opened for session %s", sessionID)

		ch.OnOpen(func() {
			log.Printf("sessions: data channel ready for session %s", sessionID)
		})

		ch.OnMessage(func(msg webrtc.DataChannelMessage) {
			// TODO(frames): relay screen frames arriving over this data
			// channel to the session's users (hub.BroadcastMessage
			// "session_frame"). Until the agent streams over WebRTC, frames
			// arrive via POST /api/sessions/:id/frame instead.
			_ = msg
		})
	})

	pc.OnConnectionStateChange(func(pcState webrtc.PeerConnectionState) {
		log.Printf("sessions: %s connection state: %s", sessionID, pcState.String())
		if pcState == webrtc.PeerConnectionStateFailed || pcState == webrtc.PeerConnectionStateClosed {
			state.mu.Lock()
			state.Status = "ended"
			state.mu.Unlock()
		}
	})

	g.mu.Lock()
	g.sessions[sessionID] = state
	g.mu.Unlock()

	return state, nil
}

// CreateOffer generates a WebRTC offer for the session.
func (g *Gateway) CreateOffer(sessionID string) (string, error) {
	state, ok := g.getSession(sessionID)
	if !ok {
		return "", fmt.Errorf("session %s not found", sessionID)
	}

	// Create a data channel for this session
	ch, err := state.PC.CreateDataChannel("ourway-session", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create data channel: %w", err)
	}

	state.mu.Lock()
	state.DataCh = ch
	state.mu.Unlock()

	// Register the ICE gathering callback before setting the local
	// description so the "complete" state cannot be missed.
	gathered := make(chan struct{})
	var gatherOnce sync.Once
	state.PC.OnICEGatheringStateChange(func(state webrtc.ICEGathererState) {
		if state == webrtc.ICEGathererStateComplete {
			gatherOnce.Do(func() { close(gathered) })
		}
	})

	offer, err := state.PC.CreateOffer(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create offer: %w", err)
	}

	if err := state.PC.SetLocalDescription(offer); err != nil {
		return "", fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait (bounded) for ICE gathering to complete so the returned offer
	// already embeds our candidates.
	select {
	case <-gathered:
	case <-time.After(3 * time.Second):
		// TODO(trickle-ICE): forward candidates that arrive after this 3s
		// window to the browser (e.g. pc.OnICECandidate -> WS
		// "ice_candidate" event on the session's user connection, consumed
		// via pc.addIceCandidate). Until then, late candidates are dropped
		// and the data channel may never open.
		log.Printf("sessions: %s ICE gathering did not complete within 3s; offer may be missing candidates", sessionID)
	}

	// Re-read the local description: it now contains the gathered candidates.
	offerJSON, err := json.Marshal(state.PC.LocalDescription())
	if err != nil {
		return "", fmt.Errorf("failed to marshal offer: %w", err)
	}

	return string(offerJSON), nil
}

// SetRemoteAnswer sets the remote answer for the session.
func (g *Gateway) SetRemoteAnswer(sessionID, answerJSON string) error {
	state, ok := g.getSession(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	var answer webrtc.SessionDescription
	if err := json.Unmarshal([]byte(answerJSON), &answer); err != nil {
		return fmt.Errorf("failed to unmarshal answer: %w", err)
	}

	if err := state.PC.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	state.mu.Lock()
	state.Status = "active"
	state.mu.Unlock()

	log.Printf("sessions: %s now active", sessionID)
	return nil
}

// AddICECandidate adds an ICE candidate to the session.
func (g *Gateway) AddICECandidate(sessionID, candidateJSON string) error {
	state, ok := g.getSession(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal([]byte(candidateJSON), &candidate); err != nil {
		return fmt.Errorf("failed to unmarshal candidate: %w", err)
	}

	if err := state.PC.AddICECandidate(candidate); err != nil {
		return fmt.Errorf("failed to add ICE candidate: %w", err)
	}

	return nil
}

// SendData sends data over the session's data channel.
func (g *Gateway) SendData(sessionID string, data []byte) error {
	state, ok := g.getSession(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	state.mu.RLock()
	ch := state.DataCh
	state.mu.RUnlock()

	if ch == nil {
		return fmt.Errorf("data channel not ready for session %s", sessionID)
	}

	if err := ch.Send(data); err != nil {
		return fmt.Errorf("failed to send data: %w", err)
	}

	return nil
}

// CloseSession closes the WebRTC peer connection for a session.
func (g *Gateway) CloseSession(sessionID string) error {
	state, ok := g.getSession(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if err := state.PC.Close(); err != nil {
		return fmt.Errorf("failed to close peer connection: %w", err)
	}

	g.mu.Lock()
	delete(g.sessions, sessionID)
	g.mu.Unlock()

	log.Printf("sessions: %s closed", sessionID)
	return nil
}

// GetSession retrieves a session state by ID.
func (g *Gateway) GetSession(sessionID string) (*SessionState, bool) {
	return g.getSession(sessionID)
}

func (g *Gateway) getSession(sessionID string) (*SessionState, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	state, ok := g.sessions[sessionID]
	return state, ok
}
