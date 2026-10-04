package session

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
)

// Framed screen protocol for the native viewer (see server/ws/viewer.go
// for the wire format). The encoder keeps the previous frame and sends
// only the changed regions, so a mostly static desktop costs a few KB
// per update instead of a full JPEG.
const (
	FrameKindFull  = 0x01
	FrameKindTiles = 0x02

	// MonitorAll is the monitor id of the whole virtual desktop.
	MonitorAll = 0xFF

	tileSize = 128
	// fullFrameRatio: when more than this share of tiles changed, one
	// full JPEG is smaller and cheaper than many tile JPEGs.
	fullFrameRatio = 0.4
)

// FrameEncoder turns raw frames into viewer frames. It is not safe for
// concurrent use.
type FrameEncoder struct {
	prev *image.RGBA
	mon  byte
}

// Reset forces the next frame to be a full keyframe (new viewer,
// dropped frames, monitor switch).
func (e *FrameEncoder) Reset() { e.prev = nil }

// Encode returns the message for img, or nil when nothing changed.
// img must not be modified by the caller afterwards (it becomes the
// reference for the next diff).
func (e *FrameEncoder) Encode(img *image.RGBA, mon byte, quality int) ([]byte, error) {
	b := img.Bounds()
	if e.prev == nil || e.mon != mon || e.prev.Bounds() != b {
		e.prev, e.mon = img, mon
		return encodeFull(img, mon, quality)
	}
	prev := e.prev
	e.prev = img

	cols := (b.Dx() + tileSize - 1) / tileSize
	rows := (b.Dy() + tileSize - 1) / tileSize
	dirty := make([]bool, cols*rows)
	nDirty := 0
	for ty := 0; ty < rows; ty++ {
		for tx := 0; tx < cols; tx++ {
			if tileChanged(img, prev, tx, ty) {
				dirty[ty*cols+tx] = true
				nDirty++
			}
		}
	}
	if nDirty == 0 {
		return nil, nil
	}
	if float64(nDirty) > fullFrameRatio*float64(cols*rows) {
		return encodeFull(img, mon, quality)
	}

	var body bytes.Buffer
	n := 0
	for ty := 0; ty < rows; ty++ {
		// Merge horizontal runs of dirty tiles into one rectangle.
		for tx := 0; tx < cols; {
			if !dirty[ty*cols+tx] {
				tx++
				continue
			}
			start := tx
			for tx < cols && dirty[ty*cols+tx] {
				tx++
			}
			r := image.Rect(start*tileSize, ty*tileSize, tx*tileSize, (ty+1)*tileSize).
				Add(b.Min).Intersect(b)
			var jb bytes.Buffer
			if err := jpeg.Encode(&jb, img.SubImage(r), &jpeg.Options{Quality: quality}); err != nil {
				return nil, fmt.Errorf("jpeg encode tile: %w", err)
			}
			var hdr [8]byte
			binary.BigEndian.PutUint16(hdr[0:], uint16(r.Min.X-b.Min.X))
			binary.BigEndian.PutUint16(hdr[2:], uint16(r.Min.Y-b.Min.Y))
			binary.BigEndian.PutUint32(hdr[4:], uint32(jb.Len()))
			body.Write(hdr[:])
			body.Write(jb.Bytes())
			n++
		}
	}
	out := make([]byte, 0, 8+body.Len())
	var hdr [8]byte
	hdr[0], hdr[1] = FrameKindTiles, mon
	binary.BigEndian.PutUint16(hdr[2:], uint16(b.Dx()))
	binary.BigEndian.PutUint16(hdr[4:], uint16(b.Dy()))
	binary.BigEndian.PutUint16(hdr[6:], uint16(n))
	out = append(out, hdr[:]...)
	return append(out, body.Bytes()...), nil
}

func encodeFull(img *image.RGBA, mon byte, quality int) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(1 << 16)
	buf.Write([]byte{FrameKindFull, mon})
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("jpeg encode: %w", err)
	}
	return buf.Bytes(), nil
}

// tileChanged compares one tile of a against b, row by row.
func tileChanged(a, b *image.RGBA, tx, ty int) bool {
	bounds := a.Bounds()
	x0 := bounds.Min.X + tx*tileSize
	y0 := bounds.Min.Y + ty*tileSize
	x1 := min(x0+tileSize, bounds.Max.X)
	y1 := min(y0+tileSize, bounds.Max.Y)
	for y := y0; y < y1; y++ {
		ao := a.PixOffset(x0, y)
		bo := b.PixOffset(x0, y)
		n := (x1 - x0) * 4
		if !bytes.Equal(a.Pix[ao:ao+n], b.Pix[bo:bo+n]) {
			return true
		}
	}
	return false
}
