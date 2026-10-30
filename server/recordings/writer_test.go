package recordings

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ourway/server/models"
)

func testPolicy(t *testing.T, mode string) Policy {
	t.Helper()
	p := Policy{Mode: mode, Dir: t.TempDir(), Retention: 30 * 24 * time.Hour, MaxBytes: 1 << 20}
	p.Normalize()
	return p
}

func writeFrames(t *testing.T, w *Writer, id string, count int, at time.Time) {
	t.Helper()
	for i := 0; i < count; i++ {
		if err := w.Append(id, []byte("jpeg-frame-"+string(rune('A'+i))), at.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
}

// A recording is playable without a re-encode step, and it survives a crash
// mid-write. Those two properties are the whole reason the format is an
// append-only sequence of length-prefixed frames rather than a container with a
// trailer, and the test pins them: the file's header is the format's name, the
// frames read back in order with their timestamps, and the bytes the writer
// reports are the bytes on disk.
func TestSequenceIsAppendOnlyFramesWithTimestamps(t *testing.T) {
	policy := testPolicy(t, models.RecordingModeOn)
	w := NewWriter(policy)
	id := "rec-1"
	start := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)

	if err := w.Start(&models.SessionRecording{ID: id, Path: policy.PathFor(id), StartedAt: start}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	writeFrames(t, w, id, 3, start)
	stats, _, err := w.Finish(id)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	raw, err := os.ReadFile(policy.PathFor(id))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte(Magic)) {
		t.Fatalf("file does not start with the format's magic: %q", raw[:6])
	}

	seq, err := Read(policy.PathFor(id))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if seq.FrameCount != 3 || len(seq.Frames) != 3 {
		t.Fatalf("frames = %d (%d parsed), want 3", seq.FrameCount, len(seq.Frames))
	}
	for i, frame := range seq.Frames {
		if frame.Index != i {
			t.Fatalf("frame %d: index %d", i, frame.Index)
		}
		want := start.Add(time.Duration(i) * time.Second).UnixMilli()
		if frame.At.UnixMilli() != want {
			t.Fatalf("frame %d: at %d, want %d (the overlay is built against these)", i, frame.At.UnixMilli(), want)
		}
	}
	if stats.Frames != int64(seq.FrameCount) || stats.Bytes != seq.Bytes {
		t.Fatalf("the writer reported %d frames / %d bytes, the file says %d / %d", stats.Frames, stats.Bytes, seq.FrameCount, seq.Bytes)
	}
	if stats.Checksum != seq.Checksum {
		t.Fatalf("the writer's checksum %q is not the file's %q", stats.Checksum, seq.Checksum)
	}

	// A single frame is addressable: playback asks for one frame at a time, and
	// it must get the frame the index names.
	frame, err := FrameAt(policy.PathFor(id), 1)
	if err != nil {
		t.Fatalf("FrameAt: %v", err)
	}
	if frame.Index != 1 || !bytes.Contains(frame.JPEG, []byte("B")) {
		t.Fatalf("FrameAt(1) = %+v", frame)
	}
	if _, err := FrameAt(policy.PathFor(id), 99); err == nil {
		t.Fatal("FrameAt(99) succeeded: a frame past the end must not answer")
	}
}

// A recording that is resumed is one recording, not two: the server restarting
// mid-session appends to the same file, and the sequence says how often it was
// restarted. A recording that pretends continuity is a recording that hides the
// minutes nobody recorded.
func TestResumeAppendsToTheSameRecordingAndCountsTheInterruption(t *testing.T) {
	policy := testPolicy(t, models.RecordingModeOn)
	path := policy.PathFor("rec-2")
	start := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)

	first := NewWriter(policy)
	if err := first.Start(&models.SessionRecording{ID: "rec-2", Path: path, StartedAt: start}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	writeFrames(t, first, "rec-2", 2, start)
	first.Close()

	second := NewWriter(policy)
	if err := second.Start(&models.SessionRecording{ID: "rec-2", Path: path, StartedAt: start}); err != nil {
		t.Fatalf("resume Start: %v", err)
	}
	writeFrames(t, second, "rec-2", 2, start.Add(3*time.Minute))
	stats, _, err := second.Finish("rec-2")
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	seq, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if seq.FrameCount != 4 {
		t.Fatalf("frames = %d, want 4 (the resume appended to the same recording)", seq.FrameCount)
	}
	if seq.Interruptions != 1 {
		t.Fatalf("interruptions = %d, want 1", seq.Interruptions)
	}
	found := false
	for _, marker := range seq.Markers {
		if marker.Kind == "resume" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no resume marker among %+v", seq.Markers)
	}
	if stats.Interruptions != seq.Interruptions {
		t.Fatalf("the writer reported %d interruptions, the file says %d", stats.Interruptions, seq.Interruptions)
	}
}

// An interrupted write leaves the frames before it readable, and the sequence
// says it is incomplete. A half-written file is evidence, and the honest reading
// of evidence is "the last frame here arrived at 14:03, and nothing after it".
func TestTruncatedTailKeepsEarlierFramesAndSaysItIsTruncated(t *testing.T) {
	policy := testPolicy(t, models.RecordingModeOn)
	path := policy.PathFor("rec-3")
	start := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)

	w := NewWriter(policy)
	if err := w.Start(&models.SessionRecording{ID: "rec-3", Path: path, StartedAt: start}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	writeFrames(t, w, "rec-3", 3, start)
	w.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(path, raw[:len(raw)-5], 0o600); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	seq, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v (a truncated recording must stay readable)", err)
	}
	if seq.FrameCount != 2 {
		t.Fatalf("frames = %d, want 2 (the frame with the cut tail is dropped)", seq.FrameCount)
	}
	if len(seq.Markers) == 0 || seq.Markers[len(seq.Markers)-1].Kind != "truncated" {
		t.Fatalf("markers = %+v, want a trailing truncated marker", seq.Markers)
	}
}

// The cap is the installation's disk promise, so hitting it is an event in the
// recording: the frames that arrived after it are counted as dropped, stated in
// the sequence, and never become a live session's failure.
func TestWriterCapDropsFramesAndCountsThem(t *testing.T) {
	policy := Policy{Mode: models.RecordingModeOn, Dir: t.TempDir(), Retention: 24 * time.Hour, MaxBytes: 60}
	policy.Normalize()
	w := NewWriter(policy)
	path := policy.PathFor("rec-4")
	if err := w.Start(&models.SessionRecording{ID: "rec-4", Path: path}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	payload := bytes.Repeat([]byte("x"), 30)
	accepted := 0
	dropped := 0
	for i := 0; i < 6; i++ {
		if err := w.Append("rec-4", payload, time.Now().Add(time.Duration(i)*time.Second)); err != nil {
			dropped++
			continue
		}
		accepted++
	}
	if accepted == 0 {
		t.Fatal("no frame was accepted: the cap is 60 bytes, at least one frame fits")
	}
	if dropped == 0 {
		t.Fatal("the cap dropped nothing: 6 frames of 30 bytes cannot fit in 40 bytes")
	}
	stats, _, err := w.Finish("rec-4")
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if stats.Dropped != int64(dropped) {
		t.Fatalf("dropped = %d, want %d", stats.Dropped, dropped)
	}
	seq, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	capMarker := false
	for _, marker := range seq.Markers {
		if marker.Kind == "cap" {
			capMarker = true
		}
	}
	if !capMarker {
		t.Fatalf("no cap marker among %+v", seq.Markers)
	}
	if seq.FrameCount != accepted {
		t.Fatalf("frames = %d, want %d", seq.FrameCount, accepted)
	}
}

func TestAppendWithoutStartIsRefused(t *testing.T) {
	policy := testPolicy(t, models.RecordingModeOn)
	w := NewWriter(policy)
	if err := w.Append("nope", []byte("x"), time.Now()); err == nil {
		t.Fatal("Append succeeded with no recording open")
	}
	if _, ok := w.Stats("nope"); ok {
		t.Fatal("Stats reported a recording that was never started")
	}
	if w.RecordingIsLive("nope") {
		t.Fatal("RecordingIsLive said yes for a recording that does not exist")
	}
}

// Frames are not text. An empty payload is not a frame, so it is not written and
// the frame count does not move: a recording of a session is a recording of
// pixels, and an empty record in the file would be a frame playback cannot show.
func TestAppendIgnoresEmptyPayloads(t *testing.T) {
	policy := testPolicy(t, models.RecordingModeOn)
	w := NewWriter(policy)
	id := "rec-5"
	if err := w.Start(&models.SessionRecording{ID: id, Path: policy.PathFor(id)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := w.Append(id, nil, time.Now()); err != nil {
		t.Fatalf("Append(nil): %v", err)
	}
	writeFrames(t, w, id, 1, time.Now())
	stats, _, err := w.Finish(id)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if stats.Frames != 1 {
		t.Fatalf("frames = %d, want 1: the empty payload must not become a frame", stats.Frames)
	}
}

// `RECORDINGS_DIR/<id>.seq` is derived from the row id and nothing else, so the
// manifest can point at a file the sweep can find again.
func TestPolicyPathIsTheRecordingID(t *testing.T) {
	p := Policy{Dir: "/var/lib/ourway/recordings"}
	p.Normalize()
	if got := p.PathFor("abc"); got != filepath.Join("/var/lib/ourway/recordings", "abc.seq") {
		t.Fatalf("PathFor = %q", got)
	}
}

// An invalid policy is `off`, and the defaults are the plan's numbers: the
// installation's recording promise is never the result of a typo.
func TestPolicyNormalizeDefaultsToNoRecording(t *testing.T) {
	p := Policy{}
	p.Normalize()
	if p.Mode != models.RecordingModeOff {
		t.Fatalf("mode = %q, want off", p.Mode)
	}
	if p.Dir == "" || p.MaxBytes <= 0 || p.Retention <= 0 {
		t.Fatalf("defaults missing: %+v", p)
	}
	if !RecordingModeIsOff(p.Mode) {
		t.Fatal("RecordingModeIsOff said no for the normalized default")
	}
	if RecordingModeIsOff(models.RecordingModeControl) {
		t.Fatal("RecordingModeIsOff said yes for control-only")
	}
}

// Markers are the sequence's own statements (resume, cap, truncation), so the
// reader must hand them back with their text.
func TestMarkersSurviveTheRoundTrip(t *testing.T) {
	policy := testPolicy(t, models.RecordingModeOn)
	w := NewWriter(policy)
	id := "rec-6"
	if err := w.Start(&models.SessionRecording{ID: id, Path: policy.PathFor(id)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := w.Mark(id, Marker{Kind: "operator_note", At: time.Now().UTC(), Text: "the operator ordered this recording"}); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	writeFrames(t, w, id, 1, time.Now())
	w.Close()

	seq, err := Read(policy.PathFor(id))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(seq.Markers) != 1 || seq.Markers[0].Kind != "operator_note" {
		t.Fatalf("markers = %+v", seq.Markers)
	}
	var want Marker
	want.Kind = "operator_note"
	want.Text = "the operator ordered this recording"
	if seq.Markers[0].Kind != want.Kind || seq.Markers[0].Text != want.Text {
		t.Fatalf("marker = %+v, want %+v", seq.Markers[0], want)
	}
	if seq.Markers[0].At.IsZero() {
		t.Fatal("the marker carries no time: playback cannot place it in the sequence")
	}
}

// Remove is the sweep's hand: the file goes with the row, and the manifest
// states that a recording whose file is gone is `missing` rather than playable.
func TestRemoveDeletesTheFileAndExistsReportsIt(t *testing.T) {
	policy := testPolicy(t, models.RecordingModeOn)
	w := NewWriter(policy)
	path := policy.PathFor("rec-7")
	if err := w.Start(&models.SessionRecording{ID: "rec-7", Path: path}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	writeFrames(t, w, "rec-7", 1, time.Now())
	w.Close()
	if !Exists(path) {
		t.Fatal("Exists said no for a file the writer just wrote")
	}
	if err := w.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if Exists(path) {
		t.Fatal("the recording file is still there after Remove")
	}
	if _, err := Read(path); err == nil {
		t.Fatal("Read succeeded on a removed recording")
	}
}
