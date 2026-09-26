package session

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/pion/webrtc/v3"
)

// WebRTCManager handles the WebRTC peer connection for remote sessions.
type WebRTCManager struct {
	config      webrtc.Configuration
	peerConn    *webrtc.PeerConnection
	dataChannel *webrtc.DataChannel
	capture     ScreenCapture
}

// NewWebRTCManager creates a new WebRTC manager.
func NewWebRTCManager(capture ScreenCapture) *WebRTCManager {
	return &WebRTCManager{
		config: webrtc.Configuration{
			ICEServers: []webrtc.ICEServer{
				{
					URLs: []string{"stun:stun.l.google.com:19302"},
				},
			},
		},
		capture: capture,
	}
}

// CreatePeerConnection creates a new WebRTC peer connection.
func (wm *WebRTCManager) CreatePeerConnection() error {
	pc, err := webrtc.NewPeerConnection(wm.config)
	if err != nil {
		return fmt.Errorf("failed to create peer connection: %w", err)
	}
	wm.peerConn = pc

	// Set up data channel for screen frames
	ch, err := pc.CreateDataChannel("ourway-session", nil)
	if err != nil {
		return fmt.Errorf("failed to create data channel: %w", err)
	}
	wm.dataChannel = ch

	ch.OnOpen(func() {
		log.Printf("webrtc: data channel open")
	})

	ch.OnMessage(func(msg webrtc.DataChannelMessage) {
		// Handle incoming messages from browser (input events)
		if msg.IsString {
			log.Printf("webrtc: received message: %s", msg.Data)
		}
	})

	// Handle connection state changes
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("webrtc: connection state: %s", state.String())
	})

	// Handle ICE candidates
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		candidateJSON, err := json.Marshal(candidate.ToJSON())
		if err != nil {
			log.Printf("webrtc: failed to marshal ICE candidate: %v", err)
			return
		}
		// In a full implementation, this would send the ICE candidate to the server
		_ = candidateJSON
	})

	return nil
}

// SetRemoteDescription sets the remote SDP description.
func (wm *WebRTCManager) SetRemoteDescription(sdp string) error {
	if wm.peerConn == nil {
		return fmt.Errorf("peer connection not created")
	}

	var desc webrtc.SessionDescription
	if err := json.Unmarshal([]byte(sdp), &desc); err != nil {
		return fmt.Errorf("failed to unmarshal SDP: %w", err)
	}

	if err := wm.peerConn.SetRemoteDescription(desc); err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	return nil
}

// CreateAnswer creates a WebRTC answer.
func (wm *WebRTCManager) CreateAnswer() (string, error) {
	if wm.peerConn == nil {
		return "", fmt.Errorf("peer connection not created")
	}

	answer, err := wm.peerConn.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("failed to create answer: %w", err)
	}

	if err := wm.peerConn.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("failed to set local description: %w", err)
	}

	answerJSON, err := json.Marshal(answer)
	if err != nil {
		return "", fmt.Errorf("failed to marshal answer: %w", err)
	}

	return string(answerJSON), nil
}

// SendFrame sends a screen frame over the data channel.
func (wm *WebRTCManager) SendFrame(frame []byte) error {
	if wm.dataChannel == nil || wm.dataChannel.ReadyState() != webrtc.DataChannelStateOpen {
		return fmt.Errorf("data channel not ready")
	}

	if err := wm.dataChannel.Send(frame); err != nil {
		return fmt.Errorf("failed to send frame: %w", err)
	}

	return nil
}

// StartFrameLoop starts sending screen frames in a loop.
func (wm *WebRTCManager) StartFrameLoop(fps int, done chan struct{}) {
	interval := time.Second / time.Duration(fps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if wm.dataChannel == nil || wm.dataChannel.ReadyState() != webrtc.DataChannelStateOpen {
				continue
			}

			frame, err := wm.capture.Capture()
			if err != nil {
				log.Printf("webrtc: capture error: %v", err)
				continue
			}

			if err := wm.SendFrame(frame); err != nil {
				log.Printf("webrtc: send frame error: %v", err)
			}
		case <-done:
			return
		}
	}
}

// Close closes the peer connection.
func (wm *WebRTCManager) Close() error {
	if wm.peerConn != nil {
		return wm.peerConn.Close()
	}
	return nil
}

// IsConnected returns whether the peer connection is established.
func (wm *WebRTCManager) IsConnected() bool {
	if wm.peerConn == nil {
		return false
	}
	return wm.peerConn.ConnectionState() == webrtc.PeerConnectionStateConnected
}
