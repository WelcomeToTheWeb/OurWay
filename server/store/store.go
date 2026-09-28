package store

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ourway/server/cache"
	"ourway/server/models"
)

// Store provides database access through separate repositories.
type Store struct {
	DB                *gorm.DB
	Cache             *cache.RedisClient
	Devices           DeviceStoreInterface
	Users             *UserStore
	Alerts            *AlertStore
	Roles             *RoleStore
	UserRoles         *UserRoleStore
	Sessions          *SessionStore
	SoftwareUpdates   *SoftwareUpdateStore
	PatchPolicies     *PatchPolicyStore
	PatchDeployments  *PatchDeploymentStore
	FileTransfers     *FileTransferStore
	SSOProviders      *SSOProviderStore
	Webhooks          *WebhookStore
	WebhookDeliveries *WebhookDeliveryStore
	APIKeys           *APIKeyStore
	MetricHistory     *MetricHistoryStore
	DeploymentResults *DeploymentResultStore
}

// NewWithDB creates a Store from an existing *gorm.DB instance,
// running migrations if needed. Useful for testing.
func NewWithDB(db *gorm.DB) (*Store, error) {
	if err := db.AutoMigrate(
		&models.Device{},
		&models.User{},
		&models.Alert{},
		&models.Role{},
		&models.UserRole{},
		&models.Session{},
		&models.SoftwareUpdate{},
		&models.PatchPolicy{},
		&models.PatchDeployment{},
		&models.FileTransfer{},
		&models.SSOProvider{},
		&models.Webhook{},
		&models.WebhookDelivery{},
		&models.APIKey{},
		&models.MetricHistory{},
		&models.DeploymentResult{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	st := &Store{
		DB:                db,
		Devices:           &DeviceStore{db: db},
		Users:             &UserStore{db: db},
		Alerts:            &AlertStore{db: db},
		Roles:             &RoleStore{db: db},
		UserRoles:         &UserRoleStore{db: db},
		Sessions:          &SessionStore{db: db},
		SoftwareUpdates:   &SoftwareUpdateStore{db: db},
		PatchPolicies:     &PatchPolicyStore{db: db},
		PatchDeployments:  &PatchDeploymentStore{db: db},
		FileTransfers:     &FileTransferStore{db: db},
		SSOProviders:      &SSOProviderStore{db: db},
		Webhooks:          &WebhookStore{db: db},
		WebhookDeliveries: &WebhookDeliveryStore{db: db},
		APIKeys:           &APIKeyStore{db: db},
		MetricHistory:     NewMetricHistoryStore(db),
		DeploymentResults: NewDeploymentResultStore(db),
	}

	// Seed built-in roles
	if err := st.Roles.SeedBuiltinRoles(); err != nil {
		return nil, fmt.Errorf("failed to seed builtin roles: %w", err)
	}

	return st, nil
}

// New connects to the database, runs migrations, and returns a Store.
func New(dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool for production workloads
	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.SetMaxOpenConns(100)
		sqlDB.SetMaxIdleConns(20)
		sqlDB.SetConnMaxLifetime(30 * time.Minute)
		sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	}

	if err := db.AutoMigrate(
		&models.Device{},
		&models.User{},
		&models.Alert{},
		&models.Role{},
		&models.UserRole{},
		&models.Session{},
		&models.SoftwareUpdate{},
		&models.PatchPolicy{},
		&models.PatchDeployment{},
		&models.FileTransfer{},
		&models.SSOProvider{},
		&models.Webhook{},
		&models.WebhookDelivery{},
		&models.APIKey{},
		&models.MetricHistory{},
		&models.DeploymentResult{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	st := &Store{
		DB:                db,
		Devices:           &DeviceStore{db: db},
		Users:             &UserStore{db: db},
		Alerts:            &AlertStore{db: db},
		Roles:             &RoleStore{db: db},
		UserRoles:         &UserRoleStore{db: db},
		Sessions:          &SessionStore{db: db},
		SoftwareUpdates:   &SoftwareUpdateStore{db: db},
		PatchPolicies:     &PatchPolicyStore{db: db},
		PatchDeployments:  &PatchDeploymentStore{db: db},
		FileTransfers:     &FileTransferStore{db: db},
		SSOProviders:      &SSOProviderStore{db: db},
		Webhooks:          &WebhookStore{db: db},
		WebhookDeliveries: &WebhookDeliveryStore{db: db},
		APIKeys:           &APIKeyStore{db: db},
		MetricHistory:     NewMetricHistoryStore(db),
		DeploymentResults: NewDeploymentResultStore(db),
	}

	// Seed built-in roles
	if err := st.Roles.SeedBuiltinRoles(); err != nil {
		return nil, fmt.Errorf("failed to seed builtin roles: %w", err)
	}

	return st, nil
}
