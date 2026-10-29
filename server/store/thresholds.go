package store

import (
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/models"
)

// ThresholdStore holds the alert threshold rules (2.4-5).
//
// Reading is a cache read, not a database read, on the hot path: `Resolve` is
// called per metric per metrics report, and a fleet of agents posts metrics
// continuously. The snapshot is rebuilt on demand and invalidated by an explicit
// `Invalidate` after a rule is written, so a saved rule takes effect at once and
// a rule written by another process appears within the TTL.
type ThresholdStore struct {
	db *gorm.DB

	// snapshot is the parsed rule set, ordered for resolution.
	snapshot   []models.AlertThreshold
	snapshotAt time.Time
	ttl        time.Duration
}

// NewThresholdStore creates the rule store.
func NewThresholdStore(db *gorm.DB) *ThresholdStore {
	return &ThresholdStore{db: db, ttl: 15 * time.Second}
}

const (
	// MetricDefaults are the built-in thresholds — the numbers `v2.3.x` alerted
	// with, and the answer for every device that no rule covers.
	//
	// They live here so the engine's floor and the API's "what applies here"
	// preview read the same constants.
	DefaultWarningCPU   = 80.0
	DefaultCriticalCPU  = 90.0
	DefaultWarningRAM   = 85.0
	DefaultCriticalRAM  = 95.0
	DefaultWarningDisk  = 85.0
	DefaultCriticalDisk = 95.0
)

// DefaultThresholds returns the built-in per-metric defaults.
func DefaultThresholds() map[string]models.ThresholdPair {
	return map[string]models.ThresholdPair{
		models.MetricCPU:  {Warning: DefaultWarningCPU, Critical: DefaultCriticalCPU},
		models.MetricRAM:  {Warning: DefaultWarningRAM, Critical: DefaultCriticalRAM},
		models.MetricDisk: {Warning: DefaultWarningDisk, Critical: DefaultCriticalDisk},
	}
}

// Create stores a new rule.
func (s *ThresholdStore) Create(t *models.AlertThreshold) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.ScopeValue = normalizeScopeValue(t.ScopeType, t.ScopeValue)
	// `Select("*")` on purpose: the column carries `default:true`, and GORM
	// substitutes a column default for a zero-valued field on insert. Without it,
	// a rule created with `enabled: false` was stored **enabled** — a rule the
	// operator disabled, firing.
	if err := s.db.Select("*").Create(t).Error; err != nil {
		return err
	}
	s.Invalidate()
	return nil
}

// Update saves changes to an existing rule.
func (s *ThresholdStore) Update(t *models.AlertThreshold) error {
	t.ScopeValue = normalizeScopeValue(t.ScopeType, t.ScopeValue)
	if err := s.db.Save(t).Error; err != nil {
		return err
	}
	s.Invalidate()
	return nil
}

// GetByID reads one rule.
func (s *ThresholdStore) GetByID(id string) (*models.AlertThreshold, error) {
	var row models.AlertThreshold
	if err := s.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// Delete removes a rule.
func (s *ThresholdStore) Delete(id string) error {
	if err := s.db.Delete(&models.AlertThreshold{}, "id = ?", id).Error; err != nil {
		return err
	}
	s.Invalidate()
	return nil
}

// List returns every rule, most specific first, then metric.
func (s *ThresholdStore) List() ([]models.AlertThreshold, error) {
	var rows []models.AlertThreshold
	if err := s.db.Find(&rows).Error; err != nil {
		return nil, err
	}
	sortThresholds(rows)
	return rows, nil
}

// Invalidate drops the cached snapshot. The API calls it after every write, so a
// rule that was just saved is the rule the next metric is measured against.
func (s *ThresholdStore) Invalidate() {
	s.snapshot = nil
	s.snapshotAt = time.Time{}
}

// Rules returns the enabled rules, ordered most specific first.
func (s *ThresholdStore) Rules() ([]models.AlertThreshold, error) {
	if s.snapshot != nil && time.Since(s.snapshotAt) < s.ttl {
		return s.snapshot, nil
	}
	var rows []models.AlertThreshold
	if err := s.db.Where("enabled = ?", true).Find(&rows).Error; err != nil {
		return nil, err
	}
	sortThresholds(rows)
	s.snapshot = rows
	s.snapshotAt = time.Now()
	return rows, nil
}

// ResolvedThreshold is the answer for one metric on one device, with the chain
// that produced it — the operator's question is "why did this unit alert at 62?".
type ResolvedThreshold = models.ResolvedThreshold

// Resolve answers the thresholds for one device and metric: the most specific
// enabled rule that applies, per metric independently.
//
// Ties at the same scope level (two matching tag rules) are broken by the lowest
// warning, then the lowest critical, then the scope value — the same answer every
// time, which is what the "thresholds are deterministic" criterion needs.
func (s *ThresholdStore) Resolve(deviceID string, tags []string, clientID, metric string) (*models.ResolvedThreshold, error) {
	rules, err := s.Rules()
	if err != nil {
		return nil, err
	}
	tagSet := map[string]bool{}
	for _, tag := range tags {
		tagSet[normalizeScopeValue(models.ScopeTag, tag)] = true
	}

	var best *models.AlertThreshold
	for i := range rules {
		rule := &rules[i]
		if rule.Metric != metric {
			continue
		}
		if !ruleApplies(rule, deviceID, tagSet, clientID) {
			continue
		}
		if best == nil || moreSpecific(rule, best) {
			found := rule
			best = found
		}
	}

	defaults := DefaultThresholds()
	pair := defaults[metric]
	scope, value, source := models.ScopeGlobal, "", "built-in default"
	if best != nil {
		pair = models.ThresholdPair{Warning: best.Warning, Critical: best.Critical}
		scope, value = best.ScopeType, best.ScopeValue
		source = "rule"
	}
	return &models.ResolvedThreshold{
		Metric:     metric,
		Warning:    pair.Warning,
		Critical:   pair.Critical,
		ScopeType:  scope,
		ScopeValue: value,
		Source:     source,
		DedupSeconds: func() int {
			if best != nil {
				return best.DedupWindowSeconds
			}
			return 0
		}(),
	}, nil
}

// ruleApplies is the scope test. A rule applies to a device when its scope names
// this device, one of this device's tags, or this device's tenant.
func ruleApplies(rule *models.AlertThreshold, deviceID string, tagSet map[string]bool, clientID string) bool {
	switch rule.ScopeType {
	case models.ScopeDevice:
		return rule.ScopeValue == deviceID
	case models.ScopeTag:
		return tagSet[normalizeScopeValue(models.ScopeTag, rule.ScopeValue)]
	case models.ScopeClient:
		return clientID != "" && rule.ScopeValue == clientID
	case models.ScopeGlobal:
		return true
	default:
		return false
	}
}

// moreSpecific reports whether a beats b. Specificity first; then the deterministic
// tie-break.
// RuleWins reports which of two rules takes precedence. Exported for the API's
// precedence preview: the chain an operator reads must be ordered by the same
// rule the resolver applies, not by a copy of it written where it cannot be seen
// to drift.
//
// Most specific scope wins. Two rules of the same scope are decided
// deterministically — the more sensitive pair first, then the scope value — so a
// unit covered by two tag rules gets the same answer on every metric post.
func RuleWins(a, b *models.AlertThreshold) bool { return moreSpecific(a, b) }

func moreSpecific(a, b *models.AlertThreshold) bool {
	rankA, rankB := models.ScopeRank(a.ScopeType), models.ScopeRank(b.ScopeType)
	if rankA != rankB {
		return rankA > rankB
	}
	if a.Warning != b.Warning {
		return a.Warning < b.Warning
	}
	if a.Critical != b.Critical {
		return a.Critical < b.Critical
	}
	return a.ScopeValue < b.ScopeValue
}

func sortThresholds(rows []models.AlertThreshold) {
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := models.ScopeRank(rows[i].ScopeType), models.ScopeRank(rows[j].ScopeType)
		if ri != rj {
			return ri > rj
		}
		if rows[i].Metric != rows[j].Metric {
			return rows[i].Metric < rows[j].Metric
		}
		if rows[i].Warning != rows[j].Warning {
			return rows[i].Warning < rows[j].Warning
		}
		return rows[i].ScopeValue < rows[j].ScopeValue
	})
}

// normalizeScopeValue keeps a tag rule and a tag on a device comparable: tags are
// stored normalized, and a rule naming "Prod " must match the tag "prod".
func normalizeScopeValue(scopeType, value string) string {
	if scopeType != models.ScopeTag {
		return value
	}
	tags := models.NormalizeTags([]string{value})
	if len(tags) == 0 {
		return ""
	}
	return tags[0]
}
