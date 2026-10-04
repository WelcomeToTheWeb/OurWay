package session

import (
	"context"
	"encoding/json"
	"image"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// maxClipboardBytes bounds text clipboard sync in both directions.
const maxClipboardBytes = 1 << 20

// RawFrame is one captured screen image plus where it came from.
type RawFrame struct {
	Img     *image.RGBA // immutable once published
	Seq     uint64
	Monitor int // selected monitor id, or MonitorAll
}

// RawSource produces raw frames for the native viewer. The Windows
// capture implements it; on other platforms the remote exe has none and
// viewer_attach is ignored.
type RawSource interface {
	// SetViewerMode switches the capture producer between legacy JPEG
	// frames (off) and raw frames for NextRaw (on).
	SetViewerMode(on bool)
	Monitors() []Monitor
	// SelectMonitor picks the monitor to capture and to map mouse
	// input onto; id MonitorAll selects the whole virtual desktop.
	SelectMonitor(id int) error
	// NextRaw waits up to timeout for a frame newer than after.
	NextRaw(after uint64, timeout time.Duration) (RawFrame, bool)
}

// Clipboard is the text clipboard of the interactive session.
type Clipboard interface {
	// Seq changes whenever the clipboard content changes.
	Seq() uint32
	Get() (string, error)
	Set(text string) error
}

// ViewerLink runs the native-viewer side of a remote session inside
// the remote exe: it encodes frames, reports monitors, and keeps the
// clipboard in sync, while the viewer is attached.
type ViewerLink struct {
	Src        RawSource
	Clip       Clipboard // optional
	SendText   func(msgType string, payload any) error
	SendBinary func(data []byte) error

	attached atomic.Bool
	keyframe atomic.Bool
	quality  atomic.Int32

	mu         sync.Mutex // guards the clipboard echo state
	clipSeq    uint32
	clipText   string
	lastMonSig string
}

// NewViewerLink creates a link; call Run to start it.
func NewViewerLink(src RawSource, clip Clipboard, sendText func(string, any) error, sendBinary func([]byte) error) *ViewerLink {
	l := &ViewerLink{Src: src, Clip: clip, SendText: sendText, SendBinary: sendBinary}
	l.quality.Store(80)
	return l
}

// Attached reports whether a viewer is connected (the remote exe then
// stops sending legacy JPEG frames).
func (l *ViewerLink) Attached() bool { return l.attached.Load() }

// SetQuality sets the JPEG quality used for viewer frames.
func (l *ViewerLink) SetQuality(q int) {
	l.quality.Store(int32(min(max(q, 10), 100)))
}

// Handle processes one text message from the server.
func (l *ViewerLink) Handle(msgType string, payload json.RawMessage) {
	switch msgType {
	case "viewer_attach":
		l.Src.SetViewerMode(true)
		l.mu.Lock()
		l.lastMonSig = "" // force a monitor_list
		l.mu.Unlock()
		l.keyframe.Store(true)
		l.attached.Store(true)
	case "viewer_detach":
		l.attached.Store(false)
		l.Src.SetViewerMode(false)
	case "request_keyframe":
		l.keyframe.Store(true)
	case "monitor_select":
		var p struct {
			ID int `json:"id"`
		}
		if json.Unmarshal(payload, &p) != nil {
			return
		}
		if err := l.Src.SelectMonitor(p.ID); err != nil {
			log.Printf("viewer: select monitor %d: %v", p.ID, err)
			return
		}
		l.keyframe.Store(true)
	case "clipboard":
		l.setClipboard(payload)
	}
}

func (l *ViewerLink) setClipboard(payload json.RawMessage) {
	if l.Clip == nil {
		return
	}
	var p struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(payload, &p) != nil || len(p.Text) > maxClipboardBytes {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.Clip.Set(p.Text); err != nil {
		log.Printf("viewer: set clipboard: %v", err)
		return
	}
	// Remember what we wrote so the poller does not echo it back.
	l.clipSeq = l.Clip.Seq()
	l.clipText = p.Text
}

// Run drives frames, monitor updates and clipboard polling until ctx
// ends. Call it once, in its own goroutine.
func (l *ViewerLink) Run(ctx context.Context) {
	var enc FrameEncoder
	var seq uint64
	lastAux := time.Time{}
	for ctx.Err() == nil {
		if !l.attached.Load() {
			enc.Reset()
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		if time.Since(lastAux) > 500*time.Millisecond {
			lastAux = time.Now()
			l.syncMonitors()
			l.pollClipboard()
		}
		fr, ok := l.Src.NextRaw(seq, 250*time.Millisecond)
		if !ok {
			continue
		}
		seq = fr.Seq
		if l.keyframe.Swap(false) {
			enc.Reset()
		}
		msg, err := enc.Encode(fr.Img, byte(fr.Monitor), int(l.quality.Load()))
		if err != nil {
			log.Printf("viewer: encode: %v", err)
			continue
		}
		if msg == nil {
			continue
		}
		if err := l.SendBinary(msg); err != nil {
			log.Printf("viewer: send frame: %v", err)
			return
		}
	}
}

// syncMonitors sends monitor_list when the layout changed.
func (l *ViewerLink) syncMonitors() {
	mons := l.Src.Monitors()
	sig, _ := json.Marshal(mons)
	l.mu.Lock()
	changed := string(sig) != l.lastMonSig
	l.lastMonSig = string(sig)
	l.mu.Unlock()
	if changed {
		_ = l.SendText("monitor_list", mons)
	}
}

// pollClipboard forwards local clipboard changes to the viewer.
func (l *ViewerLink) pollClipboard() {
	if l.Clip == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	seq := l.Clip.Seq()
	if seq == l.clipSeq {
		return
	}
	l.clipSeq = seq
	text, err := l.Clip.Get()
	if err != nil || text == "" || len(text) > maxClipboardBytes || text == l.clipText {
		return
	}
	l.clipText = text
	_ = l.SendText("clipboard", map[string]string{"text": text})
}
