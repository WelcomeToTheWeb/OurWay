package session

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"testing"
)

// apply decodes a viewer frame onto canvas (the viewer's job); it is
// the reference decoder the encoder is tested against.
func apply(t *testing.T, canvas *image.RGBA, msg []byte) *image.RGBA {
	t.Helper()
	switch msg[0] {
	case FrameKindFull:
		img, err := jpeg.Decode(bytes.NewReader(msg[2:]))
		if err != nil {
			t.Fatal(err)
		}
		c := image.NewRGBA(img.Bounds())
		draw.Draw(c, c.Bounds(), img, image.Point{}, draw.Src)
		return c
	case FrameKindTiles:
		n := int(binary.BigEndian.Uint16(msg[6:]))
		p := msg[8:]
		for i := 0; i < n; i++ {
			x := int(binary.BigEndian.Uint16(p[0:]))
			y := int(binary.BigEndian.Uint16(p[2:]))
			l := int(binary.BigEndian.Uint32(p[4:]))
			img, err := jpeg.Decode(bytes.NewReader(p[8 : 8+l]))
			if err != nil {
				t.Fatal(err)
			}
			draw.Draw(canvas, image.Rect(x, y, x+img.Bounds().Dx(), y+img.Bounds().Dy()), img, image.Point{}, draw.Src)
			p = p[8+l:]
		}
		return canvas
	}
	t.Fatalf("unknown kind %d", msg[0])
	return nil
}

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	return img
}

func near(a, b color.RGBA) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) < 12 && d(a.G, b.G) < 12 && d(a.B, b.B) < 12
}

func TestFrameEncoderTilesRoundTrip(t *testing.T) {
	white := color.RGBA{255, 255, 255, 255}
	red := color.RGBA{255, 0, 0, 255}
	var enc FrameEncoder

	f1 := solid(1000, 700, white)
	m1, err := enc.Encode(f1, 0, 95)
	if err != nil || m1[0] != FrameKindFull {
		t.Fatalf("first frame must be full: %v %v", m1[:1], err)
	}
	canvas := apply(t, nil, m1)

	// Unchanged frame -> nothing to send.
	f2 := solid(1000, 700, white)
	if m, _ := enc.Encode(f2, 0, 95); m != nil {
		t.Fatal("unchanged frame produced output")
	}

	// A small change -> a tile update that is much smaller than full.
	f3 := solid(1000, 700, white)
	draw.Draw(f3, image.Rect(300, 300, 340, 330), &image.Uniform{red}, image.Point{}, draw.Src)
	m3, err := enc.Encode(f3, 0, 95)
	if err != nil || m3[0] != FrameKindTiles {
		t.Fatalf("small change must be a tile update, got %v %v", m3[:1], err)
	}
	if len(m3) >= len(m1) {
		t.Errorf("tile update (%d B) not smaller than full frame (%d B)", len(m3), len(m1))
	}
	canvas = apply(t, canvas, m3)
	if !near(canvas.RGBAAt(310, 310), red) || !near(canvas.RGBAAt(10, 10), white) {
		t.Error("tile update did not reproduce the changed region")
	}

	// A big change -> full frame again.
	f4 := solid(1000, 700, red)
	if m4, _ := enc.Encode(f4, 0, 95); m4[0] != FrameKindFull {
		t.Error("large change must fall back to a full frame")
	}

	// Reset and monitor switches force keyframes.
	enc.Reset()
	if m, _ := enc.Encode(f4, 0, 95); m[0] != FrameKindFull {
		t.Error("frame after Reset must be full")
	}
	if m, _ := enc.Encode(solid(1000, 700, red), 1, 95); m[0] != FrameKindFull || m[1] != 1 {
		t.Error("monitor switch must send a full frame tagged with the new monitor")
	}
}
