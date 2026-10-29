package store

import (
	"testing"

	"ourway/server/models"

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

// TestSAMLColumnsLandOnExistingProviderRows is 2.4-7's upgrade question: a 2.4.3
// installation has provider rows that log people in today, and the SAML columns must
// be added beside them without changing what those rows mean.
//
// The row below is written with the columns a provider had before SAML existed and
// nothing else, which is the state an upgrade meets: the new columns arrive, the row
// keeps its protocol, its identity contract and its discovery, and the one field
// SAML's own code reads (`nameid_format`) lands on the value SAML's default is
// rather than on an empty string no code path reads.
func TestSAMLColumnsLandOnExistingProviderRows(t *testing.T) {
	db := newMigrateDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if err := db.Exec(`INSERT INTO sso_providers (id, type, name, client_id, client_secret, issuer, link_policy)
		VALUES ('11111111-1111-1111-1111-111111110001', 'oidc', 'upgrade-corp', 'ourway', 'upgrade-secret',
		        'https://idp.upgrade.example', 'pending')`).Error; err != nil {
		t.Fatalf("seed the pre-SAML row: %v", err)
	}

	for _, column := range []string{
		"idp_entity_id", "idp_sso_url", "idp_logout_url", "idp_certificate",
		"sp_entity_id", "sp_cert_pem", "sp_key_pem", "require_response_signature",
		"nameid_format", "attr_email", "attr_username", "attr_name", "attr_groups",
		"verified_email_attribute",
	} {
		if !db.Migrator().HasColumn(&models.SSOProvider{}, column) {
			t.Errorf("migration 16 left the table without the %q column", column)
		}
	}

	var row struct {
		Type       string `gorm:"column:type"`
		LinkPolicy string `gorm:"column:link_policy"`
		NameID     string `gorm:"column:nameid_format"`
		Issuer     string `gorm:"column:issuer"`
	}
	if err := db.Table("sso_providers").Where("id = ?", "11111111-1111-1111-1111-111111110001").
		Select("type", "link_policy", "nameid_format", "issuer").First(&row).Error; err != nil {
		t.Fatalf("read the pre-SAML row: %v", err)
	}
	if row.Type != "oidc" {
		t.Errorf("the row's protocol changed to %q on upgrade", row.Type)
	}
	if row.LinkPolicy != "pending" {
		t.Errorf("the row's identity contract changed to %q on upgrade", row.LinkPolicy)
	}
	if row.NameID != "emailaddress" {
		t.Errorf("nameid_format is %q, want the value SAML's code treats as its default", row.NameID)
	}
	if row.Issuer != "https://idp.upgrade.example" {
		t.Errorf("the row's issuer was lost: %q", row.Issuer)
	}

	// The key material is stored on the row and is never part of the JSON the
	// console reads: the columns exist, and the model's tags keep them out.
	if !db.Migrator().HasColumn(&models.SSOProvider{}, "sp_key_pem") {
		t.Fatal("the SP signing key has no column: signed AuthnRequests cannot be configured")
	}
	var provider models.SSOProvider
	if err := db.First(&provider, "id = ?", "11111111-1111-1111-1111-111111110001").Error; err != nil {
		t.Fatalf("load the provider model: %v", err)
	}
	if provider.SPKeyPEM != "" || provider.IdpCertificate != "" {
		t.Fatal("a provider row created before SAML arrived with key material")
	}
}
