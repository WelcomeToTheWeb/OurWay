package api

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/recordings"
)

// The recording lifecycle of a live session (2.4-8), kept beside the frame path
// that produces the frames rather than in a package that would have to guess
// when a session is live.
//
// Three rules shape it:
//
//   - **The installation's policy is the reason, and it is recorded.** `on`
//     records from the session's start; `control-only` records from the moment
//     someone sends control input, because a view-only session is not the act
//     "who was controlling that machine" is about; `off` records nothing.
//   - **Nobody switches a recording off.** There is no stop path in the product.
//     A technician who can end the installation's recording of their own session
//     has a recording that ends exactly when it stops helping them, which is the
//     opposite of why the audit log asks for it.
//   - **The frame path never fails because recording failed.** A recording that
//     cannot be written is logged and counted as dropped; the live session keeps
//     running. Remote control is the product, the recording is its record.

// errRecordingUnavailable is the answer when a recording cannot be opened: the
// policy is off, the writer is not wired, or the row could not be written.
var errRecordingUnavailable = errors.New("recording is not available on this installation")

// liveRecording is the recording this process is appending to for one session.
type liveRecording struct {
	recordingID string
}

// RecordingState is the recording wiring a session handler needs: the policy, the
// writer, and the sessions this process is writing for.
type RecordingState struct {
	policy recordings.Policy
	writer *recordings.Writer

	mu      sync.Mutex
	live    map[string]*liveRecording // session id -> recording
	control map[string]bool           // sessions that received control input
}

// WithRecordings attaches the recording policy and writer to the session
// handler. Without it the handler behaves as an installation that does not
// record, which is what `RECORD_SESSIONS=off` means.
func (h *SessionHandler) WithRecordings(policy recordings.Policy, writer *recordings.Writer) *SessionHandler {
	policy.Normalize()
	h.recording = &RecordingState{
		policy:  policy,
		writer:  writer,
		live:    map[string]*liveRecording{},
		control: map[string]bool{},
	}
	return h
}

// RecordingPolicy reports the policy the session handler runs with, for the
// routes that state it to the console.
func (h *SessionHandler) RecordingPolicy() recordings.Policy {
	if h.recording == nil {
		return recordings.Policy{Mode: models.RecordingModeOff}
	}
	return h.recording.policy
}

// RecordingMode is the installation's recording policy as the console reads it.
func (h *SessionHandler) RecordingMode() string { return h.RecordingPolicy().Mode }

// startRecording creates the recording row and opens its file. The row is
// created first: a frame written with no row is media nobody can find.
//
// It returns the row, and nil when recording could not start — which the frame
// path treats as "this frame is not recorded", stated in the log, never as a
// failed session.
func (h *SessionHandler) startRecording(session *models.Session, device *models.Device, reason string) *models.SessionRecording {
	if h.recording == nil || h.recording.writer == nil {
		return nil
	}
	state := h.recording
	policy := state.policy

	state.mu.Lock()
	if existing := state.live[session.ID]; existing != nil {
		state.mu.Unlock()
		return nil
	}
	state.mu.Unlock()

	// One recording per session, by the unique index. This is also the restart
	// case: the server came back, the session is still live, and the row — with
	// its path — is the thing that lets the file be reopened. `Start` on an
	// existing file appends to it and counts the interruption.
	if existing, err := h.store.Recordings.GetBySession(session.ID); err == nil && existing != nil {
		if existing.Path == "" {
			existing.Path = policy.PathFor(existing.ID)
			if err := h.store.Recordings.SetPath(existing.ID, existing.Path); err != nil {
				log.Printf("recordings: recording %s: the reopened file's path could not be stated on the row: %v", existing.ID, err)
			}
		}
		if err := state.writer.Start(existing); err != nil {
			log.Printf("recordings: session %s: the existing recording %s could not be reopened (%v); its frames are not being appended",
				session.ID, existing.ID, err)
		}
		state.mu.Lock()
		state.live[session.ID] = &liveRecording{recordingID: existing.ID}
		state.mu.Unlock()
		return existing
	}

	now := time.Now().UTC()
	retention := now.Add(policy.Retention)
	rec := &models.SessionRecording{
		ID:             uuid.NewString(),
		SessionID:      session.ID,
		DeviceID:       session.DeviceID,
		UserID:         session.UserID,
		StartedAt:      now,
		Reason:         reason,
		Policy:         policy.Mode,
		RetentionUntil: &retention,
	}
	// The path is stated before the row exists: a manifest whose `path` is empty
	// is a manifest that points at nothing, and a restart cannot reopen a file the
	// row does not name.
	rec.Path = policy.PathFor(rec.ID)
	if device == nil {
		device, _ = h.store.Devices.GetByID(session.DeviceID)
	}
	if device != nil && device.ClientID != nil {
		rec.ClientID = *device.ClientID
	}
	if err := h.store.Recordings.Create(rec); err != nil {
		log.Printf("recordings: session %s: the recording row could not be written (%v); this session is NOT recorded", session.ID, err)
		return nil
	}
	if err := state.writer.Start(rec); err != nil {
		log.Printf("recordings: session %s: the recording file could not be opened (%v); the row stands and states no frames", session.ID, err)
	}
	state.mu.Lock()
	state.live[session.ID] = &liveRecording{recordingID: rec.ID}
	state.mu.Unlock()
	return rec
}

// appendFrame writes one frame of a live session, starting the recording first
// when the policy says this session is recorded and it has not started.
func (h *SessionHandler) appendFrame(session *models.Session, jpeg []byte, at time.Time) {
	if h.recording == nil || h.recording.writer == nil {
		return
	}
	state := h.recording
	mode := state.policy.Mode
	if mode == models.RecordingModeOff {
		return
	}

	state.mu.Lock()
	current := state.live[session.ID]
	seenControl := state.control[session.ID]
	state.mu.Unlock()

	if current == nil {
		switch {
		case mode == models.RecordingModeOn:
			if rec := h.startRecording(session, nil, models.RecordingReasonPolicyOn); rec == nil {
				return
			}
		case mode == models.RecordingModeControl && seenControl:
			// The control-only policy started the recording at the input event;
			// a recording that starts here is the frames from now on, and the
			// session's earlier minutes are not kept. The row says which policy
			// made it, so nobody reads it as a complete session.
			if rec := h.startRecording(session, nil, models.RecordingReasonPolicyControl); rec == nil {
				return
			}
		default:
			return
		}
		state.mu.Lock()
		current = state.live[session.ID]
		state.mu.Unlock()
		if current == nil {
			return
		}
	}

	if err := state.writer.Append(current.recordingID, jpeg, at); err != nil {
		log.Printf("recordings: session %s: frame dropped (%v)", session.ID, err)
		return
	}

	// The manifest row is updated every fifth frame of the **file**, not every
	// frame: the frame path is the live session's hot loop, and a manifest a few
	// seconds behind is a manifest the console can read. Counting against the file
	// and not against this process matters on a resumed recording — the frames
	// written before the restart are part of the count, so the row never reads as a
	// recording that started over. The session's end writes the final counters.
	if stats, ok := state.writer.Stats(current.recordingID); ok && stats.Frames%5 == 0 {
		if err := h.store.Recordings.Flush(current.recordingID, stats.Frames, stats.Bytes, stats.Checksum, stats.Interruptions, stats.Dropped); err != nil {
			// The manifest is a statement about the file, so a manifest that cannot
			// be written is stated: the console reads a row that is behind, not a row
			// that pretends to be current.
			log.Printf("recordings: recording %s: the manifest could not be updated: %v", current.recordingID, err)
		}
	}
}

// noteControlInput records that a session received control input. Under
// `control-only` this is the moment the recording begins, so it starts one if
// the session has frames arriving.
func (h *SessionHandler) noteControlInput(sessionID string) {
	if h.recording == nil {
		return
	}
	state := h.recording
	state.mu.Lock()
	state.control[sessionID] = true
	state.mu.Unlock()

	if !models.RecordingModeIsControlOnly(state.policy.Mode) {
		return
	}
	if _, ok := state.writer.Stats(h.recordingIDFor(sessionID)); ok {
		return
	}
	session, err := h.store.Sessions.GetByID(sessionID)
	if err != nil || session == nil {
		return
	}
	if session.Status != "active" && session.Status != "pending" {
		return
	}
	device, _ := h.store.Devices.GetByID(session.DeviceID)
	h.startRecording(session, device, models.RecordingReasonPolicyControl)
}

func (h *SessionHandler) recordingIDFor(sessionID string) string {
	if h.recording == nil {
		return ""
	}
	h.recording.mu.Lock()
	defer h.recording.mu.Unlock()
	if current := h.recording.live[sessionID]; current != nil {
		return current.recordingID
	}
	return ""
}

// StartRecordingByRequest starts a recording for a session an operator asked for
// (the toggle). It never stops one, and it is the reason the row carries who
// asked.
func (h *SessionHandler) StartRecordingByRequest(session *models.Session, device *models.Device, reason string) (*models.SessionRecording, error) {
	rec := h.startRecording(session, device, reason)
	if rec == nil {
		if existing, err := h.store.Recordings.GetBySession(session.ID); err == nil && existing != nil {
			return existing, nil
		}
		return nil, errRecordingUnavailable
	}
	return rec, nil
}

// finishRecording closes a session's recording: the writer's final counters go
// to the row, and the row gets its `ended_at`. The console reads a row with no
// `ended_at` as "being recorded", so a recording that is never closed is a
// recording the console lies about.
func (h *SessionHandler) finishRecording(sessionID string) {
	if h.recording == nil {
		return
	}
	state := h.recording
	state.mu.Lock()
	current := state.live[sessionID]
	delete(state.control, sessionID)
	state.mu.Unlock()
	if current == nil {
		return
	}
	stats, _, err := state.writer.Finish(current.recordingID)
	if err != nil {
		log.Printf("recordings: recording %s closed with an error: %v", current.recordingID, err)
	}
	if err := h.store.Recordings.End(current.recordingID, stats.Frames, stats.Bytes, stats.Checksum, stats.Interruptions, stats.Dropped); err != nil {
		log.Printf("recordings: recording %s could not be closed in the database: %v", current.recordingID, err)
	}
	state.mu.Lock()
	delete(state.live, sessionID)
	state.mu.Unlock()
}

// LiveRecordingStats refreshes a recording's stated counters from the writer that
// is appending to it, and returns them. A live recording's row is a **sample** —
// the frame path writes it every fifth frame, because the frame path is the live
// session's hot loop — so the read that shows the recording to a person takes the
// live numbers and states them. The console's answer is exact, and the frame path
// stays cheap.
//
// Nil means this process is not writing that recording: the row's own numbers are
// all that is known, and they are the truth of a file nobody is appending to.
func (h *SessionHandler) LiveRecordingStats(recordingID string) *recordings.Stats {
	if h.recording == nil || h.recording.writer == nil {
		return nil
	}
	stats, ok := h.recording.writer.Stats(recordingID)
	if !ok {
		return nil
	}
	if err := h.store.Recordings.Flush(recordingID, stats.Frames, stats.Bytes, stats.Checksum, stats.Interruptions, stats.Dropped); err != nil {
		log.Printf("recordings: recording %s: the live counters could not be stated on the row: %v", recordingID, err)
	}
	return &stats
}

// RecordingForSession reports the recording of a session, for the live session
// panel: the operator-visible promise that says plainly when a session **is**
// being recorded.
type RecordingForSession struct {
	ID            string    `json:"id"`
	Status        string    `json:"status"`
	Frames        int64     `json:"frame_count"`
	Bytes         int64     `json:"bytes"`
	Reason        string    `json:"reason"`
	Policy        string    `json:"policy"`
	StartedAt     time.Time `json:"started_at"`
	Interruptions int       `json:"interruptions"`
	Dropped       int64     `json:"dropped_frames"`
}

// SessionRecordingInfo reads the recording of one session in the shape the
// session surfaces print.
func (h *SessionHandler) SessionRecordingInfo(sessionID string) *RecordingForSession {
	if h.store == nil || h.store.Recordings == nil {
		return nil
	}
	rec, err := h.store.Recordings.GetBySession(sessionID)
	if err != nil || rec == nil {
		return nil
	}
	return recordingInfoOf(h.RecordingPolicy(), rec)
}

func recordingInfoOf(policy recordings.Policy, rec *models.SessionRecording) *RecordingForSession {
	return &RecordingForSession{
		ID:            rec.ID,
		Status:        models.RecordingStatusOf(rec.EndedAt, policy.RowExists(rec)),
		Frames:        rec.FrameCount,
		Bytes:         rec.Bytes,
		Reason:        rec.Reason,
		Policy:        rec.Policy,
		StartedAt:     rec.StartedAt,
		Interruptions: rec.Interruptions,
		Dropped:       rec.DroppedFrames,
	}
}
