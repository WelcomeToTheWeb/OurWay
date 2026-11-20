package store

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/models"
)

// The ledger is queried by time window — the recording's input overlay asks for
// "the acts inside this session's window", retention asks for "everything older
// than the cutoff" — and both bounds are computed in UTC.
//
// SQLite has no datetime type, so `created_at >= ?` compares the two values as
// *text*. If a row's automatic CreatedAt is stamped in the server's local zone,
// the two sides serialize differently for the same instant, the text order stops
// matching the instant order, and the answer is silently zero rows. Postgres is
// immune because AutoMigrate gives those columns `timestamp with time zone` and
// stores the instant; CI is immune because its runners are UTC. So the only
// person who ever saw this was a developer whose machine had a timezone.
//
// These tests force the zone rather than trusting the runner's, because a test
// that only passes at UTC is not testing the query — it is testing the clock.

var serverZones = []*time.Location{
	time.UTC,
	time.FixedZone("America/New_York", -5*3600),
	time.FixedZone("Asia/Kathmandu", 5*3600+45*60), // a half-hour offset, for the formatting
	time.FixedZone("Pacific/Kiritimati", 14*3600),  // the far side of the date line
}

func TestGormConfigStampsUTCWhateverTheServerZone(t *testing.T) {
	for _, zone := range serverZones {
		t.Run(zone.String(), func(t *testing.T) {
			defer func(old *time.Location) { time.Local = old }(time.Local)
			time.Local = zone

			stamped := GormConfig().NowFunc()
			if stamped.Location() != time.UTC {
				t.Fatalf("GormConfig stamped %s in zone %s; a local-zone stamp becomes local-zone "+
					"text in SQLite, and no window query written in UTC will find it",
					stamped.Format(time.RFC3339Nano), stamped.Location())
			}
		})
	}
}

func TestAuditTimeWindowFindsRowsWhateverTheServerZone(t *testing.T) {
	for _, zone := range serverZones {
		t.Run(zone.String(), func(t *testing.T) {
			defer func(old *time.Location) { time.Local = old }(time.Local)
			time.Local = zone

			db, err := gorm.Open(sqlite.Open(":memory:"), GormConfig())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if err := Migrate(db); err != nil {
				t.Fatalf("migrate: %v", err)
			}

			const actor = "server-zone-actor"
			audit := NewAuditStore(db)
			if err := audit.Record(&models.AuditLog{
				Action:      models.AuditSessionInput,
				Outcome:     "ok",
				ActorUserID: actor,
				TargetType:  "device",
				TargetID:    "device-under-session",
			}); err != nil {
				t.Fatalf("record: %v", err)
			}

			// The window `RecordingHandler.Events` builds for a recording that has
			// just started: 30 s either side of now, in UTC.
			now := time.Now().UTC()
			from, until := now.Add(-30*time.Second), now.Add(30*time.Second)
			rows, err := audit.List(AuditFilter{ActorUserID: actor, Since: &from, Until: &until})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(rows) != 1 {
				var stored []models.AuditLog
				_ = db.Where("actor_user_id = ?", actor).Find(&stored).Error
				when := "no row at all"
				if len(stored) > 0 {
					when = stored[0].CreatedAt.Format(time.RFC3339Nano) + " (zone " + stored[0].CreatedAt.Location().String() + ")"
				}
				t.Fatalf("the ledger answered %d rows for a window holding one act written %s, bounded %s to %s. "+
					"In SQLite these bounds are compared as text, so a zone-local stamp is simply the wrong day.",
					len(rows), when, from.Format(time.RFC3339), until.Format(time.RFC3339))
			}
		})
	}
}

// Retention is the same comparison with the higher cost, since getting it wrong
// destroys the installation's record of acts.
//
// Stated honestly: this test passed **before** the change too. A row written now
// and a cutoff 24 h old are more than an offset apart in every zone, so the text
// order happens to match the instant order and the prune is right by luck. It is
// kept as a guard on the comparison, not as a reproduction of a failure — the
// rows near a cutoff are the ones at risk, and they are within an offset of it.
func TestAuditPruneHonoursTheCutoffWhateverTheServerZone(t *testing.T) {
	for _, zone := range serverZones {
		t.Run(zone.String(), func(t *testing.T) {
			defer func(old *time.Location) { time.Local = old }(time.Local)
			time.Local = zone

			db, err := gorm.Open(sqlite.Open(":memory:"), GormConfig())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if err := Migrate(db); err != nil {
				t.Fatalf("migrate: %v", err)
			}

			const actor = "server-zone-prune-actor"
			audit := NewAuditStore(db)
			if err := audit.Record(&models.AuditLog{
				Action: models.AuditSessionInput, Outcome: "ok", ActorUserID: actor,
			}); err != nil {
				t.Fatalf("record: %v", err)
			}

			pruned, err := audit.PruneOlderThan(time.Now().UTC().Add(-24 * time.Hour))
			if err != nil {
				t.Fatalf("prune: %v", err)
			}
			if pruned != 0 {
				t.Fatalf("retention deleted %d row(s) written this second; the cutoff was computed in UTC "+
					"and the row's stamp in local time, so what was compared was text", pruned)
			}
		})
	}
}
