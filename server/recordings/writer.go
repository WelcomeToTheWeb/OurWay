// Package recordings stores the frame sequences of recorded remote-control
// sessions (2.4-8): the append-only file behind a `session_recordings` row, the
// writer that appends to it from the frame path, and the reader the playback
// endpoints read it with.
//
// The format is a JPEG sequence with a length-prefixed record per entry, chosen
// over a media container for three reasons that are all about failure modes:
//
//   - It is **append-only**. A server that dies mid-session leaves every frame it
//     wrote, and a writer that comes back appends to the same file. AVI and MP4
//     need a trailer, and a file without one is unplayable — so the format that
//     survives a crash is the one with no structure that has to be closed.
//   - Every record carries its own timestamp, so playback shows a gap as a gap.
//     Continuity is not implied by frame order.
//   - A frame is a JPEG the agent already produced. Re-encoding frames to store
//     them would put an encoder, with its own failure modes, between a person's
//     screen and the record of it.
package recordings

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ourway/server/models"
)

const (
	// Magic is the file header. A file that does not start with it is not a
	// recording, and playback says so rather than rendering garbage.
	Magic = "OWRS1\n"

	// RecordFrame is a JPEG frame; RecordMarker is a JSON note from the writer
	// (a resume after a restart, a cap, a dropped run).
	RecordFrame  byte = 1
	RecordMarker byte = 2

	// headerLen is the magic; recordHeaderLen is kind + timestamp + length.
	headerLen       = len(Magic)
	recordHeaderLen = 13

	// MaxFrameBytes is the payload cap, and it is the same 10 MiB the frame
	// endpoint already reads. Two numbers for one limit is how a limit is bypassed.
	MaxFrameBytes = 10 << 20

	// DefaultMaxRecordingBytes caps one recording. A control session posts a
	// frame a second, so a session that is forgotten open must not be able to
	// fill the disk. When the cap is hit the writer stops appending and counts
	// what it dropped, and the row states both.
	DefaultMaxRecordingBytes int64 = 2 << 30
)

// ErrTruncated is the reader's answer for a file that ends mid-record. It is
// separate from io.EOF because "the recording ended" and "the recording file was
// cut" are different facts, and a viewer must be told which one they are looking
// at.
var ErrTruncated = errors.New("recordings: the file ends mid-record")

// Frame is one stored frame, with the time the server received it.
type Frame struct {
	Index int       `json:"index"`
	At    time.Time `json:"at"`
	JPEG  []byte    `json:"-"`
}

// Marker is one writer note. The `resume` marker is the restart case: the file
// continues, and the moment writing stopped is stated, not smoothed over.
type Marker struct {
	Kind string    `json:"kind"`
	At   time.Time `json:"at"`
	Text string    `json:"detail,omitempty"`
}

// Policy is the installation's recording settings, passed in from config. This
// package reads no environment variables: the modes are validated once, at boot,
// where the server can say what it is going to do.
type Policy struct {
	// Mode is `off`, `on` or `control-only` (models.RecordingMode*).
	Mode string
	// Dir is RECORDINGS_DIR.
	Dir string
	// Retention is RECORDING_RETENTION_DAYS as a duration.
	Retention time.Duration
	// MaxBytes is the per-recording cap; 0 uses DefaultMaxRecordingBytes.
	MaxBytes int64
}

// Normalize fills in what a policy omits and creates the directory.
func (p *Policy) Normalize() {
	if p.Dir == "" {
		p.Dir = "./dist/recordings"
	}
	if p.MaxBytes <= 0 {
		p.MaxBytes = DefaultMaxRecordingBytes
	}
	if p.Mode == "" {
		p.Mode = models.RecordingModeOff
	}
	if p.Retention <= 0 {
		p.Retention = 30 * 24 * time.Hour
	}
	_ = os.MkdirAll(p.Dir, 0700)
}

// PathFor is the file a recording id names. The path is derived from the row id
// and nothing else — never a session id, a device name, or anything a request
// supplied, because this is a path the server writes.
func (p *Policy) PathFor(id string) string {
	return filepath.Join(p.Dir, id+".seq")
}

// PathOfRow resolves the file a recording row names, deriving it from the policy
// when the row carries no path. The file's location is a function of the
// recording's id, so an installation that moves `RECORDINGS_DIR` can still read
// the recordings its manifest lists, and a row written by an older build is not
// the one that fails.
func (p Policy) PathOfRow(rec *models.SessionRecording) string {
	if rec == nil {
		return ""
	}
	if rec.Path != "" {
		return rec.Path
	}
	return p.PathFor(rec.ID)
}

// RowExists reports whether a recording's file is on disk, resolving its path
// through the policy.
func (p Policy) RowExists(rec *models.SessionRecording) bool {
	return Exists(p.PathOfRow(rec))
}

// Stats are the counters of a recording file.
type Stats struct {
	Frames        int64
	Bytes         int64
	Checksum      string
	Interruptions int
	Dropped       int64
}

// openRecording is a file the writer is appending to.
type openRecording struct {
	path          string
	file          *os.File
	frames        int64
	bytes         int64
	dropped       int64
	interruptions int
	digest        hash.Hash
	capMarked     bool
}

func (e *openRecording) stats() Stats {
	return Stats{
		Frames:        e.frames,
		Bytes:         e.bytes,
		Checksum:      hex.EncodeToString(e.digest.Sum(nil)),
		Interruptions: e.interruptions,
		Dropped:       e.dropped,
	}
}

// Writer appends frames to recording files. It is the only writer, and one map
// keyed by recording id is what makes "the file a row names" the same file in
// every code path.
type Writer struct {
	policy Policy

	mu   sync.Mutex
	open map[string]*openRecording
}

// NewWriter creates the writer for a policy.
func NewWriter(policy Policy) *Writer {
	policy.Normalize()
	return &Writer{policy: policy, open: map[string]*openRecording{}}
}

// Policy reports the settings the writer runs with.
func (w *Writer) Policy() Policy { return w.policy }

// RecordingIsLive reports whether this process is appending to this recording
// right now. The retention sweep asks it before closing a row, so a live
// recording on a running server is never written off as abandoned.
func (w *Writer) RecordingIsLive(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.open[id]
	return ok
}

// Start begins — or resumes — writing the file a recording row names. Resuming
// is the restart case: the file is opened for append, the interruption is
// counted, and a `resume` marker states that the writing stopped, so playback
// shows the break instead of hiding it.
func (w *Writer) Start(rec *models.SessionRecording) error {
	if rec == nil || rec.ID == "" {
		return errors.New("recordings: start needs a recording row")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.open[rec.ID]; ok {
		return nil
	}
	path := w.policy.PathFor(rec.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("recordings: create %s: %w", filepath.Dir(path), err)
	}

	// An existing file is a recording this process did not open: a restart, and
	// the session is still live. Append to it.
	info, statErr := os.Stat(path)
	resumed := statErr == nil && info.Size() > 0

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("recordings: open %s: %w", path, err)
	}
	entry := &openRecording{path: path, file: file, digest: sha256.New()}
	if resumed {
		// Continue the file's own hash. Re-reading is the cost of stating the
		// checksum of a recording a restart interrupted, and a recording is
		// evidence: the number is worth reading the file for.
		existing, existingFrames, err := replayHash(path, entry)
		if err != nil {
			_ = file.Close()
			return err
		}
		entry.interruptions++
		entry.bytes = existing
		entry.frames = int64(existingFrames)
		payload := markerPayload(Marker{
			Kind: "resume",
			At:   time.Now().UTC(),
			Text: "writing to this recording resumed after the server stopped writing it",
		})
		if err := writeRecord(file, RecordMarker, time.Now(), payload); err != nil {
			_ = file.Close()
			return fmt.Errorf("recordings: write resume marker: %w", err)
		}
		entry.bytes += int64(recordHeaderLen + len(payload))
		entry.digest.Write(payload)
	} else {
		if _, err := file.WriteString(Magic); err != nil {
			_ = file.Close()
			return fmt.Errorf("recordings: write header: %w", err)
		}
		entry.bytes += int64(headerLen)
		entry.digest.Write([]byte(Magic))
	}
	w.open[rec.ID] = entry
	return nil
}

// Append writes one frame. A frame over the cap, a recording at its size limit,
// or a failed write is **counted as dropped** on the row: a recording that
// quietly lost frames is a recording an operator would read as complete.
func (w *Writer) Append(id string, jpeg []byte, at time.Time) error {
	if len(jpeg) == 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	entry, ok := w.open[id]
	if !ok {
		return fmt.Errorf("recordings: recording %s is not open", id)
	}
	if len(jpeg) > MaxFrameBytes {
		entry.dropped++
		return fmt.Errorf("recordings: frame of %d bytes exceeds the %d byte cap", len(jpeg), MaxFrameBytes)
	}
	if entry.bytes+int64(len(jpeg))+recordHeaderLen > w.policy.MaxBytes {
		entry.dropped++
		if !entry.capMarked {
			entry.capMarked = true
			payload := markerPayload(Marker{
				Kind: "cap",
				At:   at,
				Text: fmt.Sprintf("this recording reached its %d byte limit; later frames are not stored", w.policy.MaxBytes),
			})
			_ = writeRecord(entry.file, RecordMarker, at, payload)
		}
		return fmt.Errorf("recordings: recording reached its %d byte limit", w.policy.MaxBytes)
	}
	if err := writeRecord(entry.file, RecordFrame, at, jpeg); err != nil {
		entry.dropped++
		return fmt.Errorf("recordings: append frame: %w", err)
	}
	entry.frames++
	entry.bytes += int64(recordHeaderLen + len(jpeg))
	entry.digest.Write(jpeg)
	return nil
}

// Mark writes a writer note into an open recording.
func (w *Writer) Mark(id string, marker Marker) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	entry, ok := w.open[id]
	if !ok {
		return fmt.Errorf("recordings: recording %s is not open", id)
	}
	payload := markerPayload(marker)
	if err := writeRecord(entry.file, RecordMarker, marker.At, payload); err != nil {
		return fmt.Errorf("recordings: write marker: %w", err)
	}
	entry.bytes += int64(recordHeaderLen + len(payload))
	entry.digest.Write(payload)
	return nil
}

// Stats returns the live counters of an open recording.
func (w *Writer) Stats(id string) (Stats, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	entry, ok := w.open[id]
	if !ok {
		return Stats{}, false
	}
	return entry.stats(), true
}

// Finish closes a recording and returns its final counters. Closing a recording
// that is not open is not an error: the sweep closes abandoned rows, and the
// file is left exactly as it was written.
func (w *Writer) Finish(id string) (Stats, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	entry, ok := w.open[id]
	if !ok {
		return Stats{}, false, nil
	}
	err := entry.file.Close()
	delete(w.open, id)
	return entry.stats(), true, err
}

// Close closes every open recording. The server calls it at shutdown, so a
// recording ends with its stated size and not with the size the last flush saw.
func (w *Writer) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, entry := range w.open {
		_ = entry.file.Close()
		delete(w.open, id)
	}
}

// Remove deletes a recording's file. The row goes first: a file unlinked with
// its row present is a recording the console offers and nobody can read.
func (w *Writer) Remove(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// PathOf is the file a recording id names under this policy.
func (w *Writer) PathOf(id string) string { return w.policy.PathFor(id) }

// ---------------------------------------------------------------------------
// Reading

// Sequence is a parsed recording file.
type Sequence struct {
	Frames        []Frame
	Markers       []Marker
	Bytes         int64
	FrameCount    int
	Interruptions int
	Checksum      string
}

// Read parses a whole recording file. Playback reads the index once and then
// fetches frames one at a time, so a three-minute session costs one index read
// and one request per frame the operator looks at.
func Read(path string) (*Sequence, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, fmt.Errorf("recordings: %s has no header: %w", filepath.Base(path), err)
	}
	if string(header) != Magic {
		return nil, fmt.Errorf("recordings: %s is not a recording file (header %q)", filepath.Base(path), header)
	}

	seq := &Sequence{Bytes: int64(headerLen)}
	digest := sha256.New()
	digest.Write(header)
	for {
		kind, at, payload, err := readRecord(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A truncated tail is the shape of a crash mid-write. Everything
			// before it is readable, and what is there is stated.
			seq.Markers = append(seq.Markers, Marker{
				Kind: "truncated",
				At:   time.Now().UTC(),
				Text: fmt.Sprintf("the file ends mid-record: %v", err),
			})
			break
		}
		seq.Bytes += int64(recordHeaderLen + len(payload))
		digest.Write(payload)
		switch kind {
		case RecordFrame:
			seq.Frames = append(seq.Frames, Frame{Index: len(seq.Frames), At: at, JPEG: payload})
			seq.FrameCount++
		case RecordMarker:
			var m Marker
			if err := json.Unmarshal(payload, &m); err != nil {
				continue
			}
			seq.Markers = append(seq.Markers, m)
			if m.Kind == "resume" {
				seq.Interruptions++
			}
		}
	}
	seq.Checksum = hex.EncodeToString(digest.Sum(nil))
	return seq, nil
}

// FrameAt reads one frame by index. It walks the records, which is the cost of a
// format with no index: a recording is read for playback, not queried.
func FrameAt(path string, index int) (Frame, error) {
	f, err := os.Open(path)
	if err != nil {
		return Frame{}, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(reader, header); err != nil || string(header) != Magic {
		return Frame{}, fmt.Errorf("recordings: %s is not a recording file", filepath.Base(path))
	}
	seen := 0
	for {
		kind, at, payload, err := readRecord(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if errors.Is(err, ErrTruncated) {
			return Frame{}, fmt.Errorf("recordings: the recording ends mid-record before frame %d", index)
		}
		if err != nil {
			return Frame{}, err
		}
		if kind != RecordFrame {
			continue
		}
		if seen == index {
			return Frame{Index: seen, At: at, JPEG: payload}, nil
		}
		seen++
	}
	return Frame{}, fmt.Errorf("recordings: frame %d is not in this recording (%d frames)", index, seen)
}

// Exists reports whether a recording's file is on disk. A row whose file is gone
// is reported as missing, not as playable.
func Exists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// ---------------------------------------------------------------------------
// Encoding

func writeRecord(f *os.File, kind byte, at time.Time, payload []byte) error {
	head := make([]byte, recordHeaderLen)
	head[0] = kind
	binary.BigEndian.PutUint64(head[1:9], uint64(at.UTC().UnixMilli()))
	binary.BigEndian.PutUint32(head[9:13], uint32(len(payload)))
	if _, err := f.Write(head); err != nil {
		return err
	}
	if _, err := f.Write(payload); err != nil {
		return err
	}
	return f.Sync()
}

func readRecord(reader *bufio.Reader) (byte, time.Time, []byte, error) {
	head := make([]byte, recordHeaderLen)
	if _, err := io.ReadFull(reader, head); err != nil {
		// A clean end reads as EOF with no bytes of a record present. Anything
		// partial is a crash mid-write, and that distinction is what lets playback
		// say "the file ends mid-record" instead of presenting a short recording as
		// a complete one.
		if errors.Is(err, io.EOF) {
			return 0, time.Time{}, nil, io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, time.Time{}, nil, ErrTruncated
		}
		return 0, time.Time{}, nil, err
	}
	kind := head[0]
	at := time.UnixMilli(int64(binary.BigEndian.Uint64(head[1:9]))).UTC()
	length := binary.BigEndian.Uint32(head[9:13])
	if length > MaxFrameBytes {
		return 0, time.Time{}, nil, fmt.Errorf("recordings: record of %d bytes exceeds the cap", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, time.Time{}, nil, ErrTruncated
		}
		return 0, time.Time{}, nil, err
	}
	return kind, at, payload, nil
}

func markerPayload(m Marker) []byte {
	payload, err := json.Marshal(m)
	if err != nil {
		// A marker is a note about the recording, never a reason to lose a frame.
		return []byte(`{"kind":"note"}`)
	}
	return payload
}

// replayHash reads an existing file into the running digest and reports its size
// and the frames it already holds, so a checksum stated after a restart
// describes the whole file and not just its tail, the row's byte count is the
// file's size, and the frame count continues where the file left off. A
// recording whose manifest undercounts its frames is a recording an operator
// reads as shorter than it is.
func replayHash(path string, entry *openRecording) (int64, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("recordings: read %s to resume it: %w", filepath.Base(path), err)
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	header := make([]byte, headerLen)
	n, err := io.ReadFull(reader, header)
	if err != nil {
		return int64(n), 0, fmt.Errorf("recordings: resume %s: %w", filepath.Base(path), err)
	}
	entry.digest.Write(header)
	total := int64(len(header))
	frames := 0
	for {
		kind, _, payload, err := readRecord(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A file cut mid-record is resumed as it stands: the bytes present are
			// the recording, and the reader reports the truncation.
			break
		}
		total += int64(recordHeaderLen + len(payload))
		entry.digest.Write(payload)
		if kind == RecordFrame {
			frames++
		}
	}
	return total, frames, nil
}
