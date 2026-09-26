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
	ID         string
	DeviceID   string
	UserID     string
	PC         *webrtc.PeerConnection
	Status     string
	DataCh     *webrtc.DataChannel
	CreatedAt  time.Time
	mu         sync.RWMutex
}

// Gateway manages WebRTC peer connections for remote sessions.
type Gateway struct {
	sessions map[string]*SessionState
	mu       sync.RWMutex
}

// NewGateway creates a new session gateway.
func NewGateway() *Gateway {
	return &Gateway{
		sessions: make(map[string]*SessionState),
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
			// Handle incoming data (screen frames from agent, input acks, etc.)
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

	offer, err := state.PC.CreateOffer(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create offer: %w", err)
	}

	if err := state.PC.SetLocalDescription(offer); err != nil {
		return "", fmt.Errorf("failed to set local description: %w", err)
	}

	// Wait for ICE gathering to complete
	state.PC.OnICEGatheringStateChange(func(state webrtc.ICEGathererState) {
		if state == webrtc.ICEGathererStateComplete {
			log.Printf("sessions: %s ICE gathering complete", sessionID)
		}
	})

	offerJSON, err := json.Marshal(offer)
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
