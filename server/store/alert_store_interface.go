package store

import (
	"ourway/server/models"
)

// AlertStoreInterface defines the interface for alert storage operations.
type AlertStoreInterface interface {
	Create(a *models.Alert) error
	List(deviceID *string, unresolvedOnly bool) ([]models.Alert, error)
	GetByID(id string) (*models.Alert, error)
	MarkResolved(id string) error
	Acknowledge(id, userID string) error
	Assign(id, userID string) error
}
