package store

import (
	"ourway/server/models"
)

// DeviceStoreInterface defines the interface for device storage operations.
type DeviceStoreInterface interface {
	Create(d *models.Device) error
	GetByID(id string) (*models.Device, error)
	GetByKey(key string) (*models.Device, error)
	GetByHostnameAndIP(hostname, privateIP string) (*models.Device, error)
	ListAll() ([]models.Device, error)
	// ListScoped is the tenancy read (2.3-0, used by 2.3-2): devices whose
	// `client_id` is in `clientIDs`, plus the unassigned bucket when
	// `includeUnassigned` is set.
	//
	// Scoping is a query, never a post-filter of `ListAll`: filtering in the
	// handler costs the whole fleet per request and leaves every leak in the
	// SQL (2.3-2's acceptance criterion is that the client filter is in the
	// store's WHERE). An empty `clientIDs` with `includeUnassigned` false is a
	// caller with no scope, and the answer is no devices — not "no filter".
	ListScoped(clientIDs []string, includeUnassigned bool) ([]models.Device, error)
	Update(d *models.Device) error
	UpdateLastSeen(id string) error
	UpdateCapabilities(id, inputCapability, captureState string) error
	Delete(id string) error
}
