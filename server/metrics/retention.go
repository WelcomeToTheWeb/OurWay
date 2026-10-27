package metrics

import (
	"context"
	"log"
	"sync"
	"time"

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

// RetentionCounters reports what the sweeper last did, for the metrics
// surface and the ledger page: rows pruned per table, when it last ran, and
// whether it errored.
type RetentionCounters struct {
	MetricDeleted int64
	AuditDeleted  int64
	LastRunAt     time.Time
	Errored       bool
	RetentionDays int
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
		MetricDeleted: m.metricDeleted,
		AuditDeleted:  m.auditDeleted,
		LastRunAt:     m.lastRunAt,
		Errored:       m.lastRunErrored,
		RetentionDays: int(m.audit.Window / (24 * time.Hour)),
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
}
