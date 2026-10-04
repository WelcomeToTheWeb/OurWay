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
	devices, err := h.store.Devices.ListAll()
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
	dev, err := h.store.Devices.GetByID(c.Param("id"))
	if err != nil {
		c.JSON(404, gin.H{"error": "device not found"})
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
