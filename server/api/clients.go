package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ourway/server/models"
	"ourway/server/store"
)

// 2.3-4 — the client surface: CRUD, device assignment, per-client rollup, and
// the grants that decide who may see a client.
//
// Two rules the routes are built around:
//
//  **Deleting a client moves its devices to unassigned. It never deletes a
//  device.** A device is a machine with a key, metrics and a patch history;
//  losing a tenant must not lose its fleet, and the console's destructive copy
//  says so.
//
//  **A client outside the caller's scope answers 404 with the body a
//  nonexistent client gets** — the device rule, applied to the tenant axis. A
//  403 here would publish which client names exist in the installation, with
//  their slugs, to accounts that are not allowed to know them.
//
// Writes are admin-only. Managers read the clients they are granted, and the
// assignment of devices and technicians is an admin act: a manager moving a unit
// between clients moves it out of another manager's view, which is the act the
// tenancy exists to control.

// ClientHandler handles the client surface.
type ClientHandler struct {
	store *store.Store
}

// NewClientHandler builds the handler.
func NewClientHandler(st *store.Store) *ClientHandler {
	return &ClientHandler{store: st}
}

// ClientNotFound is the single 404 for a client route, for a client that does
// not exist and for one the caller is not granted. Same reasoning as
// `DeviceNotFound`: the body cannot distinguish them.
func ClientNotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "client not found"})
}

// scopedClient loads a client the caller is granted. `admin` sees every client;
// everyone else sees only the rows in `user_clients`.
func (h *ClientHandler) scopedClient(c *gin.Context) (*models.Client, bool) {
	// `ClientStore.GetByID` answers (nil, nil) for an absent client — the shape
	// the store documents for routes that answer 404 without mapping a gorm
	// error. Every client route here must check the pointer, not the error;
	// checking only the error is a nil dereference, and a nil dereference in a
	// handler is a 500 with an empty body on a route that should answer 404.
	client, err := h.store.Clients.GetByID(c.Param("id"))
	if err != nil || client == nil {
		ClientNotFound(c)
		return nil, false
	}
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return nil, false
	}
	scope.ApplyScopeHeader(c)
	if !scope.all && !scope.AllowsClient(&client.ID) {
		ClientNotFound(c)
		return nil, false
	}
	return client, true
}

// currentIDs renders a grant set for the record.
func currentIDs(users []models.User) []string {
	ids := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	return ids
}

// clientRequest is the writable shape of a client.
type clientRequest struct {
	Name  string `json:"name" binding:"required"`
	Slug  string `json:"slug"`
	Notes string `json:"notes"`
	// Disabled is settable so a tenant can be paused without deleting its
	// fleet: a cancelled client stops being a target for assignment while its
	// units keep their history.
	Disabled *bool `json:"disabled"`
}

// ListClients answers the clients the caller may see.
// GET /api/clients
func (h *ClientHandler) ListClients(c *gin.Context) {
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)

	all, err := h.store.Clients.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list clients"})
		return
	}

	clients := all
	if !scope.all {
		granted := make(map[string]bool, len(scope.ClientIDs()))
		for _, id := range scope.ClientIDs() {
			granted[id] = true
		}
		clients = make([]models.Client, 0, len(all))
		for _, cl := range all {
			if granted[cl.ID] {
				clients = append(clients, cl)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"clients": clients})
}

// CreateClient adds a client. Admin only.
// POST /api/clients
func (h *ClientHandler) CreateClient(c *gin.Context) {
	var req clientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	slug := req.Slug
	if slug == "" {
		slug = req.Name
	}
	client := &models.Client{
		ID:    models.NewClientID(),
		Name:  req.Name,
		Slug:  models.NormalizeSlug(slug),
		Notes: req.Notes,
	}
	if req.Disabled != nil {
		client.Disabled = *req.Disabled
	}

	// A client is a tenant: creating one is creating a boundary between
	// people's fleets, so it is recorded with the slug that becomes the scope.
	defer auditForAnswer(c, h.store, models.AuditClientCreate, AuditTarget{Type: "client", ID: client.ID}, map[string]any{
		"name": client.Name,
		"slug": client.Slug,
	})
	created, err := h.store.Clients.Create(client)
	if err != nil {
		if errors.Is(err, store.ErrSlugTaken) {
			c.JSON(http.StatusConflict, gin.H{
				"error": "a client with that slug already exists",
				"slug":  client.Slug,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create client"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"client": created})
}

// GetClient returns one client inside the caller's scope.
// GET /api/clients/:id
func (h *ClientHandler) GetClient(c *gin.Context) {
	client, ok := h.scopedClient(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"client": client})
}

// UpdateClient changes a client. Admin only.
// PUT /api/clients/:id
func (h *ClientHandler) UpdateClient(c *gin.Context) {
	var req clientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}

	client, err := h.store.Clients.GetByID(c.Param("id"))
	if err != nil || client == nil {
		ClientNotFound(c)
		return
	}
	client.Name = req.Name
	if req.Slug != "" {
		client.Slug = models.NormalizeSlug(req.Slug)
	}
	client.Notes = req.Notes
	if req.Disabled != nil {
		client.Disabled = *req.Disabled
	}

	// `disabled` is in the record: pausing a tenant is the act that locks
	// technicians out of a fleet, and the row has to say it happened here.
	defer auditForAnswer(c, h.store, models.AuditClientUpdate, AuditTarget{Type: "client", ID: client.ID}, map[string]any{
		"name":     client.Name,
		"slug":     client.Slug,
		"disabled": client.Disabled,
	})
	if err := h.store.Clients.Update(client); err != nil {
		if errors.Is(err, store.ErrSlugTaken) {
			c.JSON(http.StatusConflict, gin.H{
				"error": "a client with that slug already exists",
				"slug":  client.Slug,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update client"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"client": client})
}

// DeleteClient removes a client and moves its devices to unassigned.
// DELETE /api/clients/:id
//
// The devices survive. `ClientStore.Delete` nulls their `client_id` in the same
// transaction and drops the grants that named the client, and this handler
// reports how many units moved, because the operator deleting a tenant needs to
// know the fleet is now in the unassigned pool and not gone.
func (h *ClientHandler) DeleteClient(c *gin.Context) {
	id := c.Param("id")

	// The caller's scope is checked on the store row first: an admin deletes,
	// and admin is scope-all, so this is the identity check, not a filter.
	if existing, err := h.store.Clients.GetByID(id); err != nil || existing == nil {
		ClientNotFound(c)
		return
	}
	devices, err := h.store.Devices.ListScoped([]string{id}, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count the client's units"})
		return
	}

	// Deleting a tenant moves its units to unassigned, which makes them
	// invisible to every non-admin. The count is in the record, because "the
	// fleet disappeared" is the question that follows this act.
	defer auditForAnswer(c, h.store, models.AuditClientDelete, AuditTarget{Type: "client", ID: id}, map[string]any{
		"devices_unassigned": len(devices),
	})
	if err := h.store.Clients.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete client"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "devices_unassigned": len(devices)})
}

// AssignDevices moves a batch of units into a client. Admin only.
// PUT /api/clients/:id/devices   {device_ids: [...]}
//
// Targets are validated **before** any write, and a target the caller cannot
// reach is not a target: assignment is how a unit leaves one manager's fleet and
// enters another's, so a bulk assign that quietly skipped the units outside the
// caller's reach would move machines the caller has no standing to move.
//
// An empty `device_ids` empties the client: its units go to unassigned. One call,
// two directions, so "assign" and "unassign" cannot drift into two definitions of
// ownership.
func (h *ClientHandler) AssignDevices(c *gin.Context) {
	var req struct {
		DeviceIDs []string `json:"device_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	client, err := h.store.Clients.GetByID(c.Param("id"))
	if err != nil || client == nil {
		ClientNotFound(c)
		return
	}

	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)

	if len(req.DeviceIDs) == 0 {
		mine, err := h.store.Devices.ListScoped([]string{client.ID}, false)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list the client's units"})
			return
		}
		ids := make([]string, 0, len(mine))
		for _, d := range mine {
			ids = append(ids, d.ID)
		}
		defer auditForAnswer(c, h.store, models.AuditDeviceAssign, AuditTarget{Type: "client", ID: client.ID}, map[string]any{
			"unassign": ids,
		})
		n, err := h.store.Clients.AssignDevices("", ids)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unassign the client's units"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "unassigned": n})
		return
	}

	// Every target must be a unit the caller can see, and every target must
	// exist: a request naming an unknown unit is refused naming it, the same
	// bulk contract as the patch and file routes.
	if !scope.all {
		foreign, err := h.store.Clients.DeviceIDsNotIn(scope.ClientIDs(), scope.IncludeUnassigned(), req.DeviceIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve client scope"})
			return
		}
		if len(foreign) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      "one or more target devices are outside your client scope",
				"device_ids": foreign,
			})
			return
		}
	}

	// Assignment is the act that decides which technician sees which fleet, so
	// the record carries the whole list it was given.
	defer auditForAnswer(c, h.store, models.AuditDeviceAssign, AuditTarget{Type: "client", ID: client.ID}, map[string]any{
		"device_ids": req.DeviceIDs,
	})
	n, err := h.store.Clients.AssignDevices(client.ID, req.DeviceIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign devices"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "assigned": n})
}

// clientRollup is one client's ledger row. Every number here is computed from
// the same store reads the device ledger uses (2.2-4's single-source rule): the
// console must never show a rollup a technician cannot reproduce by filtering
// the fleet to that client.
type clientRollup struct {
	ClientID    string     `json:"client_id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Disabled    bool       `json:"disabled"`
	Units       int        `json:"units"`
	UnitsOnline int        `json:"units_online"`
	LastSeen    *time.Time `json:"last_seen,omitempty"`

	AlertsOpen     int `json:"alerts_open"`
	AlertsCritical int `json:"alerts_critical"`
	AlertsWarning  int `json:"alerts_warning"`
	AlertsInfo     int `json:"alerts_info"`

	PatchUnits         int `json:"patch_units"`
	PatchCompliant     int `json:"patch_compliant"`
	PatchPending       int `json:"patch_pending"`
	PatchCritical      int `json:"patch_critical"`
	PatchRebootPending int `json:"patch_reboot_pending"`

	Technicians int `json:"technicians"`
}

// ClientRollup answers one client's ledger numbers.
// GET /api/clients/:id/rollup
func (h *ClientHandler) ClientRollup(c *gin.Context) {
	client, ok := h.scopedClient(c)
	if !ok {
		return
	}

	devices, err := h.store.Devices.ListScoped([]string{client.ID}, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list the client's units"})
		return
	}
	deviceIDs := make([]string, 0, len(devices))
	for _, d := range devices {
		deviceIDs = append(deviceIDs, d.ID)
	}

	// Alerts, in scope. An empty fleet answers zero, and `ListScoped` is the
	// same store query the alert ledger runs — scoped by the **client**, which
	// is why a rollup over a 2,000-unit tenant costs the same two bind
	// parameters as one over two units (2.3-8).
	alerts, err := h.store.Alerts.ListScoped([]string{client.ID}, false, nil, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count alerts"})
		return
	}

	// Patch compliance, computed inside the client - the same open-status set
	// the fleet overview uses.
	updates, err := h.store.SoftwareUpdates.ListByStatusesScoped(openStatuses, []string{client.ID}, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count updates"})
		return
	}

	// The technicians this client is granted to. Admin-only visibility on the
	// grant list, so a manager reading their own client sees the colleagues
	// who share it.
	technicians, err := h.store.Clients.UsersForClient(client.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count technicians"})
		return
	}

	rollup := rollupOf(*client, devices, alerts, updates, len(technicians))

	c.JSON(http.StatusOK, gin.H{"rollup": rollup})
}

// rollupOf is the one arithmetic behind every per-client figure. Units, alerts
// and open updates arrive already scoped to this client's fleet, so the ledger
// row, the dashboard row and the detail read cannot drift: they are the same
// function over the same rows (2.2-4's single-source rule, extended to the
// tenant axis by 2.3-5).
func rollupOf(client models.Client, devices []models.Device, alerts []models.Alert, updates []models.SoftwareUpdate, technicians int) clientRollup {
	rollup := clientRollup{
		ClientID: client.ID, Name: client.Name, Slug: client.Slug, Disabled: client.Disabled,
		Units: len(devices),
	}

	var lastSeen *time.Time
	for _, d := range devices {
		if d.Status == "online" {
			rollup.UnitsOnline++
		}
		if d.RebootPending {
			rollup.PatchRebootPending++
		}
		seen := d.LastSeen
		if !seen.IsZero() && (lastSeen == nil || seen.After(*lastSeen)) {
			lastSeen = &seen
		}
	}
	rollup.LastSeen = lastSeen
	rollup.PatchUnits = len(devices)

	for _, a := range alerts {
		rollup.AlertsOpen++
		switch a.Severity {
		case "critical":
			rollup.AlertsCritical++
		case "warning":
			rollup.AlertsWarning++
		default:
			rollup.AlertsInfo++
		}
	}

	withOpen := map[string]bool{}
	for _, u := range updates {
		withOpen[u.DeviceID] = true
		if u.Severity == "critical" {
			rollup.PatchCritical++
		}
	}
	rollup.PatchPending = len(updates)
	rollup.PatchCompliant = len(devices) - len(withOpen)
	rollup.Technicians = technicians
	return rollup
}

// ClientRollups answers every client the caller may see, in one read.
// GET /api/clients/rollups
//
// The dashboard's per-client rows and the Clients ledger's rows must be the
// same numbers (2.3-5, 2.2-4's single-source rule), and the only way to
// guarantee that is one arithmetic over one set of reads. Asked per client, an
// installation with 40 tenants costs 40 rollup reads on the dashboard mount;
// asked once, it costs one read per collection - devices, alerts, updates,
// clients, grants - whatever the tenant count.
//
// `admin` also gets the `unassigned` bucket: the units no tenant owns are the
// list a 2.2 upgrade lands its fleet in, and an operator needs to see that it
// is empty before they trust the scoping. A scoped caller has no unassigned
// units, so the bucket does not exist for them.
func (h *ClientHandler) ClientRollups(c *gin.Context) {
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)

	all, err := h.store.Clients.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list clients"})
		return
	}
	clients := all
	if !scope.all {
		granted := map[string]bool{}
		for _, id := range scope.ClientIDs() {
			granted[id] = true
		}
		clients = make([]models.Client, 0, len(all))
		for _, cl := range all {
			if granted[cl.ID] {
				clients = append(clients, cl)
			}
		}
	}

	// Admin reads the fleet whole; a scoped caller reads their client set.
	// `ListScoped` answers *nothing* for an empty client set - that is the rule
	// that stops "no rows granted" from reading as "sees everything" - so an
	// admin rollup built through it would report an installation with no units.
	var devices []models.Device
	if scope.All() {
		devices, err = h.store.Devices.ListAll()
	} else {
		devices, err = h.store.Devices.ListScoped(scope.ClientIDs(), scope.IncludeUnassigned())
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list the caller's units"})
		return
	}
	deviceIDs := make([]string, 0, len(devices))
	clientOf := map[string]string{}
	byClient := map[string][]models.Device{}
	for _, d := range devices {
		deviceIDs = append(deviceIDs, d.ID)
		key := models.UnassignedClientID
		if d.ClientID != nil {
			key = *d.ClientID
		}
		clientOf[d.ID] = key
		byClient[key] = append(byClient[key], d)
	}

	// Alerts and open updates are read once for the whole scope, then grouped
	// by the tenant their unit belongs to - the same grouping the ledger's
	// client column shows, so a row on the dashboard and a row on the ledger
	// are counts of the same array. 2.3-8's shape applies here too: the scoped
	// read is over the caller's **clients** (two bind parameters, whatever the
	// fleet size), and the admin's is the whole collection - the same
	// admin/scoped split the device read above makes, for the same reason: an
	// empty client set answers no rows, so an admin rollup built through
	// `ListScoped` would report an installation with no units.
	var alerts []models.Alert
	if scope.All() {
		alerts, err = h.store.Alerts.List(nil, true)
	} else {
		alerts, err = h.store.Alerts.ListScoped(scope.ClientIDs(), scope.IncludeUnassigned(), nil, true)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count alerts"})
		return
	}
	alertsByClient := map[string][]models.Alert{}
	for _, a := range alerts {
		if key, ok := clientOf[a.DeviceID]; ok {
			alertsByClient[key] = append(alertsByClient[key], a)
		}
	}

	var updates []models.SoftwareUpdate
	if scope.All() {
		updates, err = h.store.SoftwareUpdates.ListByStatuses(openStatuses)
	} else {
		updates, err = h.store.SoftwareUpdates.ListByStatusesScoped(openStatuses, scope.ClientIDs(), scope.IncludeUnassigned())
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count updates"})
		return
	}
	updatesByClient := map[string][]models.SoftwareUpdate{}
	for _, u := range updates {
		if key, ok := clientOf[u.DeviceID]; ok {
			updatesByClient[key] = append(updatesByClient[key], u)
		}
	}

	// One grouped read for every tenant's grant count, not one per tenant.
	grants, err := h.store.Clients.GrantsCount()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count technicians"})
		return
	}

	out := make([]clientRollup, 0, len(clients)+1)
	for _, cl := range clients {
		out = append(out, rollupOf(cl, byClient[cl.ID], alertsByClient[cl.ID], updatesByClient[cl.ID], grants[cl.ID]))
	}
	if units := byClient[models.UnassignedClientID]; scope.all && len(units) > 0 {
		// The sentinel row is named like the sentinel the client list answers,
		// so the dashboard, the ledger and the unit view all call the same
		// bucket the same thing.
		out = append(out, rollupOf(models.UnassignedPseudoClient(), units,
			alertsByClient[models.UnassignedClientID], updatesByClient[models.UnassignedClientID], 0))
	}

	c.JSON(http.StatusOK, gin.H{"rollups": out})
}

// ListClientUsers answers who is granted a client. Admin only.
// GET /api/clients/:id/users
func (h *ClientHandler) ListClientUsers(c *gin.Context) {
	client, ok := h.scopedClient(c)
	if !ok {
		return
	}
	users, err := h.store.Clients.UsersForClient(client.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list the client's users"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

// SetClientUsers replaces the accounts granted a client. Admin only.
// PUT /api/clients/:id/users   {user_ids: [...]}
//
// The grants are rows, so the change lands on the next request with a live
// token — 2.3-1's criterion, and the reason this route exists at all: an MSP
// moves a technician off a client and the technician's console stops showing
// that fleet immediately, with no logout in the story.
func (h *ClientHandler) SetClientUsers(c *gin.Context) {
	var req struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	client, err := h.store.Clients.GetByID(c.Param("id"))
	if err != nil || client == nil {
		ClientNotFound(c)
		return
	}

	desired := map[string]bool{}
	for _, id := range req.UserIDs {
		desired[id] = true
	}
	current, err := h.store.Clients.UsersForClient(client.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read the client's users"})
		return
	}
	for _, u := range current {
		if !desired[u.ID] {
			if err := h.store.Clients.RemoveUserClient(u.ID, client.ID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke a grant"})
				return
			}
		}
	}
	// Grants are recorded with the set they replace: a scope change that does
	// not say what it removed is not a record of the change.
	defer auditForAnswer(c, h.store, models.AuditClientGrant, AuditTarget{Type: "client", ID: client.ID}, map[string]any{
		"user_ids": req.UserIDs,
		"was":      currentIDs(current),
	})
	added := 0
	for _, id := range req.UserIDs {
		if err := h.store.Clients.AddUserClient(id, client.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add a grant"})
			return
		}
		added++
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "users": len(req.UserIDs), "added": added})
}
