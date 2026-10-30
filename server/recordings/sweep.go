package recordings

import (
	"log"
	"os"
	"time"

	"ourway/server/models"
	"ourway/server/store"
)

// Sweep is the recording half of the retention loop (2.4-8). It runs on the
// metric retention manager's schedule, in that manager — the 2.3-6 rule for the
// audit ledger, applied to a second table: one sweeper an operator can read,
// never a goroutine nobody supervises.
//
// It does two things, and the order is the point:
//
//  1. **Close** recordings that are open in the database but not live in this
//     process and belong to a session that has ended. A row that says
//     `recording` while nobody writes the file is a recording the console would
//     show as live forever.
//  2. **Prune** rows whose `retention_until` has passed, and remove the files
//     they name. The row goes first, so a recording is never offered with its
//     media gone.
type Sweep struct {
	// Store is the manifest rows.
	Store *store.RecordingStore
	// Sessions answers whether the session a recording belongs to is over.
	Sessions *store.SessionStore
	// Writer is asked before closing a row: a recording this process is
	// appending to is live, whatever the clock says.
	Writer *Writer
	// Window is RECORDING_RETENTION_DAYS.
	Window time.Duration
}

// NewSweep wires the sweep to the store, the writer, and the retention window
// (`RECORDING_RETENTION_DAYS`). The window is a parameter and not a guess: the
// number the operator reads in the console and the number the sweep prunes by
// come from one place.
func NewSweep(st *store.Store, writer *Writer, retention time.Duration) *Sweep {
	return &Sweep{Store: st.Recordings, Sessions: st.Sessions, Writer: writer, Window: retention}
}

// SweepCounters is what one sweep did, for the counters the console prints.
type SweepCounters struct {
	RecordingsDeleted int64
	RecordingsClosed  int
	FilesRemoved      int
	LastRunAt         time.Time
	Errored           bool
}

// Run performs one sweep. A nil policy (no recording store wired) does nothing,
// which is what an installation with 2.4-8's migration and no recording policy
// looks like.
func (s *Sweep) Run() SweepCounters {
	result := SweepCounters{LastRunAt: time.Now()}
	if s == nil || s.Store == nil {
		return result
	}

	s.closeAbandoned(&result)
	s.prune(&result)

	return result
}

// closeAbandoned ends recordings the database says are open, the session is
// over, and this process is not writing them.
func (s *Sweep) closeAbandoned(result *SweepCounters) {
	open, err := s.Store.ListOpen()
	if err != nil {
		log.Printf("recordings: sweep could not list open recordings: %v", err)
		result.Errored = true
		return
	}
	for i := range open {
		rec := open[i]
		if s.Writer != nil && s.Writer.RecordingIsLive(rec.ID) {
			continue
		}
		if s.Sessions != nil {
			session, err := s.Sessions.GetByID(rec.SessionID)
			if err == nil && session != nil && session.Status != "ended" && session.Status != "error" {
				// A live session whose recording is open, on a writer that is not
				// writing it, is the restart case: the next frame reopens the file.
				// Closing it here would end a recording the session is still
				// producing.
				continue
			}
		}
		stats := Stats{}
		if s.Writer != nil {
			if live, ok := s.Writer.Stats(rec.ID); ok {
				stats = live
			}
		}
		if err := s.Store.End(rec.ID, stats.Frames, stats.Bytes, rec.Checksum, rec.Interruptions, stats.Dropped); err != nil {
			log.Printf("recordings: sweep could not close recording %s: %v", rec.ID, err)
			result.Errored = true
			continue
		}
		result.RecordingsClosed++
	}
}

// prune removes the rows whose retention window closed, then their files.
func (s *Sweep) prune(result *SweepCounters) {
	if s.Window <= 0 {
		return
	}
	cutoff := time.Now().UTC()
	recs, err := s.Store.PruneOlderThan(cutoff)
	if err != nil {
		log.Printf("recordings: sweep could not prune recordings: %v", err)
		result.Errored = true
		return
	}
	if len(recs) == 0 {
		return
	}
	result.RecordingsDeleted = int64(len(recs))
	for i := range recs {
		path := recs[i].Path
		if path == "" && s.Writer != nil {
			path = s.Writer.PathOf(recs[i].ID)
		}
		if err := removeFile(path); err != nil {
			// The row is gone, so nothing offers the recording. A file nobody can
			// remove is stated: it is disk space the operator cannot account for.
			log.Printf("recordings: sweep could not remove %s: %v", path, err)
			result.Errored = true
			continue
		}
		result.FilesRemoved++
	}
	// The window is the retention length, so it is stated in days: the number the
	// console reads is the number that pruned.
	log.Printf("recordings: pruned %d recordings past their retention window of %d days (%d files removed, cutoff %s)",
		len(recs), int(s.Window/(24*time.Hour)), result.FilesRemoved, cutoff.Format(time.RFC3339))
}

func removeFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RecordingModeIsOff is the shorthand the wiring uses: an installation with no
// recording policy runs no sweep.
func RecordingModeIsOff(mode string) bool {
	return mode == models.RecordingModeOff
}
