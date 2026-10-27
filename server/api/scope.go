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

// ApplyScopeHeader states the scope on the response. Called by every scoped
// read, so the console never has to infer a scope from what is missing.
func (s clientScope) ApplyScopeHeader(c *gin.Context) {
	c.Header("X-OurWay-Client-Scope", s.HeaderValue())
}
