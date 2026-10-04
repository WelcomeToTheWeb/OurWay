package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"testing"
)

func jpegOf(w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 95})
	return b.Bytes()
}

func TestScreenApply(t *testing.T) {
	var s Screen
	white, red := color.RGBA{255, 255, 255, 255}, color.RGBA{255, 0, 0, 255}

	// A tile update before any base frame asks for a keyframe.
	hdr := []byte{frameTiles, 0, 0, 0, 0, 0, 0, 0}
	if err := s.Apply(hdr); err != errNeedKeyframe {
		t.Fatalf("want errNeedKeyframe, got %v", err)
	}

	full := append([]byte{frameFull, 0}, jpegOf(256, 256, white)...)
	if err := s.Apply(full); err != nil {
		t.Fatal(err)
	}
	if w, h := s.Size(); w != 256 || h != 256 {
		t.Fatalf("size %dx%d", w, h)
	}

	tile := jpegOf(64, 64, red)
	msg := []byte{frameTiles, 0}
	msg = binary.BigEndian.AppendUint16(msg, 256)
	msg = binary.BigEndian.AppendUint16(msg, 256)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	msg = binary.BigEndian.AppendUint16(msg, 128)
	msg = binary.BigEndian.AppendUint16(msg, 64)
	msg = binary.BigEndian.AppendUint32(msg, uint32(len(tile)))
	msg = append(msg, tile...)
	if err := s.Apply(msg); err != nil {
		t.Fatal(err)
	}
	img, v := s.Snapshot(0)
	if img == nil || v == 0 {
		t.Fatal("no snapshot")
	}
	if c := img.RGBAAt(150, 90); c.R < 240 || c.G > 20 {
		t.Errorf("tile not applied, pixel %v", c)
	}
	if c := img.RGBAAt(10, 10); c.G < 240 {
		t.Errorf("untouched area changed, pixel %v", c)
	}
	if again, _ := s.Snapshot(v); again != nil {
		t.Error("snapshot of unchanged screen should be nil")
	}

	// Malformed input is rejected, never panics.
	bad := append([]byte(nil), msg...)
	bad[8], bad[9] = 0xFF, 0xF0 // x far outside the screen
	if err := s.Apply(bad); err == nil {
		t.Error("tile outside screen accepted")
	}
	if err := s.Apply(msg[:len(msg)-5]); err == nil {
		t.Error("truncated tile accepted")
	}
	if err := s.Apply([]byte{0x7F, 0}); err == nil {
		t.Error("unknown kind accepted")
	}
	// A different monitor needs its own keyframe.
	m1 := append([]byte(nil), msg...)
	m1[1] = 1
	if err := s.Apply(m1); err != errNeedKeyframe {
		t.Errorf("monitor change should need keyframe, got %v", err)
	}
}
