package ws

import (
	"encoding/json"
	"log"
	"time"

	"ourway/server/models"
	"ourway/server/store"
)

// Native viewer support.
//
// The technician's native viewer (cmd/ourway-viewer) connects to /ws
// with the subprotocol list "ourway-auth, <viewer token>, viewer". The
// token is scoped to one session, so the connection is registered as
// "viewer:<session id>" and may only talk to that session's device.
//
// Frames reach the viewer as binary WebSocket messages, not base64
// JSON. Two formats exist (the first byte tells them apart; a legacy
// raw JPEG starts with 0xFF, never 0x01 or 0x02):
//
//	0x01 full frame: [0x01][monitor u8][JPEG ...]
//	0x02 tile update: [0x02][monitor u8][width u16][height u16][n u16]
//	     then n times [x u16][y u16][len u32][JPEG of len bytes]
//	     (integers big-endian; width/height are the monitor's full size)
//
// The server only validates the kind byte and forwards the message
// verbatim. Text messages are JSON {type, payload} and are relayed
// between viewer and remote exe according to the allow-lists below.
const (
	frameKindFull  = 0x01
	frameKindTiles = 0x02

	// maxViewerMsgBytes bounds a text message from the viewer
	// (clipboard text is the largest legitimate one).
	maxViewerMsgBytes = 2 << 20
)

// viewerToRemote lists the message types a viewer may send to the
// session's remote-control exe.
var viewerToRemote = map[string]bool{
	"input":            true,
	"clipboard":        true,
	"monitor_select":   true,
	"session_quality":  true,
	"request_keyframe": true,
}

// remoteToViewer lists the message types the remote exe may send to
// the viewer.
var remoteToViewer = map[string]bool{
	"monitor_list": true,
	"clipboard":    true,
	"session_info": true,
}

// isFramedFrame reports whether a binary message from the remote exe
// uses the framed viewer protocol rather than being a raw JPEG.
func isFramedFrame(b []byte) bool {
	return len(b) > 0 && (b[0] == frameKindFull || b[0] == frameKindTiles)
}

// rawMessage is Message with an undecoded payload, so relayed
// messages are forwarded without a decode/encode round trip of the
// payload.
type rawMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// SendToViewer delivers a text message to the viewer attached to the
// session, if any. It is local to this instance, like the remote exe
// connection it pairs with.
func (h *Hub) SendToViewer(sessionID, msgType string, payload interface{}) {
	data, err := json.Marshal(Message{Type: msgType, Payload: payload})
	if err != nil {
		return
	}
	h.mu.RLock()
	v, ok := h.clients["viewer:"+sessionID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case v.SendCh <- data:
	default:
		h.dropped.Add(1)
	}
}

// hasViewer reports whether a viewer is attached to the session.
func (h *Hub) hasViewer(sessionID string) bool {
	h.mu.RLock()
	_, ok := h.clients["viewer:"+sessionID]
	h.mu.RUnlock()
	return ok
}

// sendViewerFrame queues a binary frame for the viewer. Frames are
// perishable: when the viewer is slow the oldest queued frame is
// dropped so it always sees the newest screen, never a growing lag.
func (h *Hub) sendViewerFrame(sessionID string, frame []byte) {
	h.mu.RLock()
	v, ok := h.clients["viewer:"+sessionID]
	h.mu.RUnlock()
	if !ok || v.BinCh == nil {
		return
	}
	for tries := 0; tries < 2; tries++ {
		select {
		case v.BinCh <- frame:
			return
		default:
		}
		select {
		case <-v.BinCh:
			h.dropped.Add(1)
			// A dropped frame may have been a tile update the viewer
			// now lacks; the next frames are diffs against it, so
			// ask the exe for a full keyframe (at most once a second).
			now := time.Now().UnixNano()
			if last := v.lastKeyReq.Load(); now-last > int64(time.Second) && v.lastKeyReq.CompareAndSwap(last, now) {
				if out, err := json.Marshal(Message{Type: "request_keyframe"}); err == nil {
					h.sendRemoteExe(v.DeviceKey, out)
				}
			}
		default:
		}
	}
}

// sendRemoteExe delivers a text message to the remote exe of a device
// only (not the agent service).
func (h *Hub) sendRemoteExe(deviceKey string, data []byte) bool {
	h.mu.RLock()
	r, ok := h.clients["remote:"+deviceKey]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case r.SendCh <- data:
		return true
	default:
		h.dropped.Add(1)
		return false
	}
}

// sendAgent delivers a text message to the agent service connection of
// a device only (not the remote exe).
func (h *Hub) sendAgent(deviceKey string, data []byte) bool {
	h.mu.RLock()
	d, ok := h.clients["device:"+deviceKey]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case d.SendCh <- data:
		return true
	default:
		h.dropped.Add(1)
		return false
	}
}

// endSessionForViewer ends the session when its viewer goes away and
// tells the device to stop capturing (what DELETE /api/sessions/:id
// does for the browser viewer).
func (h *Hub) endSessionForViewer(st *store.Store, sess *models.Session, deviceKey string) {
	cur, err := st.Sessions.GetByID(sess.ID)
	if err != nil || (cur.Status != "pending" && cur.Status != "active") {
		return
	}
	if err := st.Sessions.EndSession(sess.ID); err != nil {
		log.Printf("ws: failed to end session %s after viewer left: %v", sess.ID, err)
		return
	}
	if err := h.SendToDevice(deviceKey, "session_end", map[string]string{"session_id": sess.ID}); err != nil {
		log.Printf("ws: failed to notify device of session end: %v", err)
	}
}

// handleViewerMessage routes one text message from a session's viewer.
func (h *Hub) handleViewerMessage(client *Client, sess *models.Session, raw []byte) {
	var msg rawMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	switch {
	case msg.Type == "ping":
		if pong, err := json.Marshal(Message{Type: "pong"}); err == nil {
			select {
			case client.SendCh <- pong:
			default:
				h.dropped.Add(1)
			}
		}
	case viewerToRemote[msg.Type]:
		h.sendRemoteExe(client.DeviceKey, raw)
	case msg.Type == "special_key":
		// Ctrl+Alt+Del cannot be injected with SendInput; only the
		// SYSTEM agent service can raise the Secure Attention Sequence.
		var p struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(msg.Payload, &p) != nil || p.Key != "ctrl_alt_del" {
			return
		}
		out, err := json.Marshal(Message{Type: "send_sas", Payload: map[string]string{"session_id": sess.ID}})
		if err == nil && !h.sendAgent(client.DeviceKey, out) {
			log.Printf("ws: session %s: agent not connected for send_sas", sess.ID)
		}
	}
}

// handleRemoteTextMessage routes one text message from a remote exe
// to its session's viewer.
func (h *Hub) handleRemoteTextMessage(sess *models.Session, raw []byte) {
	var msg rawMessage
	if err := json.Unmarshal(raw, &msg); err != nil || !remoteToViewer[msg.Type] {
		return
	}
	h.mu.RLock()
	v, ok := h.clients["viewer:"+sess.ID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case v.SendCh <- raw:
	default:
		h.dropped.Add(1)
	}
}

// relayRichFrame forwards a framed (non-legacy) binary frame from the
// remote exe to the viewer. Malformed frames are dropped.
func (h *Hub) relayRichFrame(sess *models.Session, frame []byte) {
	if len(frame) < 2 || (frame[0] != frameKindFull && frame[0] != frameKindTiles) {
		return
	}
	h.sendViewerFrame(sess.ID, frame)
}
