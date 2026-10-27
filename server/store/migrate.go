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
