package store

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newMigrateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory SQLite: %v", err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return db
}

func TestMigrateAppliesAllVersions(t *testing.T) {
	db := newMigrateDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}
	versions, err := AppliedVersions(db)
	if err != nil {
		t.Fatalf("AppliedVersions failed: %v", err)
	}
	if len(versions) != len(migrations) {
		t.Fatalf("expected %d applied versions, got %v", len(migrations), versions)
	}
	for i, m := range migrations {
		if versions[i] != m.version {
			t.Errorf("applied version %d = %d, want %d", i, versions[i], m.version)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := newMigrateDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate failed: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}
	versions, err := AppliedVersions(db)
	if err != nil {
		t.Fatalf("AppliedVersions failed: %v", err)
	}
	if len(versions) != len(migrations) {
		t.Errorf("expected %d versions after re-run, got %d", len(migrations), len(versions))
	}
}

func TestNewWithDBRecordsMigrations(t *testing.T) {
	db := newMigrateDB(t)
	st, err := NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	versions, err := AppliedVersions(db)
	if err != nil {
		t.Fatalf("AppliedVersions failed: %v", err)
	}
	if len(versions) == 0 {
		t.Fatal("expected NewWithDB to record applied migrations")
	}
	// The baseline must have created the core tables and seeded roles.
	roles, err := st.Roles.ListRoles()
	if err != nil {
		t.Fatalf("ListRoles failed: %v", err)
	}
	if len(roles) == 0 {
		t.Error("expected builtin roles to be seeded")
	}
}
