package metrics

import (
	"context"
	"log"
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
		Hot:  7 * 24 * time.Hour,    // 7 days full resolution
		Warm: 30 * 24 * time.Hour,   // 30 days
		Cold: 365 * 24 * time.Hour, // 365 days
	}
}

// RetentionManager periodically enforces retention policies.
type RetentionManager struct {
	store     *store.MetricHistoryStore
	policy    RetentionPolicy
	ticker    *time.Ticker
	stopCh    chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewRetentionManager creates a new retention manager.
func NewRetentionManager(store *store.MetricHistoryStore, policy RetentionPolicy, checkInterval time.Duration) *RetentionManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &RetentionManager{
		store:     store,
		policy:    policy,
		ticker:    time.NewTicker(checkInterval),
		stopCh:    make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
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

// enforce applies the retention policy, deleting expired metrics.
func (m *RetentionManager) enforce() {
	cutoff := time.Now().Add(-m.policy.Cold)
	deleted, err := m.store.DeleteOlderThan(cutoff)
	if err != nil {
		log.Printf("metrics: failed to enforce retention: %v", err)
		return
	}
	if deleted > 0 {
		log.Printf("metrics: retention cleanup deleted %d old records (cutoff: %s)",
			deleted, cutoff.Format(time.RFC3339))
	}
}
