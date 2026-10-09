package automation

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// Engine periodically evaluates runbooks and dispatches their command to
// the scoped, online devices over the device WebSocket. Per-device results
// are recorded as RunbookRun rows when the agents report back.
type Engine struct {
	store *store.Store
	hub   *ws.Hub
	// Now is swappable so tests can pin the evaluation time.
	Now func() time.Time
}

// NewEngine creates a runbook engine.
func NewEngine(st *store.Store, hub *ws.Hub) *Engine {
	return &Engine{store: st, hub: hub, Now: time.Now}
}

// Run starts the periodic loop (one check per minute); it stops with ctx.
func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.EvaluateDue()
			e.pruneRuns()
		}
	}
}

// EvaluateDue dispatches every enabled runbook that is inside its
// maintenance window and has not yet run in it. Exported for testing.
func (e *Engine) EvaluateDue() {
	now := e.Now()
	runbooks, err := e.store.Runbooks.ListAll()
	if err != nil {
		log.Printf("runbook: failed to list runbooks: %v", err)
		return
	}
	for i := range runbooks {
		rb := &runbooks[i]
		if !rb.Enabled {
			continue
		}
		if !scheduleDue(rb.Schedule, now) {
			continue
		}
		if _, _, in := inWindow(rb, now); !in {
			continue
		}
		if _, _, already := ranThisWindow(rb, now); already {
			continue
		}
		// Record the run first: a runbook that fails halfway must not
		// restart on the next tick of the same window.
		ran := now
		rb.LastRunAt = &ran
		if err := e.store.Runbooks.Update(rb); err != nil {
			log.Printf("runbook: failed to record run of %s: %v", rb.Name, err)
			continue
		}
		e.dispatch(rb)
	}
}

// DispatchNow dispatches the runbook immediately, bypassing the
// schedule/window/Enabled gate (manual trigger from the API).
func (e *Engine) DispatchNow(rb *models.Runbook) {
	e.dispatch(rb)
}

// dispatch sends the runbook command to each online device in scope and
// creates a pending RunbookRun row per device.
func (e *Engine) dispatch(rb *models.Runbook) {
	deviceIDs, err := e.resolveScope(rb)
	if err != nil {
		log.Printf("runbook: failed to resolve scope of %s: %v", rb.Name, err)
		return
	}
	sent := 0
	for _, id := range deviceIDs {
		device, err := e.store.Devices.GetByID(id)
		if err != nil {
			continue
		}
		if device.Status != "online" {
			continue
		}
		run := &models.RunbookRun{
			ID:        uuid.New().String(),
			RunbookID: rb.ID,
			DeviceID:  id,
			Status:    "pending",
			StartedAt: e.Now(),
		}
		if err := e.store.Runbooks.CreateRun(run); err != nil {
			log.Printf("runbook: failed to record run for device %s: %v", id, err)
			continue
		}
		payload := map[string]interface{}{
			"run_id":    run.ID,
			"device_id": id,
			"command":   rb.Command,
		}
		if err := e.hub.SendToDevice(device.DeviceKey, "run_command", payload); err != nil {
			run.Status = "failed"
			run.Output = "device not connected: " + err.Error()
			now := e.Now()
			run.FinishedAt = &now
			_ = e.store.Runbooks.UpdateRun(run)
			continue
		}
		sent++
	}
	log.Printf("runbook: %s dispatched to %d/%d device(s)", rb.Name, sent, len(deviceIDs))
}

// pruneRuns deletes run history older than 30 days.
func (e *Engine) pruneRuns() {
	if err := e.store.Runbooks.DeleteOldRuns(30 * 24 * time.Hour); err != nil {
		log.Printf("runbook: failed to prune run history: %v", err)
	}
}

// resolveScope maps a runbook's Scope/ScopeValue to concrete device IDs,
// mirroring the patch-policy scope semantics.
func (e *Engine) resolveScope(rb *models.Runbook) ([]string, error) {
	devices, err := e.store.Devices.ListAll()
	if err != nil {
		return nil, err
	}
	switch rb.Scope {
	case "all":
		ids := make([]string, 0, len(devices))
		for _, d := range devices {
			ids = append(ids, d.ID)
		}
		return ids, nil
	case "tags", "devices":
		wanted := make(map[string]bool)
		var wantedTags []string
		for _, part := range strings.Split(rb.ScopeValue, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				wanted[part] = true
				wantedTags = append(wantedTags, strings.ToLower(part))
			}
		}
		var ids []string
		for _, d := range devices {
			if wanted[d.ID] || wanted[d.Name] || wanted[d.Hostname] ||
				(rb.Scope == "tags" && d.HasAnyTag(wantedTags)) {
				ids = append(ids, d.ID)
			}
		}
		return ids, nil
	default:
		log.Printf("runbook: unknown scope %q on %s, treating as no devices", rb.Scope, rb.Name)
		return nil, nil
	}
}

// scheduleDue mirrors the patch-policy schedule: daily matches every day,
// weekly Sundays, monthly the 1st.
func scheduleDue(schedule string, now time.Time) bool {
	switch strings.ToLower(strings.TrimSpace(schedule)) {
	case "daily":
		return true
	case "weekly":
		return now.Weekday() == time.Sunday
	case "monthly":
		return now.Day() == 1
	default:
		return false
	}
}

// runbookLocation resolves the runbook's time zone (UTC when empty/unknown).
func runbookLocation(rb *models.Runbook) *time.Location {
	if rb.Timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(rb.Timezone)
	if err != nil {
		log.Printf("runbook: unknown timezone %q on %s, using UTC", rb.Timezone, rb.Name)
		return time.UTC
	}
	return loc
}

// inWindow reports whether now falls inside one of the runbook's
// maintenance windows (same semantics as patch policies).
func inWindow(rb *models.Runbook, now time.Time) (start, end time.Time, in bool) {
	loc := runbookLocation(rb)
	local := now.In(loc)
	hour, min, ok := parseWindowStart(rb.WindowStart)
	if !ok {
		hour, min = 0, 0
	}
	hours := rb.WindowHours
	if hours <= 0 {
		hours = 24
	}
	today := time.Date(local.Year(), local.Month(), local.Day(), hour, min, 0, 0, loc)
	if !local.Before(today) && local.Before(today.Add(time.Duration(hours)*time.Hour)) {
		return today, today.Add(time.Duration(hours) * time.Hour), true
	}
	yesterday := today.AddDate(0, 0, -1)
	if !local.Before(yesterday) && local.Before(yesterday.Add(time.Duration(hours)*time.Hour)) {
		return yesterday, yesterday.Add(time.Duration(hours) * time.Hour), true
	}
	return today, today.Add(time.Duration(hours) * time.Hour), false
}

// ranThisWindow reports whether the runbook's LastRunAt falls inside the
// current maintenance window, so it runs once per window.
func ranThisWindow(rb *models.Runbook, now time.Time) (start, end time.Time, already bool) {
	start, end, in := inWindow(rb, now)
	if !in || rb.LastRunAt == nil {
		return start, end, false
	}
	last := *rb.LastRunAt
	return start, end, !last.Before(start) && last.Before(end)
}

// parseWindowStart parses "HH:MM" (empty means midnight).
func parseWindowStart(v string) (hour, min int, ok bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, 0, true
	}
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}
