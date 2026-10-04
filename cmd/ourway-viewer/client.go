package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"nhooyr.io/websocket"
)

// Monitor mirrors agent/session.Monitor.
type Monitor struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Primary bool   `json:"primary"`
}

// Callbacks are invoked from the client's read goroutine.
type Callbacks struct {
	Frame     func()          // the screen image changed
	Monitors  func([]Monitor) // the remote monitor layout (re)arrived
	Clipboard func(text string)
	Ended     func(reason string)
}

// Client is the viewer's WebSocket connection to the server, scoped to
// one session by the viewer token.
type Client struct {
	conn   *websocket.Conn
	screen *Screen
	cb     Callbacks
	ctx    context.Context
	cancel context.CancelFunc
}

// Dial connects to the server for the launch's session.
func Dial(ctx context.Context, l Launch, screen *Screen, cb Callbacks) (*Client, error) {
	cctx, cancel := context.WithCancel(ctx)
	dctx, dcancel := context.WithTimeout(cctx, 15*time.Second)
	defer dcancel()
	conn, _, err := websocket.Dial(dctx, l.Server+"/ws", &websocket.DialOptions{
		Subprotocols: []string{"ourway-auth", l.Token, "viewer"},
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect to %s: %w", l.Server, err)
	}
	conn.SetReadLimit(32 << 20) // a full 4K JPEG frame can be several MB
	return &Client{conn: conn, screen: screen, cb: cb, ctx: cctx, cancel: cancel}, nil
}

// Close ends the connection.
func (c *Client) Close() {
	c.cancel()
	c.conn.Close(websocket.StatusNormalClosure, "viewer closed")
}

// Run reads until the connection ends, then reports it via Ended. It
// also sends the app-level ping the server's read deadline needs.
func (c *Client) Run() {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-t.C:
				c.send("ping", nil)
			}
		}
	}()
	reason := "connection closed"
	defer func() {
		if c.cb.Ended != nil {
			c.cb.Ended(reason)
		}
	}()
	for {
		typ, data, err := c.conn.Read(c.ctx)
		if err != nil {
			if c.ctx.Err() != nil {
				reason = "closed"
			} else if s := websocket.CloseStatus(err); s == websocket.StatusPolicyViolation {
				reason = "session ended"
			} else {
				reason = "connection lost: " + err.Error()
			}
			return
		}
		if typ == websocket.MessageBinary {
			switch err := c.screen.Apply(data); err {
			case nil:
				if c.cb.Frame != nil {
					c.cb.Frame()
				}
			case errNeedKeyframe:
				c.send("request_keyframe", nil)
			default:
				log.Printf("viewer: bad frame: %v", err)
				c.send("request_keyframe", nil)
			}
			continue
		}
		var msg struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "monitor_list":
			var mons []Monitor
			if json.Unmarshal(msg.Payload, &mons) == nil && c.cb.Monitors != nil {
				c.cb.Monitors(mons)
			}
		case "clipboard":
			var p struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(msg.Payload, &p) == nil && c.cb.Clipboard != nil {
				c.cb.Clipboard(p.Text)
			}
		case "session_end":
			reason = "session ended"
			return
		}
	}
}

func (c *Client) send(typ string, payload any) {
	data, err := json.Marshal(struct {
		Type    string `json:"type"`
		Payload any    `json:"payload,omitempty"`
	}{typ, payload})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil && c.ctx.Err() == nil {
		log.Printf("viewer: send %s: %v", typ, err)
	}
}

// sendInput wraps a key/mouse payload in the envelope the remote exe
// expects: {type:"input", payload:{type:"mouse"|"key", payload:{...}}}.
func (c *Client) sendInput(kind string, payload map[string]any) {
	c.send("input", map[string]any{"type": kind, "payload": payload})
}

// Mouse sends a pointer event; x and y are percentages (0-100) of the
// displayed remote monitor. event: move, down, up, click.
func (c *Client) Mouse(event string, x, y float64, button string) {
	c.sendInput("mouse", map[string]any{"event": event, "x": x, "y": y, "button": button})
}

// Wheel sends a scroll in browser convention (positive = down).
func (c *Client) Wheel(deltaY float64) {
	c.sendInput("mouse", map[string]any{"event": "wheel", "delta": deltaY})
}

// Key sends a key press (down=true) or release.
func (c *Client) Key(key string, down bool) {
	ev := "up"
	if down {
		ev = "down"
	}
	c.sendInput("key", map[string]any{"key": key, "event": ev})
}

// CtrlAltDel asks the agent service to raise the Secure Attention Sequence.
func (c *Client) CtrlAltDel() { c.send("special_key", map[string]string{"key": "ctrl_alt_del"}) }

// SelectMonitor switches the captured monitor (255 = all).
func (c *Client) SelectMonitor(id int) { c.send("monitor_select", map[string]int{"id": id}) }

// SetQuality sets the JPEG quality (10-100).
func (c *Client) SetQuality(q int) { c.send("session_quality", map[string]int{"quality": q}) }

// SendClipboard pushes text to the remote clipboard.
func (c *Client) SendClipboard(text string) { c.send("clipboard", map[string]string{"text": text}) }
