package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ourway/server/models"
	"ourway/server/recordings"
	"ourway/server/store"
)

// RecordingHandler serves 2.4-8: the recording ledger, the manifest, the frames,
// the input overlay, and the toggle that orders a session recorded.
type RecordingHandler struct {
	store    *store.Store
	sessions *SessionHandler
}

// NewRecordingHandler wires the recording surface to the session handler that
// owns the writer.
func NewRecordingHandler(st *store.Store, sessions *SessionHandler) *RecordingHandler {
	return &RecordingHandler{store: st, sessions: sessions}
}

// RecordingView is a recording as the console reads it. The file path is not in
// it: a path is a server-side fact, and the id is the handle every read uses.
type RecordingView struct {
	ID             string     `json:"id"`
	SessionID      string     `json:"session_id"`
	DeviceID       string     `json:"device_id"`
	DeviceName     string     `json:"device_name"`
	ClientID       string     `json:"client_id"`
	UserID         string     `json:"user_id"`
	Username       string     `json:"username"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason"`
	Policy         string     `json:"policy"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at"`
	Frames         int64      `json:"frame_count"`
	Bytes          int64      `json:"bytes"`
	Checksum       string     `json:"checksum"`
	Interruptions  int        `json:"interruptions"`
	Dropped        int64      `json:"dropped_frames"`
	RetentionUntil *time.Time `json:"retention_until"`
	Playable       bool       `json:"playable"`
}

// FrameStamp is one frame's place in the sequence: its index and the time the
// server received it. The overlay is built against these, which is why the
// timestamps are on the index and not implied by frame order.
type FrameStamp struct {
	Index int       `json:"index"`
	At    time.Time `json:"at"`
}

// StartRecording is the toggle: `POST /api/sessions/:id/recording`.
//
// It starts a recording. There is no route that stops one — a participant who
// can end the installation's recording of their own session has a recording that
// ends exactly when it stops helping them, which is the opposite of why the
// audit log asks for one. The only end a recording has is its session's end.
//
// Who may start one: the session's own operator (recording what you did is
// always allowed), and an admin, for a session inside their scope.
func (h *RecordingHandler) StartRecording(c *gin.Context) {
	sessionID := c.Param("id")
	session, err := h.store.Sessions.GetByID(sessionID)
	if err != nil {
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}

	userID := CallerUserID(c)
	isAdmin := hasRole(c, RoleAdmin)
	if session.UserID != userID && !isAdmin {
		// The same answer a session that does not exist gets: a body that
		// distinguishes "not yours" from "absent" says the session exists.
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}

	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)
	ok, err := scope.AllowsDeviceID(c, session.DeviceID, h.store)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve client scope"})
		return
	}
	if !ok {
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}

	if session.Status == "ended" || session.Status == "error" {
		c.JSON(409, gin.H{"error": "the session is over: a recording starts while the session is live", "reason": "session_over"})
		return
	}

	if h.sessions.RecordingMode() == models.RecordingModeOff {
		c.JSON(409, gin.H{
			"error":  "this installation does not record sessions (RECORD_SESSIONS=off): there is nothing to record to",
			"reason": "recording_disabled",
		})
		return
	}

	reason := models.RecordingReasonSelfRecorded
	if session.UserID != userID {
		reason = models.RecordingReasonOperatorOrdered
	}
	device, _ := h.store.Devices.GetByID(session.DeviceID)
	rec, err := h.sessions.StartRecordingByRequest(session, device, reason)
	if err != nil {
		if errors.Is(err, errRecordingUnavailable) {
			c.JSON(409, gin.H{"error": "the recording could not be opened", "reason": "recording_unavailable"})
			return
		}
		c.JSON(500, gin.H{"error": "the recording could not be started"})
		return
	}

	defer auditForAnswer(c, h.store, models.AuditSessionRecordStart, h.targetFor(rec), map[string]any{
		"session_id": rec.SessionID,
		"reason":     rec.Reason,
		"policy":     rec.Policy,
	})
	c.JSON(http.StatusCreated, gin.H{"recording": h.viewOf(rec)})
}

// ForSession answers `GET /api/sessions/:id/recording`: the recording of this
// session, or its absence. The live session panel reads it to state plainly that
// a session **is** being recorded.
func (h *RecordingHandler) ForSession(c *gin.Context) {
	sessionID := c.Param("id")
	rec, err := h.store.Recordings.GetBySession(sessionID)
	if err != nil {
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}
	if !h.authorizeRecording(c, rec) {
		return
	}
	c.JSON(200, gin.H{"recording": h.viewOf(rec), "playable": h.playable(rec)})
}

// List answers `GET /api/recordings`: the recordings this caller can see.
//
// Two rules union: a person always reads their **own** recordings (the sessions
// they controlled, including units they have since lost scope over — the record
// of what you did does not disappear when your grants change), and a scoped
// caller additionally reads the recordings of the tenants they are granted
// (2.3.1). Admin reads the installation.
func (h *RecordingHandler) List(c *gin.Context) {
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)
	userID := CallerUserID(c)
	limit := listLimit(c)

	var (
		recs []models.SessionRecording
	)
	if scope.All() {
		recs, err = h.store.Recordings.ListAll(limit)
	} else {
		scoped, scopeErr := h.store.Recordings.ListScoped(scope.ClientIDs(), false, limit)
		own, ownErr := h.store.Recordings.ListOwn(userID, limit)
		if scopeErr != nil || ownErr != nil {
			c.JSON(500, gin.H{"error": "failed to list recordings"})
			return
		}
		recs = mergeRecordings(scoped, own, limit)
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list recordings"})
		return
	}

	views := make([]RecordingView, 0, len(recs))
	for i := range recs {
		views = append(views, h.viewOf(&recs[i]))
	}
	c.JSON(200, gin.H{
		"recordings":       views,
		"recording_policy": h.sessions.RecordingMode(),
		"retention_days":   int(h.sessions.RecordingPolicy().Retention / (24 * time.Hour)),
	})
}

// Manifest answers `GET /api/recordings/:id` and is the act that **playing a
// recording** starts with, so it is the audited one: the row names who played,
// which recording, and the session it records. The frames themselves are the
// playback stream and are not audited per frame — a ledger of every frame a
// person looked at is a second system to read, and the manifest row already
// names the act.
func (h *RecordingHandler) Manifest(c *gin.Context) {
	rec, view, ok := h.authorizedRecording(c)
	if !ok {
		return
	}
	defer auditForAnswer(c, h.store, models.AuditSessionRecordingPlay, h.targetFor(rec), map[string]any{
		"session_id": rec.SessionID,
		"frames":     rec.FrameCount,
	})

	body := gin.H{"recording": view, "playable": h.playable(rec)}
	policy := h.sessions.RecordingPolicy()
	if seq, err := recordings.Read(policy.PathOfRow(rec)); err == nil {
		stamps := make([]FrameStamp, 0, len(seq.Frames))
		for _, frame := range seq.Frames {
			stamps = append(stamps, FrameStamp{Index: frame.Index, At: frame.At})
		}
		body["frames"] = stamps
		body["markers"] = seq.Markers
		body["bytes_on_disk"] = seq.Bytes
		body["checksum_on_disk"] = seq.Checksum
		if seq.Checksum != rec.Checksum && rec.Checksum != "" {
			// The file is not what the row says it is. Stated, because a
			// recording whose content moved is a recording nobody can rely on.
			body["checksum_mismatch"] = true
		}
	} else if h.playable(rec) {
		body["index_error"] = err.Error()
	}
	c.JSON(200, body)
}

// Frames answers `GET /api/recordings/:id/frames`: the index alone, for a
// scrubber that wants timestamps without the manifest's audit trail.
func (h *RecordingHandler) Frames(c *gin.Context) {
	rec, _, ok := h.authorizedRecording(c)
	if !ok {
		return
	}
	seq, err := recordings.Read(h.sessions.RecordingPolicy().PathOfRow(rec))
	if err != nil {
		c.JSON(404, gin.H{"error": "the recording's file is not readable", "reason": "recording_unreadable"})
		return
	}
	stamps := make([]FrameStamp, 0, len(seq.Frames))
	for _, frame := range seq.Frames {
		stamps = append(stamps, FrameStamp{Index: frame.Index, At: frame.At})
	}
	c.JSON(200, gin.H{"frames": stamps, "markers": seq.Markers, "interruptions": seq.Interruptions})
}

// Frame answers `GET /api/recordings/:id/frames/:index` with one JPEG. This is
// the playback stream: 200 `image/jpeg`, and the caching header says the frame
// is the frame — a recording does not change under an operator watching it.
func (h *RecordingHandler) Frame(c *gin.Context) {
	rec, _, ok := h.authorizedRecording(c)
	if !ok {
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		c.JSON(400, gin.H{"error": "frame index must be a number"})
		return
	}
	frame, err := recordings.FrameAt(h.sessions.RecordingPolicy().PathOfRow(rec), index)
	if err != nil {
		c.JSON(404, gin.H{"error": "that frame is not in this recording", "reason": "frame_not_found"})
		return
	}
	c.Header("Cache-Control", "private, max-age=3600")
	c.Data(http.StatusOK, "image/jpeg", frame.JPEG)
}

// Events answers `GET /api/recordings/:id/events`: the keyboard and mouse the
// ledger recorded for this session, in the recording's own time window, so
// playback can show an act over the frame it produced.
//
// The events come from the audit ledger and not from the recording file. That is
// a deliberate choice with a cost worth naming: the ledger is the installation's
// record of acts, it is scoped, and it is retained by a policy the operator
// reads. Input copied into a media file would be a second copy of what a person
// did, kept for a different reason and pruned on a different schedule.
func (h *RecordingHandler) Events(c *gin.Context) {
	rec, _, ok := h.authorizedRecording(c)
	if !ok {
		return
	}
	until := time.Now().UTC()
	if rec.EndedAt != nil {
		until = rec.EndedAt.Add(30 * time.Second)
	}
	from := rec.StartedAt.Add(-30 * time.Second)
	rows, err := h.store.Audit.List(store.AuditFilter{
		ActorUserID: rec.UserID,
		TargetID:    rec.DeviceID,
		Since:       &from,
		Until:       &until,
		Limit:       500,
	})
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to read the session's ledger rows"})
		return
	}
	type event struct {
		At        time.Time `json:"at"`
		Action    string    `json:"action"`
		Target    string    `json:"target"`
		InputType string    `json:"input_type,omitempty"`
	}
	events := make([]event, 0, len(rows))
	for i := range rows {
		row := rows[i]
		if row.TargetID != rec.DeviceID {
			continue
		}
		details := decodeAuditDetails(row.Details)
		if sessionID, _ := details["session_id"].(string); sessionID != "" && sessionID != rec.SessionID {
			continue
		}
		if row.Action != models.AuditSessionInput && row.Action != models.AuditSessionStart &&
			row.Action != models.AuditSessionEnd && row.Action != models.AuditSessionQuality &&
			row.Action != models.AuditSessionRecordStart {
			continue
		}
		inputType, _ := details["input_type"].(string)
		events = append(events, event{
			At:        row.CreatedAt.UTC(),
			Action:    row.Action,
			Target:    row.TargetType,
			InputType: inputType,
		})
	}
	// Newest-first is the ledger's order; playback wants oldest-first.
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	c.JSON(200, gin.H{"events": events})
}

// DownloadManifest answers `GET /api/recordings/:id/manifest` — the manifest a
// person hands to somebody else: the session, the unit, the account, the frame
// count, the checksum, the policy and reason, the retention date. Audited,
// because exporting a recording out of the installation is the act that leaves a
// copy somewhere the installation does not control.
func (h *RecordingHandler) DownloadManifest(c *gin.Context) {
	rec, view, ok := h.authorizedRecording(c)
	if !ok {
		return
	}
	defer auditForAnswer(c, h.store, models.AuditSessionRecordingFetch, h.targetFor(rec), map[string]any{
		"session_id": rec.SessionID,
		"what":       "manifest",
	})
	if seq, err := recordings.Read(h.sessions.RecordingPolicy().PathOfRow(rec)); err == nil {
		view["frames"] = seq.FrameCount
		view["bytes_on_disk"] = seq.Bytes
		view["checksum_on_disk"] = seq.Checksum
		view["markers"] = seq.Markers
	}
	c.Header("Content-Disposition", `attachment; filename="recording-`+rec.ID+`.manifest.json"`)
	c.JSON(200, gin.H{"recording": view})
}

// DownloadFile answers `GET /api/recordings/:id/file` with the sequence file
// itself. Admin, session owner, and scoped callers of the unit's tenant — the
// same door as playback, because the file is the same fact.
func (h *RecordingHandler) DownloadFile(c *gin.Context) {
	rec, _, ok := h.authorizedRecording(c)
	if !ok {
		return
	}
	defer auditForAnswer(c, h.store, models.AuditSessionRecordingFetch, h.targetFor(rec), map[string]any{
		"session_id": rec.SessionID,
		"what":       "sequence",
		"bytes":      rec.Bytes,
	})
	c.Header("Content-Disposition", `attachment; filename="recording-`+rec.ID+`.seq"`)
	c.Header("Content-Type", "application/octet-stream")
	c.File(h.sessions.RecordingPolicy().PathOfRow(rec))
}

// ---------------------------------------------------------------------------
// Access and shape

// authorizeRecording is the 2.3.1 rule on the recording door: the caller's
// tenant scope decides, and a non-admin reads **their own** recordings. An
// out-of-scope recording answers the 404 that a recording that does not exist
// answers — a body that distinguishes them says the recording exists.
func (h *RecordingHandler) authorizeRecording(c *gin.Context, rec *models.SessionRecording) bool {
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return false
	}
	scope.ApplyScopeHeader(c)
	// Three doors, and the list uses the same three. Admin reads the
	// installation; a person always reads the record of what **they** did, even on
	// a unit they have since lost scope over (a technician's own sessions do not
	// become unaccountable when their grants change); and a manager or technician
	// reads the recordings of the tenants they are granted.
	if scope.All() {
		return true
	}
	if rec.UserID == CallerUserID(c) {
		return true
	}
	ok, err := scope.AllowsDeviceID(c, rec.DeviceID, h.store)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve client scope"})
		return false
	}
	if !ok {
		// A recording outside the caller's tenants answers the 404 a recording that
		// does not exist answers: a body that distinguishes them says the recording
		// exists.
		c.JSON(404, gin.H{"error": "recording not found"})
		return false
	}
	return true
}

func (h *RecordingHandler) authorizedRecording(c *gin.Context) (*models.SessionRecording, map[string]any, bool) {
	id := c.Param("id")
	rec, err := h.store.Recordings.GetByID(id)
	if err != nil {
		c.JSON(404, gin.H{"error": "recording not found"})
		return nil, nil, false
	}
	if !h.authorizeRecording(c, rec) {
		return nil, nil, false
	}
	view := h.viewMap(rec)
	return rec, view, true
}

func (h *RecordingHandler) playable(rec *models.SessionRecording) bool {
	return h.sessions.RecordingPolicy().RowExists(rec) && rec.FrameCount > 0
}

func (h *RecordingHandler) viewOf(rec *models.SessionRecording) RecordingView {
	// A recording this server is appending to is read from the writer, not from the
	// last sample the row holds: the operator's question is "how long is it so far",
	// and an answer that lags is an answer about a recording nobody is watching.
	if stats := h.sessions.LiveRecordingStats(rec.ID); stats != nil {
		rec.FrameCount = stats.Frames
		rec.Bytes = stats.Bytes
		rec.Checksum = stats.Checksum
		rec.Interruptions = stats.Interruptions
		rec.DroppedFrames = stats.Dropped
	}
	view := RecordingView{
		ID:             rec.ID,
		SessionID:      rec.SessionID,
		DeviceID:       rec.DeviceID,
		ClientID:       rec.ClientID,
		UserID:         rec.UserID,
		Status:         models.RecordingStatusOf(rec.EndedAt, h.sessions.RecordingPolicy().RowExists(rec)),
		Reason:         rec.Reason,
		Policy:         rec.Policy,
		StartedAt:      rec.StartedAt,
		EndedAt:        rec.EndedAt,
		Frames:         rec.FrameCount,
		Bytes:          rec.Bytes,
		Checksum:       rec.Checksum,
		Interruptions:  rec.Interruptions,
		Dropped:        rec.DroppedFrames,
		RetentionUntil: rec.RetentionUntil,
		Playable:       h.playable(rec),
	}
	if device, err := h.store.Devices.GetByID(rec.DeviceID); err == nil && device != nil {
		view.DeviceName = device.Name
	}
	if user, err := h.store.Users.GetByID(rec.UserID); err == nil && user != nil {
		view.Username = user.Username
	}
	return view
}

func (h *RecordingHandler) viewMap(rec *models.SessionRecording) map[string]any {
	view := h.viewOf(rec)
	body, err := json.Marshal(view)
	if err != nil {
		return map[string]any{"id": view.ID, "session_id": view.SessionID}
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return map[string]any{"id": view.ID, "session_id": view.SessionID}
	}
	return out
}

// decodeAuditDetails reads a ledger row's details back. The ledger stores them
// as JSON, and playback is the one reader that needs them typed.
func decodeAuditDetails(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(raw), &details); err != nil {
		return map[string]any{}
	}
	return details
}

func (h *RecordingHandler) targetFor(rec *models.SessionRecording) AuditTarget {
	target := AuditTarget{Type: "recording", ID: rec.ID, ClientID: rec.ClientID}
	if device, err := h.store.Devices.GetByID(rec.DeviceID); err == nil && device != nil {
		target = DeviceTarget(device)
		target.ID = rec.ID
		target.Type = "recording"
	}
	return target
}

// mergeRecordings unions the scoped and the caller's own readings, newest first,
// without double-counting a recording that is both.
func mergeRecordings(scoped, own []models.SessionRecording, limit int) []models.SessionRecording {
	seen := map[string]bool{}
	merged := make([]models.SessionRecording, 0, len(scoped)+len(own))
	for _, group := range [][]models.SessionRecording{scoped, own} {
		for i := range group {
			rec := group[i]
			if seen[rec.ID] {
				continue
			}
			seen[rec.ID] = true
			merged = append(merged, rec)
		}
	}
	sortRecordingsByStart(merged)
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}

func sortRecordingsByStart(recs []models.SessionRecording) {
	for i := 1; i < len(recs); i++ {
		for j := i; j > 0 && recs[j].StartedAt.After(recs[j-1].StartedAt); j-- {
			recs[j], recs[j-1] = recs[j-1], recs[j]
		}
	}
}

// listLimit bounds the recording ledger the way every other list is bounded.
func listLimit(c *gin.Context) int {
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil || limit <= 0 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}
