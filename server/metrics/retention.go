package metrics

import (
	"context"
	"log"
	"sync"
	"time"

	"ourway/server/recordings"
	"ourway/server/store"
)

// RetentionPolicy defines how long metrics are kept.
type RetentionPolicy struct {
	// Hot retention: full resolution data kept for this duration
	Hot time.Duration
	// Warm retention: data older than Hot but younger than Warm is kept
	Warm time.Duration
	// Cold retention: data older than Warm but younger than Cold is kept
	Cold time.Duration
}

// DefaultRetentionPolicy returns sensible default retention settings.
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		Hot:  7 * 24 * time.Hour,   // 7 days full resolution
		Warm: 30 * 24 * time.Hour,  // 30 days
		Cold: 365 * 24 * time.Hour, // 365 days
	}
}

// AuditRetentionPolicy is the audit ledger's window (2.3-6). It is a separate
// number from the metric tiers: the ledger is compliance data, its size is
// bounded by "how long can a client ask who touched my machines", and 2.3's
// plan states it as `AUDIT_RETENTION_DAYS` with a default of 90.
type AuditRetentionPolicy struct {
	// Store is the ledger. A nil store means the deployment prunes metrics
	// only, which is what a build without 2.3's migration does.
	Store *store.AuditStore
	// Window is how long rows are kept.
	Window time.Duration
}

// RetentionManager periodically enforces retention policies.
type RetentionManager struct {
	store  *store.MetricHistoryStore
	audit  AuditRetentionPolicy
	policy RetentionPolicy
	ticker *time.Ticker
	stopCh chan struct{}
	ctx    context.Context
	cancel context.CancelFunc

	// Counters, because M11's lesson is the shape of this whole type: a
	// cleanup nobody can observe is a cleanup that quietly stopped running.
	// The last run's outcome is kept so the metrics surface reports the
	// sweeper's state, not just that a goroutine exists.
	mu             sync.Mutex
	metricDeleted  int64
	auditDeleted   int64
	lastRunAt      time.Time
	lastRunErrored bool

	// recordingsSweep is 2.4-8's half of this job, and lastRecording is what it
	// reported: the plan's rule for the ledger applies to recordings too — one
	// supervised sweeper, one schedule, observable counters.
	recordingsSweep *recordings.Sweep
	lastRecording   recordings.SweepCounters
}

// NewRetentionManager creates a new retention manager.
func NewRetentionManager(store *store.MetricHistoryStore, policy RetentionPolicy, checkInterval time.Duration) *RetentionManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &RetentionManager{
		store:  store,
		policy: policy,
		ticker: time.NewTicker(checkInterval),
		stopCh: make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
}

// WithAudit attaches the audit ledger to the same sweep. The plan's rule is
// that the ledger's prune runs on **the same schedule as the metric
// history's**, in the manager that already exists, rather than as a second
// goroutine nobody supervises.
func (m *RetentionManager) WithAudit(audit AuditRetentionPolicy) *RetentionManager {
	m.audit = audit
	return m
}

// RecordingRetentionPolicy is 2.4-8's recording window, attached to the same
// sweep.
type RecordingRetentionPolicy struct {
	// Sweep is the recordings' job: close what the sessions left open, prune
	// what passed its retention date, and remove the files with the rows.
	Sweep *recordings.Sweep
}

// WithRecordings attaches 2.4-8's recording sweep to the same hourly job.
func (m *RetentionManager) WithRecordings(policy RecordingRetentionPolicy) *RetentionManager {
	m.recordingsSweep = policy.Sweep
	return m
}

// RetentionCounters reports what the sweeper last did, for the metrics
// surface and the ledger page: rows pruned per table, when it last ran, and
// whether it errored.
type RetentionCounters struct {
	MetricDeleted int64
	AuditDeleted  int64
	// RecordingsDeleted, RecordingFiles and RecordingsClosed are 2.4-8: recordings
	// pruned past their retention window, the files removed with them, and the
	// recordings the sweep had to close because the session was over and nothing
	// in this process was writing them. A recording nobody prunes is a recording
	// whose frames quietly outgrow the disk, which is M11's pattern exactly.
	RecordingsDeleted int64
	RecordingFiles    int
	RecordingsClosed  int
	LastRunAt         time.Time
	Errored           bool
	RetentionDays     int
}

// RetentionSummary implements the ledger's view of the sweeper: the same
// counters, in the shape the audit page reports.
func (m *RetentionManager) RetentionSummary() (rowsPruned int64, lastRun time.Time, errored bool) {
	c := m.Counters()
	return c.AuditDeleted, c.LastRunAt, c.Errored
}

// Counters returns the last sweep's counters.
func (m *RetentionManager) Counters() RetentionCounters {
	m.mu.Lock()
	defer m.mu.Unlock()
	return RetentionCounters{
		MetricDeleted:     m.metricDeleted,
		AuditDeleted:      m.auditDeleted,
		RecordingsDeleted: m.lastRecording.RecordingsDeleted,
		RecordingFiles:    m.lastRecording.FilesRemoved,
		RecordingsClosed:  m.lastRecording.RecordingsClosed,
		LastRunAt:         m.lastRunAt,
		Errored:           m.lastRunErrored,
		RetentionDays:     int(m.audit.Window / (24 * time.Hour)),
	}
}

// Start begins the periodic retention enforcement loop.
func (m *RetentionManager) Start() {
	log.Printf("metrics: retention manager started (hot=%v, warm=%v, cold=%v)",
		m.policy.Hot, m.policy.Warm, m.policy.Cold)

	go func() {
		for {
			select {
			case <-m.ticker.C:
				m.enforce()
			case <-m.stopCh:
				log.Println("metrics: retention manager stopped")
				return
			case <-m.ctx.Done():
				log.Println("metrics: retention manager context done")
				return
			}
		}
	}()
}

// Stop stops the retention manager.
func (m *RetentionManager) Stop() {
	m.ticker.Stop()
	close(m.stopCh)
	m.cancel()
}

// enforce applies the retention policy, deleting expired metrics and expired
// audit rows.
func (m *RetentionManager) enforce() {
	errored := false
	var auditPruned int64

	cutoff := time.Now().Add(-m.policy.Cold)
	deleted, err := m.store.DeleteOlderThan(cutoff)
	if err != nil {
		log.Printf("metrics: failed to enforce retention: %v", err)
		errored = true
	} else if deleted > 0 {
		log.Printf("metrics: retention cleanup deleted %d old records (cutoff: %s)",
			deleted, cutoff.Format(time.RFC3339))
	}

	// The ledger's prune shares the sweep. A failure here is logged and
	// counted, and never aborts the metric cleanup: the two tables are
	// independent, and a ledger that cannot be pruned must not turn into
	// metrics that are never pruned.
	if m.audit.Store != nil && m.audit.Window > 0 {
		auditCutoff := time.Now().Add(-m.audit.Window)
		pruned, auditErr := m.audit.Store.PruneOlderThan(auditCutoff)
		auditPruned = pruned
		if auditErr != nil {
			log.Printf("metrics: failed to prune the audit log: %v", auditErr)
			errored = true
		} else if pruned > 0 {
			log.Printf("metrics: audit log pruned %d rows past %d days (cutoff: %s)",
				pruned, int(m.audit.Window/(24*time.Hour)), auditCutoff.Format(time.RFC3339))
		}
	}

	m.mu.Lock()
	m.metricDeleted += deleted
	m.auditDeleted += auditPruned
	m.lastRunAt = time.Now()
	m.lastRunErrored = errored
	m.mu.Unlock()

	// 2.4-8: the recordings' turn, after the two tables above. The order is the
	// same rule as the ledger's: a recording that cannot be pruned must not turn
	// into metrics that are never pruned.
	if m.recordingsSweep != nil {
		result := m.recordingsSweep.Run()
		m.mu.Lock()
		m.lastRecording.RecordingsDeleted += result.RecordingsDeleted
		m.lastRecording.RecordingsClosed += result.RecordingsClosed
		m.lastRecording.FilesRemoved += result.FilesRemoved
		m.lastRecording.LastRunAt = result.LastRunAt
		if result.Errored {
			m.lastRunErrored = true
		}
		m.mu.Unlock()
	}
}
