package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"ourway/server/models"
)

// baseModels is the full model set making up the baseline schema. Only
// migration 001 (the baseline) may use AutoMigrate; every later migration
// must be a versioned, explicit change so production schemas evolve
// reproducibly instead of diverging silently.
var baseModels = []any{
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
}

// migration is one versioned, ordered schema change. Version numbers are
// contiguous and never reused; a migration, once shipped, is never edited.
type migration struct {
	version int64
	name    string
	fn      func(*gorm.DB) error
}

// appliedMigration is a row in schema_migrations.
type appliedMigration struct {
	Version   int64     `gorm:"primaryKey"`
	Name      string    `gorm:"not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func (appliedMigration) TableName() string { return "schema_migrations" }

// migrations is the ordered, append-only list of schema changes. v001 is
// the AutoMigrate baseline: it is idempotent, so installs whose schema was
// created by the pre-versioning AutoMigrate converge onto the same
// baseline and are recorded as applied without any data change.
var migrations = []migration{
	{version: 1, name: "baseline_auto_migrate", fn: func(db *gorm.DB) error {
		return db.AutoMigrate(baseModels...)
	}},
	{version: 2, name: "sessions_remote_token_hash", fn: func(db *gorm.DB) error {
		if db.Migrator().HasColumn(&models.Session{}, "remote_token_hash") {
			return nil
		}
		return db.Migrator().AddColumn(&models.Session{}, "RemoteTokenHash")
	}},
	{version: 3, name: "patch_update_metadata_and_reboot_pending", fn: func(db *gorm.DB) error {
		m := db.Migrator()
		for _, col := range []string{"ExternalID", "KB", "Severity", "Category", "DeploymentID"} {
			if !m.HasColumn(&models.SoftwareUpdate{}, col) {
				if err := m.AddColumn(&models.SoftwareUpdate{}, col); err != nil {
					return err
				}
			}
		}
		if !m.HasColumn(&models.Device{}, "RebootPending") {
			return m.AddColumn(&models.Device{}, "RebootPending")
		}
		return nil
	}},
	{version: 4, name: "devices_tags", fn: func(db *gorm.DB) error {
		if db.Migrator().HasColumn(&models.Device{}, "Tags") {
			return nil
		}
		return db.Migrator().AddColumn(&models.Device{}, "Tags")
	}},
	{version: 5, name: "maintenance_windows_and_deploy_queue", fn: func(db *gorm.DB) error {
		m := db.Migrator()
		for _, col := range []string{"WindowStart", "WindowHours", "Timezone", "LastRunAt"} {
			if !m.HasColumn(&models.PatchPolicy{}, col) {
				if err := m.AddColumn(&models.PatchPolicy{}, col); err != nil {
					return err
				}
			}
		}
		if !m.HasColumn(&models.PatchDeployment{}, "AutoReboot") {
			if err := m.AddColumn(&models.PatchDeployment{}, "AutoReboot"); err != nil {
				return err
			}
		}
		// Manual deployments have no policy; the original NOT NULL uuid
		// column rejected them on PostgreSQL. (SQLite cannot alter a
		// column, but only ever sees fresh schemas built from the model.)
		if db.Dialector.Name() == "postgres" {
			if err := db.Exec("ALTER TABLE patch_deployments ALTER COLUMN policy_id DROP NOT NULL").Error; err != nil {
				return err
			}
		}
		if !m.HasTable(&models.QueuedDeploy{}) {
			return m.CreateTable(&models.QueuedDeploy{})
		}
		return nil
	}},
	{version: 6, name: "sessions_viewer_token_hash", fn: func(db *gorm.DB) error {
		if db.Migrator().HasColumn(&models.Session{}, "viewer_token_hash") {
			return nil
		}
		return db.Migrator().AddColumn(&models.Session{}, "ViewerTokenHash")
	}},
	{version: 7, name: "devices_input_and_capture_capability", fn: func(db *gorm.DB) error {
		// 2.2-1: the agent reports whether remote input can land on it and
		// whether capture is producing frames. Additive: older agents send
		// neither, and the stored "unknown" is what the console shows.
		for _, col := range []struct{ model, field string }{
			{"devices", "input_capability"},
			{"devices", "capture_state"},
		} {
			if db.Migrator().HasColumn(col.model, col.field) {
				continue
			}
			if err := db.Migrator().AddColumn(&models.Device{}, col.field); err != nil {
				return err
			}
		}
		return nil
	}},
	{version: 8, name: "clients_and_user_client_scope", fn: func(db *gorm.DB) error {
		// 2.3-0: the tenant axis. Two new tables and one nullable column — no
		// data rewrite, because a pre-2.3 device has no client and `NULL` is
		// that fact (2.3-7's upgrade test asserts exactly this: every device
		// survives, every device reads as unassigned).
		if !db.Migrator().HasTable(&models.Client{}) {
			if err := db.Migrator().CreateTable(&models.Client{}); err != nil {
				return err
			}
		}
		if !db.Migrator().HasTable(&models.UserClient{}) {
			if err := db.Migrator().CreateTable(&models.UserClient{}); err != nil {
				return err
			}
		}
		if !db.Migrator().HasColumn(&models.Device{}, "client_id") {
			if err := db.Migrator().AddColumn(&models.Device{}, "ClientID"); err != nil {
				return err
			}
		}
		// The index is created here as well as declared on the model: on an
		// existing 2.2 table, AddColumn does not build model indexes, and every
		// scoped read filters on this column.
		if !db.Migrator().HasIndex(&models.Device{}, "client_id") {
			if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_devices_client_id ON devices (client_id)").Error; err != nil {
				return err
			}
		}
		// The plan names `client_id` a foreign key, so Postgres enforces it:
		// a device pointing at a client that does not exist is a row no scope
		// query can ever return. `ClientStore.Delete` nulls the devices first,
		// so the default RESTRICT never blocks the deletion it must not block.
		if db.Dialector.Name() == "postgres" {
			var exists bool
			if err := db.Raw(
				"SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_devices_client')",
			).Row().Scan(&exists); err != nil {
				return err
			}
			if !exists {
				if err := db.Exec("ALTER TABLE devices ADD CONSTRAINT fk_devices_client FOREIGN KEY (client_id) REFERENCES clients(id)").Error; err != nil {
					return err
				}
			}
		}
		return nil
	}},
	{version: 9, name: "audit_log", fn: func(db *gorm.DB) error {
		// 2.3-6: the record of mutating acts. docs/phase1-tasks.md lists "Audit
		// log for user actions" as complete; there was no table, no writer and
		// no UI. Migration numbers are append-only, so it lands as 9.
		//
		// The two composite indexes are the queries the ledger answers: a
		// manager reading their clients' rows newest-first, and an operator
		// asking "every failed login, this week". A single-column index on
		// `created_at` serves neither: both filters are selective together.
		if !db.Migrator().HasTable(&models.AuditLog{}) {
			if err := db.Migrator().CreateTable(&models.AuditLog{}); err != nil {
				return err
			}
		}
		if db.Dialector.Name() == "postgres" {
			if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_audit_log_client_created ON audit_log (client_id, created_at)").Error; err != nil {
				return err
			}
			if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_audit_log_action_created ON audit_log (action, created_at)").Error; err != nil {
				return err
			}
		} else if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
			return err
		}
		return nil
	}},
	{version: 10, name: "tenant_scoped_read_indexes", fn: func(db *gorm.DB) error {
		// 2.3-8: the indexes the scoped reads are shaped for.
		//
		// Scoping an entity list is a semi-join — `device_id IN (SELECT id
		// FROM devices WHERE client_id IN (…))` — and a semi-join is only
		// cheap if both sides can be walked: the planner needs
		// `idx_devices_client_id` (migration 8) on the fleet side, and a
		// composite index on the entity side, because every one of these
		// reads filters by unit *and* orders by recency. A single-column
		// index on `created_at` serves neither half.
		//
		// `software_updates` is ordered by status, not time: a patch panel
		// asks "what is open on my units", which is the (device, status)
		// pair.
		for _, stmt := range []string{
			"CREATE INDEX IF NOT EXISTS idx_alerts_device_created ON alerts (device_id, created_at)",
			"CREATE INDEX IF NOT EXISTS idx_sessions_device_created ON sessions (device_id, created_at)",
			"CREATE INDEX IF NOT EXISTS idx_file_transfers_device_created ON file_transfers (device_id, created_at)",
			"CREATE INDEX IF NOT EXISTS idx_software_updates_device_status ON software_updates (device_id, status)",
		} {
			if err := db.Exec(stmt).Error; err != nil {
				return err
			}
		}
		return nil
	}},
	{version: 11, name: "client_enrollment_tokens", fn: func(db *gorm.DB) error {
		// 2.3-4c: a tenant's enrollment credential. The table stores the hash
		// only (see `models.ClientEnrollmentToken`), and the unique index on it
		// is the registration lookup — the agent's presented value is hashed and
		// this column is walked, so enrollment costs one indexed read and no scan
		// of credentials.
		if !db.Migrator().HasTable(&models.ClientEnrollmentToken{}) {
			if err := db.Migrator().CreateTable(&models.ClientEnrollmentToken{}); err != nil {
				return err
			}
		}
		// As in migration 8: a token names the tenant it enrolls into, and a
		// token pointing at a client that does not exist is a credential that
		// can only ever answer "invalid". `ClientTokenStore`-side cleanup runs
		// inside `ClientStore.Delete`, so the default RESTRICT never blocks the
		// deletion it must not block.
		if db.Dialector.Name() == "postgres" {
			var exists bool
			if err := db.Raw(
				"SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_client_enrollment_tokens_client')",
			).Row().Scan(&exists); err != nil {
				return err
			}
			if !exists {
				if err := db.Exec("ALTER TABLE client_enrollment_tokens ADD CONSTRAINT fk_client_enrollment_tokens_client FOREIGN KEY (client_id) REFERENCES clients(id)").Error; err != nil {
					return err
				}
			}
		}
		return nil
	}},
	{version: 12, name: "oidc_provider_discovery_and_identity_policy", fn: func(db *gorm.DB) error {
		// 2.4-1: a provider configured by **issuer**, with the endpoints the
		// provider publishes, plus the per-provider knobs the 2.4-3 identity
		// contract needs. Columns are added, never rebuilt: a 2.3 installation
		// has provider rows that log people in today, and a 2.4 upgrade that
		// dropped them would lock operators out of their own console.
		//
		// Defaults are what makes the upgrade safe, and they are not neutral:
		// `link_policy` defaults to `pending`, so a provider that exists before
		// the contract ships stops silently claiming local accounts by asserted
		// email the moment 2.4 is applied. An operator who wants the old behaviour
		// chooses it, per provider, in the UI.
		for _, col := range []struct{ name, field string }{
			{"issuer", "Issuer"},
			{"discovery_url", "DiscoveryURL"},
			{"jwks_uri", "JwksURI"},
			{"redirect_uri", "RedirectURI"},
			{"discovery_doc", "DiscoveryDoc"},
			{"discovered_at", "DiscoveredAt"},
			{"claims_email", "ClaimsEmail"},
			{"claims_username", "ClaimsUsername"},
			{"claims_name", "ClaimsName"},
			{"claims_groups", "ClaimsGroups"},
			{"allowed_domains", "AllowedDomains"},
			{"require_verified_email", "RequireVerifiedEmail"},
			{"link_policy", "LinkPolicy"},
			{"role_mapping", "RoleMapping"},
			{"sort_order", "SortOrder"},
			{"pkce_supported", "PKCESupported"},
		} {
			if !db.Migrator().HasColumn(&models.SSOProvider{}, col.name) {
				if err := db.Migrator().AddColumn(&models.SSOProvider{}, col.field); err != nil {
					return err
				}
			}
		}
		// `AddColumn` on an existing table does not apply the model's `default:`
		// to the rows already there (migration 8 met the same thing with the
		// device index). Existing providers must land on `pending`, not on an
		// empty string that no code path reads.
		if err := db.Exec("UPDATE sso_providers SET link_policy = 'pending' WHERE link_policy IS NULL OR link_policy = ''").Error; err != nil {
			return err
		}
		return nil
	}},
	{version: 13, name: "sso_pending_identities", fn: func(db *gorm.DB) error {
		// 2.4-3: the linking contract's state. A new provider/email pair whose email
		// belongs to an existing account becomes a **pending identity** — a row with no
		// roles that cannot log in, naming the account the assertion points at, until an
		// admin approves, merges, or refuses it.
		//
		// The columns live on `users` rather than in a side table because the record is
		// an account-shaped fact an operator acts on in the Users page, and a row there
		// cannot be forgotten the way a queue can.
		//
		// Numbering: the plan's 2.4 line said 13 was thresholds. Migrations are
		// append-only and contiguous, and the identity work landed first, so thresholds
		// are 14, channels 15, recordings 16 — `docs/phase2.4-tasks.md` says so.
		for _, col := range []struct{ name, field string }{
			{"status", "Status"},
			{"pending_provider", "PendingProvider"},
			{"pending_link_user_id", "PendingLinkUserID"},
		} {
			if !db.Migrator().HasColumn(&models.User{}, col.name) {
				if err := db.Migrator().AddColumn(&models.User{}, col.field); err != nil {
					return err
				}
			}
		}
		// Same lesson as migration 12: `AddColumn` does not apply the model's default to
		// the rows that already exist, and an empty status is a state no code path
		// reads. Every account that existed before the contract ships is active — the
		// contract changes what a *new* SSO assertion may do, not what already works.
		if err := db.Exec("UPDATE users SET status = 'active' WHERE status IS NULL OR status = ''").Error; err != nil {
			return err
		}
		return nil
	}},
}

// Migrate applies all pending schema migrations inside a transaction per
// migration, recording each in schema_migrations. It is safe to call
// repeatedly; already-applied versions are skipped.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&appliedMigration{}); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}
	for _, m := range migrations {
		applied, err := isApplied(db, m.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := m.fn(tx); err != nil {
				return fmt.Errorf("migration %d (%s) failed: %w", m.version, m.name, err)
			}
			return tx.Create(&appliedMigration{Version: m.version, Name: m.name, AppliedAt: time.Now().UTC()}).Error
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// isApplied reports whether the given migration version is recorded.
func isApplied(db *gorm.DB, version int64) (bool, error) {
	var row appliedMigration
	err := db.Where("version = ?", version).First(&row).Error
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("failed to read schema_migrations: %w", err)
}

// AppliedVersions returns the versions recorded in schema_migrations.
// Useful for diagnostics and tests.
func AppliedVersions(db *gorm.DB) ([]int64, error) {
	var rows []appliedMigration
	if err := db.Order("version ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Version)
	}
	return out, nil
}
