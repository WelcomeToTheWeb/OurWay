package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ourway/server/models"
)

// 2.3-2 — the scoping helpers the patch routes share.
//
// Detected updates, deployments and transfers reference a `device_id` and carry
// no client of their own, so scoping them is membership in the caller's visible
// unit set. `fleetScope` answers that set once per request (2.3-8's rule), and
// every filter below is a filter over it.

// refusedTargetsOf pulls the out-of-scope ids out of a bulk refusal's response
// body, so the audit row can name them. The bulk validators already compute
// that list — it is what makes the 400 actionable — and re-deriving it for the
// record would be a second answer to "which ones were refused".
func refusedTargetsOf(body gin.H) []string {
	// The bulk validators name their refused targets under the key the caller
	// was given them under: units for a device command, updates for an update
	// command. Both are recorded, so the row says which kind of id it lists.
	if ids, ok := body["device_ids"].([]string); ok && len(ids) > 0 {
		return ids
	}
	ids, _ := body["update_ids"].([]string)
	return ids
}

// updateTarget names a detected update by the unit it was found on, so the
// record carries the tenant at the time of the act.
func (h *PatchHandler) updateTarget(update *models.SoftwareUpdate) AuditTarget {
	target := AuditTarget{Type: "update", ID: update.ID}
	if device, err := h.store.Devices.GetByID(update.DeviceID); err == nil && device != nil {
		target.ClientID = DeviceTarget(device).ClientID
	}
	return target
}

// fleetScope resolves the caller and their visible units. `restricted` is false
// for admin, whose reads stay unrestricted and cost no scope query.
func (h *PatchHandler) fleetScope(c *gin.Context) (clientScope, []string, bool, error) {
	scope, err := ClientScope(c, h.store)
	if err != nil {
		return scope, nil, false, err
	}
	scope.ApplyScopeHeader(c)
	ids, restricted, err := scope.VisibleDeviceIDs(h.store)
	if err != nil {
		return scope, nil, true, err
	}
	return scope, ids, restricted, nil
}

// scopedDevices returns the units an overview counts: admin's unrestricted read,
// or the scoped read for everyone else, so a compliance table is never built
// from rows the caller was never allowed to see.
func (h *PatchHandler) scopedDevices(scope clientScope) ([]models.Device, error) {
	if scope.all {
		return h.store.Devices.ListAll()
	}
	return h.store.Devices.ListScoped(scope.ClientIDs(), scope.IncludeUnassigned())
}

// scopedOpenUpdates reads the open updates inside the caller's fleet. The filter
// is the store's SQL over the caller's **clients** (`ListByStatusesScoped`), so a
// scoped manager's compliance numbers are computed over their clients and never
// over a fleet they were not allowed to read — 2.3-2's criterion that scoping is
// a query, not a trim, and 2.3-8's that the query is over the tenant set so its
// size is the caller's grant count, not their unit count.
func (h *PatchHandler) scopedOpenUpdates(scope clientScope, restricted bool) ([]models.SoftwareUpdate, error) {
	if !restricted {
		return h.store.SoftwareUpdates.ListByStatuses(openStatuses)
	}
	return h.store.SoftwareUpdates.ListByStatusesScoped(openStatuses, scope.ClientIDs(), scope.IncludeUnassigned())
}

// validateDeviceTargets checks unit ids a command is about to reach. Returns
// code 0 when the request may proceed.
func (h *PatchHandler) validateDeviceTargets(c *gin.Context, deviceIDs []string) (int, gin.H) {
	scope, err := ClientScope(c, h.store)
	if err != nil {
		return http.StatusForbidden, gin.H{"error": "caller scope unavailable"}
	}
	scope.ApplyScopeHeader(c)
	if scope.all {
		return 0, nil
	}
	foreign, err := h.store.Clients.DeviceIDsNotIn(scope.ClientIDs(), scope.IncludeUnassigned(), deviceIDs)
	if err != nil {
		return http.StatusInternalServerError, gin.H{"error": "failed to resolve client scope"}
	}
	if len(foreign) > 0 {
		return http.StatusBadRequest, gin.H{
			"error":      "one or more target devices are outside your client scope",
			"device_ids": foreign,
		}
	}
	return 0, nil
}

// validateUpdateTargets checks that every update id the caller names belongs to
// a unit they can see, and names the ones that are not.
//
// The update table is scoped through its unit; an id that names no update is
// reported the same way as one that names a foreign unit's update, because the
// bulk contract is "these ids are not yours to act on" and a response that
// separates the two tells the caller which update ids exist elsewhere.
func (h *PatchHandler) validateUpdateTargets(c *gin.Context, updateIDs []string) (int, gin.H) {
	scope, err := ClientScope(c, h.store)
	if err != nil {
		return http.StatusForbidden, gin.H{"error": "caller scope unavailable"}
	}
	scope.ApplyScopeHeader(c)
	if scope.all {
		return 0, nil
	}
	deviceByUpdate := make(map[string]string, len(updateIDs))
	seen := make(map[string]bool, len(updateIDs))
	for _, id := range updateIDs {
		u, err := h.store.SoftwareUpdates.GetByID(id)
		if err != nil {
			// Unknown id: report it, so "no rows changed" cannot be the caller's
			// only clue that the request they sent was refused.
			deviceByUpdate[id] = ""
			seen[id] = false
			continue
		}
		deviceByUpdate[id] = u.DeviceID
		seen[id] = true
	}
	visible, _, err := scope.VisibleDeviceIDs(h.store)
	if err != nil {
		return http.StatusInternalServerError, gin.H{"error": "failed to resolve client scope"}
	}
	visibleSet := make(map[string]bool, len(visible))
	for _, id := range visible {
		visibleSet[id] = true
	}
	rejected := make([]string, 0)
	for _, id := range updateIDs {
		if !seen[id] || !visibleSet[deviceByUpdate[id]] {
			rejected = append(rejected, id)
		}
	}
	if len(rejected) > 0 {
		return http.StatusBadRequest, gin.H{
			"error":      "one or more updates are outside your client scope",
			"update_ids": rejected,
		}
	}
	return 0, nil
}

// checkUpdateScope answers for one update the caller named. Like the bulk
// version, an update whose unit is out of scope answers update-not-found — the
// same body a nonexistent id gets.
func (h *PatchHandler) checkUpdateScope(c *gin.Context, update *models.SoftwareUpdate) (int, gin.H) {
	scope, err := ClientScope(c, h.store)
	if err != nil {
		return http.StatusForbidden, gin.H{"error": "caller scope unavailable"}
	}
	scope.ApplyScopeHeader(c)
	ok, err := scope.AllowsDeviceID(c, update.DeviceID, h.store)
	if err != nil {
		return http.StatusInternalServerError, gin.H{"error": "failed to resolve client scope"}
	}
	if !ok {
		return http.StatusNotFound, gin.H{"error": "update not found"}
	}
	return 0, nil
}

// keepDeploymentsOnVisibleDevices drops deployments that touch a unit the caller
// cannot see. A deployment is a bundle of units, so it is visible only when every
// unit in it is: a deployment across two clients is not a deployment the manager
// of one of them owns.
func keepDeploymentsOnVisibleDevices(deployments []models.PatchDeployment, deviceIDs []string) []models.PatchDeployment {
	visible := make(map[string]bool, len(deviceIDs))
	for _, id := range deviceIDs {
		visible[id] = true
	}
	kept := make([]models.PatchDeployment, 0, len(deployments))
	for _, d := range deployments {
		all := true
		for _, id := range d.DeviceIDs {
			if !visible[id] {
				all = false
				break
			}
		}
		if all {
			kept = append(kept, d)
		}
	}
	return kept
}

// scopedDeployment loads a deployment and checks every unit it touches. A
// deployment is a bundle of units, so a deployment that names a unit the caller
// cannot see is not visible to them at all — the answer they get is the answer
// for a deployment that does not exist.
func (h *PatchHandler) scopedDeployment(c *gin.Context) (*models.PatchDeployment, bool) {
	deployment, err := h.store.PatchDeployments.GetByID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
		return nil, false
	}
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return nil, false
	}
	scope.ApplyScopeHeader(c)
	if scope.all {
		return deployment, true
	}
	visible, _, err := scope.VisibleDeviceIDs(h.store)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve client scope"})
		return nil, false
	}
	visibleSet := make(map[string]bool, len(visible))
	for _, id := range visible {
		visibleSet[id] = true
	}
	for _, id := range deployment.DeviceIDs {
		if !visibleSet[id] {
			c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
			return nil, false
		}
	}
	return deployment, true
}
