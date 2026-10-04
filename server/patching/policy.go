package patching

import (
	"context"
	"log"
	"strings"
	"time"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// PolicyEngine periodically evaluates patch policies and drives the
// scan -> approve -> deploy -> (optional) reboot pipeline for the devices
// each policy scopes to. Schedule, Scope, AutoReboot and
// MaxDevicesPerBatch were previously stored but never read; this engine is
// their consumer.
type PolicyEngine struct {
	store    *store.Store
	hub      *ws.Hub
	deployer *Deployer
	rebooter *Rebooter
	scanner  *Scanner
	// Now is swappable so tests can pin the evaluation time.
	Now func() time.Time
}

// NewPolicyEngine creates a patch policy engine.
func NewPolicyEngine(store *store.Store, hub *ws.Hub, deployer *Deployer, rebooter *Rebooter, scanner *Scanner) *PolicyEngine {
	return &PolicyEngine{
		store:    store,
		hub:      hub,
		deployer: deployer,
		rebooter: rebooter,
		scanner:  scanner,
		Now:      time.Now,
	}
}

// Run starts the periodic evaluation loop. The ticker granularity is
// hourly so a daily/weekly/monthly policy fires within an hour of the
// local wall-clock window. Stops when ctx is cancelled.
func (e *PolicyEngine) Run(ctx context.Context) {
	const interval = time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.EvaluateDue(ctx)
		}
	}
}

// EvaluateDue runs every policy whose schedule window is due at time now.
// It is exported (and takes now explicitly) so it can be tested without
// waiting for wall-clock windows.
func (e *PolicyEngine) EvaluateDue(ctx context.Context) {
	now := e.Now()
	policies, err := e.store.PatchPolicies.ListAll()
	if err != nil {
		log.Printf("policy: failed to list policies: %v", err)
		return
	}
	for i := range policies {
		policy := policies[i]
		if !scheduleDue(policy.Schedule, now) {
			continue
		}
		if err := e.applyPolicy(ctx, &policy); err != nil {
			log.Printf("policy: failed to apply policy %s (%s): %v", policy.Name, policy.ID, err)
		}
	}
}

// applyPolicy resolves the policy's device scope, scans those devices for
// updates, auto-approves detected updates when approval is not required,
// deploys the approved set in batches of MaxDevicesPerBatch, and reboots
// the devices that reported success when AutoReboot is set.
func (e *PolicyEngine) applyPolicy(ctx context.Context, policy *models.PatchPolicy) error {
	deviceIDs, err := e.resolveScope(policy)
	if err != nil {
		return err
	}
	if len(deviceIDs) == 0 {
		log.Printf("policy: %s matched 0 devices, nothing to do", policy.Name)
		return nil
	}
	online := make([]string, 0, len(deviceIDs))
	for _, id := range deviceIDs {
		device, err := e.store.Devices.GetByID(id)
		if err != nil {
			continue
		}
		if device.Status == "online" {
			online = append(online, id)
		}
	}
	if len(online) == 0 {
		log.Printf("policy: %s: 0 of %d matched devices online, skipping run", policy.Name, len(deviceIDs))
		return nil
	}
	// Refresh patch inventory for the matched devices so approvals are
	// made against current data, then auto-approve when allowed.
	for _, id := range online {
		device, err := e.store.Devices.GetByID(id)
		if err != nil {
			continue
		}
		if err := e.hub.SendToDevice(device.DeviceKey, "scan_updates", map[string]interface{}{
			"device_id": id,
		}); err != nil {
			log.Printf("policy: scan send failed for device %s: %v", id, err)
		}
	}
	if !policy.ApprovalRequired {
		if err := e.autoApprove(deviceIDs); err != nil {
			return err
		}
	}
	// Batch the deploy (MaxDevicesPerBatch, default 10 when unset).
	batchSize := policy.MaxDevicesPerBatch
	if batchSize <= 0 {
		batchSize = 10
	}
	var rebootKeys []string
	for start := 0; start < len(online); start += batchSize {
		end := start + batchSize
		if end > len(online) {
			end = len(online)
		}
		batch := online[start:end]
		deploymentID, err := e.deployer.DeployToDevices(ctx, batch)
		if err != nil {
			if err == ErrNoApprovedUpdates {
				continue
			}
			log.Printf("policy: deploy failed for batch of %d: %v", len(batch), err)
			continue
		}
		log.Printf("policy: %s deployment %s started for %d devices", policy.Name, deploymentID, len(batch))
		if policy.AutoReboot {
			rebootKeys = append(rebootKeys, e.successKeys(deploymentID, batch)...)
		}
	}
	if policy.AutoReboot && len(rebootKeys) > 0 {
		e.rebooter.RebootDevices(rebootKeys)
		log.Printf("policy: %s rebooted %d device(s) after patching", policy.Name, len(rebootKeys))
	}
	return nil
}

// successKeys returns the device keys of the batch's devices whose deploy
// result is success. Devices that have not reported yet are skipped; the
// reboot only touches devices known to have patched cleanly.
func (e *PolicyEngine) successKeys(deploymentID string, batch []string) []string {
	results, err := e.store.DeploymentResults.ListByDeployment(deploymentID)
	if err != nil {
		return nil
	}
	succeeded := make(map[string]bool, len(results))
	for _, result := range results {
		if result.Result == "success" {
			succeeded[result.DeviceID] = true
		}
	}
	var keys []string
	for _, id := range batch {
		if !succeeded[id] {
			continue
		}
		device, err := e.store.Devices.GetByID(id)
		if err != nil {
			continue
		}
		keys = append(keys, device.DeviceKey)
	}
	return keys
}

// resolveScope maps a policy's Scope/ScopeValue to concrete device IDs.
func (e *PolicyEngine) resolveScope(policy *models.PatchPolicy) ([]string, error) {
	devices, err := e.store.Devices.ListAll()
	if err != nil {
		return nil, err
	}
	switch policy.Scope {
	case "all":
		ids := make([]string, 0, len(devices))
		for _, d := range devices {
			ids = append(ids, d.ID)
		}
		return ids, nil
	case "tags", "devices":
		// ScopeValue is comma-separated. For scope "tags" the entries are
		// device tags; for "devices" they are device IDs, names or
		// hostnames. (Scope "tags" also still honours IDs/names so
		// policies saved before tags existed keep matching.)
		wanted := make(map[string]bool)
		var wantedTags []string
		for _, part := range strings.Split(policy.ScopeValue, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				wanted[part] = true
				wantedTags = append(wantedTags, strings.ToLower(part))
			}
		}
		var ids []string
		for _, d := range devices {
			if wanted[d.ID] || wanted[d.Name] || wanted[d.Hostname] ||
				(policy.Scope == "tags" && d.HasAnyTag(wantedTags)) {
				ids = append(ids, d.ID)
			}
		}
		return ids, nil
	default:
		log.Printf("policy: unknown scope %q on policy %s, treating as no devices", policy.Scope, policy.Name)
		return nil, nil
	}
}

// autoApprove marks every detected update for the scoped devices approved
// so the deploy phase can pick them up.
func (e *PolicyEngine) autoApprove(deviceIDs []string) error {
	for _, deviceID := range deviceIDs {
		updates, err := e.store.SoftwareUpdates.ListByDeviceAndStatus(deviceID, "detected")
		if err != nil {
			log.Printf("policy: failed to list detected updates for device %s: %v", deviceID, err)
			continue
		}
		for i := range updates {
			if _, err := e.store.SoftwareUpdates.MarkApproved(updates[i].ID); err != nil {
				log.Printf("policy: failed to approve update %s: %v", updates[i].ID, err)
			}
		}
	}
	return nil
}

// scheduleDue reports whether the schedule (daily/weekly/monthly) is due
// at now. Daily fires every day; weekly on Sundays; monthly on the 1st.
// The evaluation time is truncated to the hour so the hourly Run loop
// fires each due window exactly once.
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
