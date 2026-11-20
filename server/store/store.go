package store

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ourway/server/cache"
)

// Store provides database access through separate repositories.
type Store struct {
	DB      *gorm.DB
	Cache   *cache.RedisClient
	Devices DeviceStoreInterface
	Clients ClientStoreInterface
	// ClientTokens is 2.3-4c: a tenant's enrollment credentials. The agent-facing
	// registration path reads it by hash; only the admin surface writes it.
	ClientTokens *ClientTokenStore
	// Audit is 2.3-6: the record of mutating acts, written by the handlers.
	Audit             *AuditStore
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
	QueuedDeploys     *QueuedDeployStore
	// Thresholds is 2.4-5: the alert rule set, with device → tag → client →
	// global precedence.
	Thresholds *ThresholdStore
	// Recordings is 2.4-8: the manifest rows of recorded remote-control sessions.
	// The frames are a file on disk; this is what states where it is, how big it is,
	// and why it exists at all.
	Recordings *RecordingStore
}

// NewWithDB creates a Store from an existing *gorm.DB instance,
// running migrations if needed. Useful for testing.
func NewWithDB(db *gorm.DB) (*Store, error) {
	if err := Migrate(db); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	st := &Store{
		DB:                db,
		Devices:           &DeviceStore{db: db},
		Clients:           &ClientStore{db: db},
		ClientTokens:      &ClientTokenStore{db: db},
		Audit:             &AuditStore{db: db},
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
		QueuedDeploys:     &QueuedDeployStore{db: db},
		Thresholds:        NewThresholdStore(db),
		Recordings:        &RecordingStore{db: db},
	}

	// Seed built-in roles
	if err := st.Roles.SeedBuiltinRoles(); err != nil {
		return nil, fmt.Errorf("failed to seed builtin roles: %w", err)
	}

	return st, nil
}

// GormConfig is the GORM configuration every open in this product should use.
//
// `NowFunc` is not cosmetic. GORM fills a model's automatic CreatedAt/UpdatedAt
// from it, and the default is `time.Now()` — the **server's local zone**. The
// rest of the product already stamps UTC by hand (`time.Now().UTC()` at every
// write site that sets a timestamp itself), which leaves GORM's automatic fields
// as the single place a local-zone time gets into the database.
//
// On Postgres this is invisible, and that is why it survived: AutoMigrate makes
// those columns `timestamp with time zone`, so the driver stores the instant and
// both comparison and ordering are exact whatever zone the process ran in.
//
// On SQLite it is not invisible. SQLite has no datetime type: a timestamp is
// stored as the text it serialized to, and `created_at >= ?` is therefore a
// **text** comparison. A row written "2026-11-19 23:45:23-05:00" is textually
// earlier than the UTC bound "2026-11-20 04:44:53+00:00" even though it is the
// same instant, so a window query over that row answers **zero rows** — and a
// server east of UTC has it answer zero through the other end instead. Every CI
// runner is UTC, so the only person who sees it is a developer whose laptop is
// set to a real timezone, watching a red test against product code that is
// correct. Stamping UTC makes the test dialect agree with production semantics,
// rather than teaching the tests to expect a zone-dependent answer.
func GormConfig() *gorm.Config {
	return &gorm.Config{
		NowFunc: func() time.Time { return time.Now().UTC() },
	}
}

// New connects to the database, runs migrations, and returns a Store.
func New(dsn string) (*Store, error) {
	cfg := GormConfig()
	cfg.Logger = logger.Default.LogMode(logger.Silent)
	db, err := gorm.Open(postgres.Open(dsn), cfg)
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

	if err := Migrate(db); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	st := &Store{
		DB:                db,
		Devices:           &DeviceStore{db: db},
		Clients:           &ClientStore{db: db},
		ClientTokens:      &ClientTokenStore{db: db},
		Audit:             &AuditStore{db: db},
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
		QueuedDeploys:     &QueuedDeployStore{db: db},
		Thresholds:        NewThresholdStore(db),
		Recordings:        &RecordingStore{db: db},
	}

	// Seed built-in roles
	if err := st.Roles.SeedBuiltinRoles(); err != nil {
		return nil, fmt.Errorf("failed to seed builtin roles: %w", err)
	}

	return st, nil
}
