package session

import (
	"context"
	"encoding/json"
	"image"
	"sync"
	"testing"
	"time"
)

type fakeSrc struct {
	mu       sync.Mutex
	viewer   bool
	selected int
	seq      uint64
}

func (f *fakeSrc) SetViewerMode(on bool) { f.mu.Lock(); f.viewer = on; f.mu.Unlock() }
func (f *fakeSrc) Monitors() []Monitor {
	return []Monitor{{ID: 0, W: 200, H: 100, Primary: true}, {ID: 1, X: 200, W: 200, H: 100}}
}
func (f *fakeSrc) SelectMonitor(id int) error {
	f.mu.Lock()
	f.selected = id
	f.mu.Unlock()
	return nil
}
func (f *fakeSrc) NextRaw(after uint64, d time.Duration) (RawFrame, bool) {
	time.Sleep(10 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	return RawFrame{Img: image.NewRGBA(image.Rect(0, 0, 200, 100)), Seq: f.seq, Monitor: f.selected}, true
}

type fakeClip struct {
	mu   sync.Mutex
	seq  uint32
	text string
}

func (c *fakeClip) Seq() uint32 { c.mu.Lock(); defer c.mu.Unlock(); return c.seq }
func (c *fakeClip) Get() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text, nil
}
func (c *fakeClip) Set(s string) error { c.mu.Lock(); c.text = s; c.seq++; c.mu.Unlock(); return nil }
func (c *fakeClip) user(s string)      { c.Set(s) }

type sink struct {
	mu   sync.Mutex
	text []string
	bin  [][]byte
}

func (s *sink) sendText(typ string, p any) error {
	b, _ := json.Marshal(p)
	s.mu.Lock()
	s.text = append(s.text, typ+" "+string(b))
	s.mu.Unlock()
	return nil
}
func (s *sink) sendBin(b []byte) error {
	s.mu.Lock()
	s.bin = append(s.bin, b)
	s.mu.Unlock()
	return nil
}
func (s *sink) has(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.text {
		if len(m) >= len(prefix) && m[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestViewerLink(t *testing.T) {
	src, clip, out := &fakeSrc{}, &fakeClip{}, &sink{}
	l := NewViewerLink(src, clip, out.sendText, out.sendBin)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)

	// Nothing is sent before a viewer attaches.
	time.Sleep(50 * time.Millisecond)
	if len(out.bin) != 0 || l.Attached() {
		t.Fatal("link active before viewer_attach")
	}

	l.Handle("viewer_attach", nil)
	waitFor(t, "first full frame", func() bool {
		out.mu.Lock()
		defer out.mu.Unlock()
		return len(out.bin) > 0 && out.bin[0][0] == FrameKindFull
	})
	waitFor(t, "monitor_list", func() bool { return out.has("monitor_list") })
	if !src.viewer {
		t.Error("viewer mode not enabled on the source")
	}

	// Monitor switch: source told, next frame is a full frame for it.
	out.mu.Lock()
	n := len(out.bin)
	out.mu.Unlock()
	l.Handle("monitor_select", json.RawMessage(`{"id":1}`))
	waitFor(t, "frame for monitor 1", func() bool {
		out.mu.Lock()
		defer out.mu.Unlock()
		for _, f := range out.bin[n:] {
			if f[0] == FrameKindFull && f[1] == 1 {
				return true
			}
		}
		return false
	})

	// Viewer -> device clipboard is applied and not echoed back; a
	// local change is reported once.
	l.Handle("clipboard", json.RawMessage(`{"text":"from viewer"}`))
	if clip.text != "from viewer" {
		t.Errorf("clipboard not set: %q", clip.text)
	}
	clip.user("from user")
	waitFor(t, "clipboard to viewer", func() bool { return out.has(`clipboard {"text":"from user"}`) })
	if out.has(`clipboard {"text":"from viewer"}`) {
		t.Error("viewer's own clipboard was echoed back")
	}

	l.Handle("viewer_detach", nil)
	if l.Attached() || src.viewer {
		t.Error("detach did not stop viewer mode")
	}
}
