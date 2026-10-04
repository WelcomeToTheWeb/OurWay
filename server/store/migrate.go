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
