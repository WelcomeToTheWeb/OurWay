package api

import (
	"sort"

	"github.com/gin-gonic/gin"

	"ourway/server/models"
	"ourway/server/store"
)

// TagHandler manages device tags.
type TagHandler struct {
	store *store.Store
}

// NewTagHandler creates a TagHandler.
func NewTagHandler(st *store.Store) *TagHandler { return &TagHandler{store: st} }

// ListTags returns every tag in use with its device count.
// GET /api/tags
func (h *TagHandler) ListTags(c *gin.Context) {
	// The tag vocabulary is the fleet's, so the counts are the caller's: a
	// manager of two clients is offered the tags on their units, not the
	// installation's. The tag filter on a scoped list is the store's client
	// filter, so a tag cannot be a side channel to another client's fleet.
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)

	devices, err := h.store.Devices.ListAll()
	if !scope.all {
		devices, err = h.store.Devices.ListScoped(scope.ClientIDs(), scope.IncludeUnassigned())
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list devices"})
		return
	}
	counts := map[string]int{}
	for _, d := range devices {
		for _, t := range d.Tags {
			counts[t]++
		}
	}
	type tagCount struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := make([]tagCount, 0, len(counts))
	for t, n := range counts {
		out = append(out, tagCount{t, n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	c.JSON(200, gin.H{"tags": out})
}

// SetDeviceTags replaces one device's tags.
// PUT /api/devices/:id/tags
func (h *TagHandler) SetDeviceTags(c *gin.Context) {
	var req struct {
		Tags []string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}
	// Tags ride the unit: a technician removed from a client must not be able to
	// retag that client's units, and retagging is how a fleet disappears from
	// someone's view.
	dev, ok := ScopedDevice(c, h.store)
	if !ok {
		return
	}
	dev.Tags = models.NormalizeTags(req.Tags)
	if err := h.store.Devices.Update(dev); err != nil {
		c.JSON(500, gin.H{"error": "failed to update tags"})
		return
	}
	c.JSON(200, gin.H{"tags": dev.Tags})
}

// BulkTags adds and/or removes tags on many devices.
// POST /api/devices/tags/bulk  {device_ids, add, remove}
func (h *TagHandler) BulkTags(c *gin.Context) {
	var req struct {
		DeviceIDs []string `json:"device_ids" binding:"required"`
		Add       []string `json:"add"`
		Remove    []string `json:"remove"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.DeviceIDs) == 0 {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}
	// Bulk targets are validated before anything is written. Silently skipping
	// ids the caller may not touch (the old behaviour, which skipped unknown
	// ones too) means a technician who includes a foreign unit in a bulk
	// request watches it get skipped — a partial success is exactly the
	// "partial bulk succeeded" case the plan refuses. Out-of-scope targets are
	// named, and the fleet is untouched.
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)
	if !scope.all {
		foreign, err := h.store.Clients.DeviceIDsNotIn(scope.ClientIDs(), scope.IncludeUnassigned(), req.DeviceIDs)
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to validate bulk targets"})
			return
		}
		if len(foreign) > 0 {
			c.JSON(400, gin.H{"error": "one or more targets are outside your client scope", "device_ids": foreign})
			return
		}
	}

	add := models.NormalizeTags(req.Add)
	remove := map[string]bool{}
	for _, t := range models.NormalizeTags(req.Remove) {
		remove[t] = true
	}
	updated := 0
	for _, id := range req.DeviceIDs {
		dev, err := h.store.Devices.GetByID(id)
		if err != nil {
			continue
		}
		kept := make([]string, 0, len(dev.Tags)+len(add))
		for _, t := range dev.Tags {
			if !remove[t] {
				kept = append(kept, t)
			}
		}
		dev.Tags = models.NormalizeTags(append(kept, add...))
		if err := h.store.Devices.Update(dev); err == nil {
			updated++
		}
	}
	c.JSON(200, gin.H{"updated": updated})
}
