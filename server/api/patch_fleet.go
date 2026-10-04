package api

import (
	"sort"

	"github.com/gin-gonic/gin"
)

// openStatuses are the update states that count against compliance.
var openStatuses = []string{"detected", "approved", "installing", "failed"}

var severityRank = map[string]int{"critical": 0, "important": 1, "moderate": 2, "low": 3, "unspecified": 4}

// deviceCompliance is one device's patch summary.
type deviceCompliance struct {
	DeviceID      string `json:"device_id"`
	Name          string `json:"name"`
	OS            string `json:"os"`
	Status        string `json:"status"`
	Detected      int    `json:"detected"`
	Approved      int    `json:"approved"`
	Installing    int    `json:"installing"`
	Failed        int    `json:"failed"`
	Critical      int    `json:"critical"`
	RebootPending bool   `json:"reboot_pending"`
	Compliant     bool   `json:"compliant"`
}

// PatchOverview returns fleet patch compliance: totals and one row per
// device. A device is compliant when it has no detected, approved,
// installing or failed updates.
// GET /api/patch/overview
func (h *PatchHandler) PatchOverview(c *gin.Context) {
	devices, err := h.store.Devices.ListAll()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list devices"})
		return
	}
	open, err := h.store.SoftwareUpdates.ListByStatuses(openStatuses)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list updates"})
		return
	}
	byDevice := make(map[string]*deviceCompliance, len(devices))
	rows := make([]*deviceCompliance, 0, len(devices))
	for _, d := range devices {
		row := &deviceCompliance{DeviceID: d.ID, Name: d.Name, OS: d.OS, Status: d.Status, RebootPending: d.RebootPending}
		byDevice[d.ID] = row
		rows = append(rows, row)
	}
	pending, critical := 0, 0
	for _, u := range open {
		row := byDevice[u.DeviceID]
		if row == nil {
			continue
		}
		switch u.Status {
		case "detected":
			row.Detected++
		case "approved":
			row.Approved++
		case "installing":
			row.Installing++
		case "failed":
			row.Failed++
		}
		pending++
		if u.Severity == "critical" {
			row.Critical++
			critical++
		}
	}
	compliant, rebootPending := 0, 0
	for _, row := range rows {
		row.Compliant = row.Detected+row.Approved+row.Installing+row.Failed == 0
		if row.Compliant {
			compliant++
		}
		if row.RebootPending {
			rebootPending++
		}
	}
	// Worst first: critical, then most outstanding.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Critical != rows[j].Critical {
			return rows[i].Critical > rows[j].Critical
		}
		oi := rows[i].Detected + rows[i].Approved + rows[i].Failed
		oj := rows[j].Detected + rows[j].Approved + rows[j].Failed
		return oi > oj
	})
	c.JSON(200, gin.H{
		"totals": gin.H{
			"devices":        len(rows),
			"compliant":      compliant,
			"pending":        pending,
			"critical":       critical,
			"reboot_pending": rebootPending,
		},
		"devices": rows,
	})
}

// fleetUpdate groups the same update across devices.
type fleetUpdate struct {
	Key         string         `json:"key"`
	Title       string         `json:"title"`
	KB          string         `json:"kb"`
	Severity    string         `json:"severity"`
	Source      string         `json:"source"`
	Category    string         `json:"category"`
	Devices     int            `json:"devices"`
	Statuses    map[string]int `json:"statuses"`
	UpdateIDs   []string       `json:"update_ids"`
	DetectedIDs []string       `json:"detected_ids"`
}

// ListFleetUpdates returns outstanding updates grouped across devices so a
// single approval can cover every device that needs it.
// GET /api/patch/updates
func (h *PatchHandler) ListFleetUpdates(c *gin.Context) {
	open, err := h.store.SoftwareUpdates.ListByStatuses(openStatuses)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list updates"})
		return
	}
	groups := map[string]*fleetUpdate{}
	order := []string{}
	for _, u := range open {
		key := u.Source + "|" + u.ExternalID
		if u.ExternalID == "" {
			key = u.Source + "|" + u.Title + "|" + u.Version
		}
		g := groups[key]
		if g == nil {
			sev := u.Severity
			if sev == "" {
				sev = "unspecified"
			}
			g = &fleetUpdate{Key: key, Title: u.Title, KB: u.KB, Severity: sev, Source: u.Source, Category: u.Category,
				Statuses: map[string]int{}, UpdateIDs: []string{}, DetectedIDs: []string{}}
			groups[key] = g
			order = append(order, key)
		}
		g.Devices++
		g.Statuses[u.Status]++
		g.UpdateIDs = append(g.UpdateIDs, u.ID)
		if u.Status == "detected" {
			g.DetectedIDs = append(g.DetectedIDs, u.ID)
		}
	}
	out := make([]*fleetUpdate, 0, len(order))
	for _, k := range order {
		out = append(out, groups[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := severityRank[out[i].Severity], severityRank[out[j].Severity]
		if ri != rj {
			return ri < rj
		}
		return out[i].Devices > out[j].Devices
	})
	c.JSON(200, gin.H{"updates": out})
}

// BulkSetUpdateStatus approves or skips detected updates in bulk.
// POST /api/patch/updates/approve | /skip   {update_ids: []}
func (h *PatchHandler) bulkSet(c *gin.Context, status string) {
	var req struct {
		UpdateIDs []string `json:"update_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.UpdateIDs) == 0 {
		c.JSON(400, gin.H{"error": "update_ids required"})
		return
	}
	n, err := h.store.SoftwareUpdates.SetStatusIfDetected(req.UpdateIDs, status)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to update"})
		return
	}
	c.JSON(200, gin.H{"changed": n})
}

// BulkApprove approves detected updates.
func (h *PatchHandler) BulkApprove(c *gin.Context) { h.bulkSet(c, "approved") }

// BulkSkip marks detected updates as skipped (ignored).
func (h *PatchHandler) BulkSkip(c *gin.Context) { h.bulkSet(c, "skipped") }

// ScanDevices triggers a scan on the given devices (all online devices
// when none are given).
// POST /api/patch/scan   {device_ids?: []}
func (h *PatchHandler) ScanMany(c *gin.Context) {
	var req struct {
		DeviceIDs []string `json:"device_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	ids := req.DeviceIDs
	if len(ids) == 0 {
		devices, err := h.store.Devices.ListAll()
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to list devices"})
			return
		}
		for _, d := range devices {
			ids = append(ids, d.ID)
		}
	}
	c.JSON(200, gin.H{"scans_sent": h.scanner.ScanDeviceIDs(ids)})
}
