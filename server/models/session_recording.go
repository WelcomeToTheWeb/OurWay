package models

import "time"

// Session recording (2.4-8): the frame sequence a remote-control session left
// behind, so the audit log has an answer to "who was controlling that machine at
// 14:00" that is not a reconstruction.
//
// The row is a manifest, not the media: the frames live in an append-only file
// under `RECORDINGS_DIR` and the row states what is in it — how many frames, how
// many bytes, where, until when, and why it exists at all. A recording with no
// reason is a recording nobody can justify to the person whose screen it shows,
// so `Reason` is required, not decorative.
type SessionRecording struct {
	ID        string `gorm:"type:uuid;primaryKey" json:"id"`
	SessionID string `gorm:"type:uuid;not null;uniqueIndex" json:"session_id"`
	DeviceID  string `gorm:"type:uuid;not null;index" json:"device_id"`
	UserID    string `gorm:"type:uuid;not null;index" json:"user_id"`
	// ClientID is the tenant the unit belonged to **at recording time**. 2.3.1's
	// scoping rule is enforced on reads by joining through the device, and this
	// column is what the ledger prints and the retention sweep counts: a unit
	// moved to another tenant tomorrow does not move its recording.
	ClientID string `gorm:"type:uuid;index" json:"client_id"`

	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`

	// FrameCount and Bytes are what the writer last flushed, not what it hopes
	// to write: the console prints them, the retention sweep compares them
	// against the file, and a recording whose stated bytes do not match the
	// file is reported as the damaged artifact it is.
	FrameCount int64  `gorm:"not null;default:0" json:"frame_count"`
	Bytes      int64  `gorm:"not null;default:0" json:"bytes"`
	Path       string `gorm:"not null" json:"-"`
	// Checksum is the SHA-256 of the sequence file as of the last flush.
	// A recording is evidence, so the file it names is stated by content.
	Checksum string `json:"checksum"`

	// Interruptions counts the times the writer reopened this file after it
	// had stopped writing to it — a server restart mid-session, an agent that
	// fell over and came back. The file is append-only, so the media survives;
	// what must not be pretended is continuity. Playback reads the gap markers
	// in the file and shows the break, and the row states how many there were.
	Interruptions int `gorm:"not null;default:0" json:"interruptions"`
	// DroppedFrames counts frames the server refused to append (over the size
	// cap, a writer that failed). A recording that silently lost frames is a
	// recording an operator would read as complete.
	DroppedFrames int64 `gorm:"not null;default:0" json:"dropped_frames"`

	// Reason names why this recording exists, in the vocabulary `RecordingReason*`
	// below. It is printed on the row: "the installation records control
	// sessions", "the operator ordered this one recorded", "the technician
	// recorded their own session".
	Reason string `gorm:"not null" json:"reason"`
	// Policy is the installation's recording mode at recording time
	// (`off`/`on`/`control-only`), recorded so a recording read a year later
	// says the rule it was made under, not the rule that happens to be set now.
	Policy string `gorm:"not null" json:"policy"`

	// RetentionUntil is when the sweep deletes this row and its file
	// (RECORDING_RETENTION_DAYS). It is a column, not a computed expression, so
	// the sweep is a query and the console can state "this recording is deleted
	// in 12 days" before anyone needs it.
	RetentionUntil *time.Time `json:"retention_until"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Recording policy modes (`RECORD_SESSIONS`). The default is **off**: recording
// a person's screen is a promise the installation makes, and an installation
// that records by default is an installation that surprised somebody.
const (
	RecordingModeOff       = "off"
	RecordingModeOn        = "on"
	RecordingModeControl   = "control-only"
	RecordingModeControlLo = "control_only"
)

// ValidRecordingMode is the vocabulary `RECORD_SESSIONS` accepts. An unparseable
// value is refused at boot with a log line, not silently read as "off": the
// difference between off and a typo is the difference between "we do not record"
// and "we do not record, quietly".
func ValidRecordingMode(mode string) bool {
	switch normalizeRecordingMode(mode) {
	case RecordingModeOff, RecordingModeOn, RecordingModeControl:
		return true
	default:
		return false
	}
}

// NormalizeRecordingMode maps the accepted spellings onto the canonical three.
// `control_only` is accepted because an operator typing an env var names the
// value they mean, and the underscore form is what the neighbouring vars use.
func NormalizeRecordingMode(mode string) string { return normalizeRecordingMode(mode) }

func normalizeRecordingMode(mode string) string {
	switch mode {
	case RecordingModeControlLo:
		return RecordingModeControl
	default:
		return mode
	}
}

// RecordingModeRecords reports whether this mode records sessions on its own.
// `off` records nothing; `on` records every session from its start;
// `control-only` records a session from the moment control input arrives,
// because a view-only session is not the act the question is about.
func RecordingModeRecords(mode string) bool {
	switch normalizeRecordingMode(mode) {
	case RecordingModeOn, RecordingModeControl:
		return true
	default:
		return false
	}
}

// RecordingModeIsControlOnly is the lazy-start case: frames before the first
// control input are not kept.
func RecordingModeIsControlOnly(mode string) bool {
	return normalizeRecordingMode(mode) == RecordingModeControl
}

// Why a recording exists. `Reason` is required on every row, and each value
// names an actor: a policy, an admin's order, a technician's own choice.
const (
	// RecordingReasonPolicyOn: the installation records every session.
	RecordingReasonPolicyOn = "policy:on"
	// RecordingReasonPolicyControl: the installation records sessions in which
	// someone controls a machine, from the moment they start.
	RecordingReasonPolicyControl = "policy:control-only"
	// RecordingReasonOperatorOrdered: an admin ordered this session recorded
	// (audited `session.record_start`).
	RecordingReasonOperatorOrdered = "operator:ordered"
	// RecordingReasonSelfRecorded: the technician recording their own session
	// (audited `session.record_start`).
	RecordingReasonSelfRecorded = "technician:self"
)

// RecordingStatus is what the recording is doing right now. `recording` is a
// live artifact — the file is open and frames are being appended; `complete`
// is closed and readable; `missing` is a row whose file is gone, which the
// console states as the damaged artifact it is rather than as a playable one.
const (
	RecordingStatusRecording = "recording"
	RecordingStatusComplete  = "complete"
	RecordingStatusMissing   = "missing"
)

// RecordingStatusOf derives the status from the row and the state of its file.
// The file is checked by the caller that can see the filesystem; this function
// only states the rule.
func RecordingStatusOf(endedAt *time.Time, fileExists bool) string {
	if endedAt == nil {
		return RecordingStatusRecording
	}
	if !fileExists {
		return RecordingStatusMissing
	}
	return RecordingStatusComplete
}
