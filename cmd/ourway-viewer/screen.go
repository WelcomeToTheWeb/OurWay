package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"sync"
)

// Frame kinds; keep in sync with server/ws/viewer.go and
// agent/session/framing.go.
const (
	frameFull  = 0x01
	frameTiles = 0x02
)

// errNeedKeyframe means a tile update arrived without a matching base
// image; the caller should request a full frame.
var errNeedKeyframe = errors.New("tile update without base frame")

// Screen is the remote desktop image the frames are composited onto.
type Screen struct {
	mu      sync.Mutex
	img     *image.RGBA
	monitor int
	version uint64
}

// Apply composites one frame message onto the screen.
func (s *Screen) Apply(msg []byte) error {
	if len(msg) < 2 {
		return errors.New("short frame")
	}
	switch msg[0] {
	case frameFull:
		img, err := jpeg.Decode(bytes.NewReader(msg[2:]))
		if err != nil {
			return fmt.Errorf("decode full frame: %w", err)
		}
		rgba := image.NewRGBA(img.Bounds())
		draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
		s.mu.Lock()
		s.img, s.monitor = rgba, int(msg[1])
		s.version++
		s.mu.Unlock()
		return nil
	case frameTiles:
		return s.applyTiles(msg)
	}
	return fmt.Errorf("unknown frame kind %#x", msg[0])
}

func (s *Screen) applyTiles(msg []byte) error {
	if len(msg) < 8 {
		return errors.New("short tile header")
	}
	w := int(binary.BigEndian.Uint16(msg[2:]))
	h := int(binary.BigEndian.Uint16(msg[4:]))
	n := int(binary.BigEndian.Uint16(msg[6:]))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.img == nil || s.img.Bounds().Dx() != w || s.img.Bounds().Dy() != h || s.monitor != int(msg[1]) {
		return errNeedKeyframe
	}
	p := msg[8:]
	for i := 0; i < n; i++ {
		if len(p) < 8 {
			return errors.New("truncated tile header")
		}
		x := int(binary.BigEndian.Uint16(p[0:]))
		y := int(binary.BigEndian.Uint16(p[2:]))
		l := int(binary.BigEndian.Uint32(p[4:]))
		if l < 0 || l > len(p)-8 {
			return errors.New("tile length out of range")
		}
		tile, err := jpeg.Decode(bytes.NewReader(p[8 : 8+l]))
		if err != nil {
			return fmt.Errorf("decode tile: %w", err)
		}
		dst := image.Rect(x, y, x+tile.Bounds().Dx(), y+tile.Bounds().Dy())
		if !dst.In(s.img.Bounds()) {
			return errors.New("tile outside screen")
		}
		draw.Draw(s.img, dst, tile, tile.Bounds().Min, draw.Src)
		p = p[8+l:]
	}
	s.version++
	return nil
}

// Snapshot returns a copy of the current image and its version, or nil
// when nothing changed since after (so the UI re-uploads only on
// change). The copy is safe to hand to the GPU while frames keep
// arriving.
func (s *Screen) Snapshot(after uint64) (*image.RGBA, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.img == nil || s.version == after {
		return nil, s.version
	}
	cp := image.NewRGBA(s.img.Bounds())
	copy(cp.Pix, s.img.Pix)
	return cp, s.version
}

// Size returns the remote screen size (0,0 before the first frame).
func (s *Screen) Size() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.img == nil {
		return 0, 0
	}
	return s.img.Bounds().Dx(), s.img.Bounds().Dy()
}
