package store

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// MetricHistoryStore provides database access for metric history.
type MetricHistoryStore struct {
	db *gorm.DB
}

// NewMetricHistoryStore creates a new metric history store.
func NewMetricHistoryStore(db *gorm.DB) *MetricHistoryStore {
	return &MetricHistoryStore{db: db}
}

// Insert stores a single metrics snapshot.
func (s *MetricHistoryStore) Insert(m *models.MetricHistory) error {
	return s.db.Create(m).Error
}

// BatchInsert stores multiple metrics snapshots in a single transaction.
func (s *MetricHistoryStore) BatchInsert(metrics []models.MetricHistory) error {
	if len(metrics) == 0 {
		return nil
	}
	return s.db.Create(&metrics).Error
}

// QueryByDevice queries metric history for a device within a time range.
func (s *MetricHistoryStore) QueryByDevice(deviceID string, from, to time.Time) ([]models.MetricHistory, error) {
	var metrics []models.MetricHistory
	err := s.db.Where("device_id = ? AND timestamp >= ? AND timestamp <= ?", deviceID, from, to).
		Order("timestamp ASC").
		Limit(10000).
		Find(&metrics).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query metrics: %w", err)
	}
	return metrics, nil
}

// QueryLatestByDevice returns the most recent metrics for a device.
func (s *MetricHistoryStore) QueryLatestByDevice(deviceID string) (*models.MetricHistory, error) {
	var m models.MetricHistory
	err := s.db.Where("device_id = ?", deviceID).
		Order("timestamp DESC").
		First(&m).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("no metrics found for device %s", deviceID)
		}
		return nil, fmt.Errorf("failed to query latest metrics: %w", err)
	}
	return &m, nil
}

// DeleteOlderThan deletes metrics older than the specified time (retention policy).
func (s *MetricHistoryStore) DeleteOlderThan(before time.Time) (int64, error) {
	result := s.db.Where("timestamp < ?", before).Delete(&models.MetricHistory{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete old metrics: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// TotalCount returns the total number of stored metrics.
func (s *MetricHistoryStore) TotalCount() (int64, error) {
	var count int64
	if err := s.db.Model(&models.MetricHistory{}).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count metrics: %w", err)
	}
	return count, nil
}
