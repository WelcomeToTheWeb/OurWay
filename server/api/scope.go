package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"ourway/server/models"
	"ourway/server/store"
)

// 2.3-1 — the one place that answers "may this caller see it".
//
// Two properties the whole release depends on, and neither is optional:
//
//  1. **Scope is read from the user's rows on every request**, never from a
//     token claim. An access token is valid until it expires; revoking a
//     technician's client has to take effect on their *next request*, so the
//     answer must come from the database that can be changed under a live
//     token.
//  2. **Out of scope answers 404 with the identical body a nonexistent ID
//     gets.** Status and bytes, not just status. A 403 — or a 404 with a
//     "not authorized for this client" body — tells the caller that the ID
//     exists, which is a tenant directory handed to the people the tenancy is
//     built to keep out.
//
// Admin is scope-all **by role** and never reads the junction: an admin with
// scope rows would be a scope that says "these clients", and the console would
// have to guess what the rows mean. A non-admin with no rows sees nothing,
// **including unassigned devices** (2.3-7 states that plainly: unassigned units
// are invisible to non-admins until they are assigned) — "no rows granted" must
// never read as "sees everything".

// clientScope is a caller's view of the fleet.
type clientScope struct {
	all bool
	ids []string
}

// ErrScopeIdentity is the resolver's answer when a route asks about scope
// before an identity is on the context — a route wired without the auth
// middleware, which must fail loudly rather than default to the widest view.
var ErrScopeIdentity = errors.New("no authenticated user on this request")

// ClientScope resolves the caller on this request.
//
// The store read is deliberate: it is the request's one scope read (2.3-8's
// rule that resolution costs one query per request, not one per check), so
// handlers resolve once and pass the result to the checks below.
func ClientScope(c *gin.Context, st *store.Store) (clientScope, error) {
	if hasRole(c, RoleAdmin) {
		return clientScope{all: true}, nil
	}
	userID := CallerUserID(c)
	if userID == "" {
		return clientScope{}, ErrScopeIdentity
	}
	ids, err := st.Clients.ClientIDsForUser(userID)
	if err != nil {
		return clientScope{}, err
	}
	return clientScope{ids: ids}, nil
}

// CallerUserID reads the identity the auth middleware put on the context.
// Exported because 2.3-6 (audit) needs the same single definition of "who is
// this request": two ways of reading the actor is two ways to log the wrong one.
func CallerUserID(c *gin.Context) string {
	v, exists := c.Get("user_id")
	if !exists {
		return ""
	}
	uid, _ := v.(string)
	return uid
}

// AllowsClient answers for a device's client. `nil` is unassigned, and
// unassigned belongs to admin alone.
func (s clientScope) AllowsClient(clientID *string) bool {
	if s.all {
		return true
	}
	if clientID == nil {
		return false
	}
	for _, id := range s.ids {
		if id == *clientID {
			return true
		}
	}
	return false
}

// AllowsDevice is the check a by-ID route runs after it has the device row.
func (s clientScope) AllowsDevice(d *models.Device) bool {
	if d == nil {
		return false
	}
	return s.AllowsClient(d.ClientID)
}

// ClientIDs is the caller's client set, for store-level list scoping.
func (s clientScope) ClientIDs() []string { return s.ids }

// All reports the unrestricted scope: the admin's answer, and the one place a
// reader asks "am I seeing the whole installation".
func (s clientScope) All() bool { return s.all }

// VisibleDeviceIDs answers "which units may this caller see" for the entities
// that reference a unit — alerts, detected updates, transfers, sessions. Those
// tables carry a `device_id` and no client of their own, so scoping them is
// membership in the caller's unit set, and this is the one store read that
// answers it for the request.
//
// `restricted` is the useful half: for admin it is false and callers keep their
// current unrestricted read, so scope costs no query at all for the role that
// needs none. For a scoped caller it costs exactly one, whatever the number of
// entities the response contains (2.3-8's rule).
func (s clientScope) VisibleDeviceIDs(st *store.Store) (ids []string, restricted bool, err error) {
	if s.all {
		return nil, false, nil
	}
	devices, err := st.Devices.ListScoped(s.ids, s.IncludeUnassigned())
	if err != nil {
		return nil, true, err
	}
	ids = make([]string, 0, len(devices))
	for _, d := range devices {
		ids = append(ids, d.ID)
	}
	return ids, true, nil
}

// AllowsDeviceID checks an entity's unit. `restricted` callers must name a unit
// they can see; admin passes. An entity whose device is missing is out of scope
// for a restricted caller and visible to admin, which is what an orphaned row
// means for each role.
func (s clientScope) AllowsDeviceID(deviceID string, st *store.Store) (bool, error) {
	if s.all {
		return true, nil
	}
	ids, _, err := s.VisibleDeviceIDs(st)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == deviceID {
			return true, nil
		}
	}
	return false, nil
}

// IncludeUnassigned is always false for a scoped caller: a client grant is a
// grant over that client's units, not over the pool nobody has assigned. Admin
// reads the fleet through `ListAll`, so this never applies to them.
func (s clientScope) IncludeUnassigned() bool { return false }

// HeaderValue is the `X-OurWay-Client-Scope` value: the client ids the caller's
// scope names, sorted and comma-joined, or `all`.
//
// The header exists because "the technician cannot find the unit" is the support
// ticket this release is measured by. A console (and a support engineer reading
// a HAR) should see the scope the server answered with, on the response that
// omits the unit.
func (s clientScope) HeaderValue() string {
	if s.all {
		return "all"
	}
	if len(s.ids) == 0 {
		return "none"
	}
	ids := append([]string(nil), s.ids...)
	slices.Sort(ids)
	return strings.Join(ids, ",")
}

// RoleAdmin is the scope-all role. Named here because the resolver is the only
// place that decides who sees the whole installation, and a bare "admin"
// string in a condition is a role name that can drift from the seeded one.
const RoleAdmin = "admin"

// DeviceNotFound is the single 404 a device route answers with, for a device
// that does not exist *and* for one the caller may not see. Named as a function
// so "the same body" is a fact of the code and not a convention: a handler that
// writes its own "not authorized" text is a leak, and this is the only door.
func DeviceNotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
}

// ScopeError answers a request whose scope could not be resolved — 403, with a
// body that names the server's problem and no fact about the fleet. Scope
// resolution failing must not read as "see everything".
func ScopeError(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"error": "caller scope unavailable"})
}

// ScopedDevice is the door every by-device route of every kind walks through:
// resolve the caller, load the device, answer 404 identically for absent and
// out-of-scope, state the scope on the response. `ok` is false once a response
// has been written, so the caller returns.
//
// One door is the point. Alerts, sessions, updates, files, tags, streams and
// reboots each take a device id, and a leak in any one of them is the leak the
// release is measured by — so they share this, and 2.3-2's test matrix runs the
// same byte-comparison over all of them.
func ScopedDevice(c *gin.Context, st *store.Store) (*models.Device, bool) {
	device, err := st.Devices.GetByID(c.Param("id"))
	if err != nil {
		DeviceNotFound(c)
		return nil, false
	}
	scope, err := ClientScope(c, st)
	if err != nil {
		ScopeError(c)
		return nil, false
	}
	scope.ApplyScopeHeader(c)
	if !scope.AllowsDevice(device) {
		DeviceNotFound(c)
		return nil, false
	}
	return device, true
}

// ApplyScopeHeader states the scope on the response. Called by every scoped
// read, so the console never has to infer a scope from what is missing.
func (s clientScope) ApplyScopeHeader(c *gin.Context) {
	c.Header("X-OurWay-Client-Scope", s.HeaderValue())
}
