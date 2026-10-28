package store

import (
	"gorm.io/gorm"

	"ourway/server/models"
)

// 2.3-8 — the scope is a WHERE clause, not a list of ids.
//
// An alert, a detected update, a session and a file transfer all name a
// `device_id` and no client. The obvious scoping is "resolve the caller's
// visible units, then `device_id IN (…)`: correct, and it is what 2.3-2 shipped.
// At MSP scale it is the wrong shape twice over — a tenant of 2,000 units turns
// one list request into a statement with 2,000 bind parameters, and the fleet
// is read in full first to build that list.
//
// The subquery is the same rule stated once, in the place that can enforce it:
// `device_id IN (SELECT id FROM devices WHERE client_id IN (…))`. Two bind
// parameters whatever the tenant's size, the planner's job, and a plan the
// index on `devices.client_id` was built for (`TestPostgresScopedListUsesTheClientIndex`
// asks the planner, on Postgres, to prove it).

// visibleUnitIDs is the subquery a unit-bearing table scopes against: the ids of
// the units of these clients, plus the unassigned pool when `includeUnassigned`.
func visibleUnitIDs(db *gorm.DB, clientIDs []string, includeUnassigned bool) *gorm.DB {
	q := db.Model(&models.Device{}).Select("id")
	if includeUnassigned && len(clientIDs) == 0 {
		return q.Where("client_id IS NULL")
	}
	if includeUnassigned {
		return q.Where("client_id IN ? OR client_id IS NULL", clientIDs)
	}
	return q.Where("client_id IN ?", clientIDs)
}

// scopeByClient narrows `q` to rows whose `device_id` names a unit the caller's
// client set contains. `column` is the caller's unit column, because these
// tables do not agree on its name.
//
// An empty client set with no unassigned pool answers **no rows** — a caller
// with no scope has no fleet, and reading the whole table to trim it in the
// handler is both the leak and the cost this release exists to remove.
func scopeByClient(db *gorm.DB, q *gorm.DB, column string, clientIDs []string, includeUnassigned bool) (*gorm.DB, bool) {
	if len(clientIDs) == 0 && !includeUnassigned {
		return q, false
	}
	return q.Where(column+" IN (?)", visibleUnitIDs(db, clientIDs, includeUnassigned)), true
}
