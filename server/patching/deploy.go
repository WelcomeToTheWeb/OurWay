package patching

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// ErrNoApprovedUpdates is returned by DeployToDevices when none of the
// requested devices have approved updates. The API maps it to HTTP 400 so
// the UI does not start a deployment that has nothing to deploy (an empty
// update list used to trigger a full OS upgrade on the agent side).
var ErrNoApprovedUpdates = errors.New("no approved updates for the requested devices")

// Deployer deploys patches to devices.
type Deployer struct {
	store *store.Store
	hub   *ws.Hub
	mu    sync.Mutex
}

// NewDeployer creates a new patch deployer.
func NewDeployer(store *store.Store, hub *ws.Hub) *Deployer {
	return &Deployer{
		store: store,
		hub:   hub,
	}
}

// FinalizeTimedOut marks timed-out running deployments as failed. Callers
// (the deployment API) invoke this lazily so the UI never shows a
// deployment stuck in "running" past its timeout.
func (d *Deployer) FinalizeTimedOut() {
	if n, err := d.store.PatchDeployments.FinalizeTimedOut(); err != nil {
		log.Printf("patching: failed to finalize timed-out deployments: %v", err)
	} else if n > 0 {
		log.Printf("patching: finalized %d timed-out deployment(s)", n)
	}
}

// DeployToDevices deploys approved updates to devices. It first filters the
// requested devices down to those that actually have approved updates, so a
// deployment is never started with an empty update set (which the agent
// would interpret as a request for a full OS upgrade).
func (d *Deployer) DeployToDevices(ctx context.Context, deviceIDs []string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Pre-filter to devices that have at least one approved update, and
	// remember the updates per device so the deploy loop below does not
	// re-query them.
	updatesByDevice := make(map[string][]models.SoftwareUpdate, len(deviceIDs))
	targets := make([]string, 0, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		updates, err := d.store.SoftwareUpdates.ListByDeviceAndStatus(deviceID, "approved")
		if err != nil {
			log.Printf("patching: failed to list updates for device %s: %v", deviceID, err)
			continue
		}
		if len(updates) == 0 {
			continue
		}
		updatesByDevice[deviceID] = updates
		targets = append(targets, deviceID)
	}
	if len(targets) == 0 {
		return "", ErrNoApprovedUpdates
	}
	deviceIDs = targets

	// Create deployment record (tracking the targeted devices so rollback can find them)
	deployment := &models.PatchDeployment{
		ID:        uuid.New().String(),
		Status:    "pending",
		DeviceIDs: deviceIDs,
	}
	if err := d.store.PatchDeployments.Create(deployment); err != nil {
		return "", fmt.Errorf("failed to create deployment: %w", err)
	}

	// Mark as started
	if err := d.store.PatchDeployments.MarkStarted(deployment.ID, len(deviceIDs)); err != nil {
		return "", fmt.Errorf("failed to mark deployment started: %w", err)
	}

	// Flag the updates as installing under this deployment so the result
	// can mark them installed/failed.
	for deviceID, updates := range updatesByDevice {
		ids := make([]string, 0, len(updates))
		for _, u := range updates {
			ids = append(ids, u.ID)
		}
		if err := d.store.SoftwareUpdates.MarkInstalling(ids, deployment.ID); err != nil {
			log.Printf("patching: failed to mark updates installing for %s: %v", deviceID, err)
		}
	}

	// Send deploy command to each device
	for _, deviceID := range deviceIDs {
		device, err := d.store.Devices.GetByID(deviceID)
		if err != nil {
			log.Printf("patching: failed to get device %s: %v", deviceID, err)
			// Unknown device: count it as failed so the deployment can finish.
			d.recordResult(deployment.ID, deviceID, "deploy", "failed", "device not found")
			continue
		}

		// The approved updates for THIS device only (never cross-device),
		// pre-fetched above.
		updates := updatesByDevice[deviceID]

		// Send deploy command
		if err := d.hub.SendToDevice(device.DeviceKey, "deploy_updates", map[string]interface{}{
			"device_id":     deviceID,
			"deployment_id": deployment.ID,
			"updates":       updates,
		}); err != nil {
			// Device is offline (or its connection is gone): record the
			// failure now so the deployment is not left "running" waiting
			// for a report that will never come.
			log.Printf("patching: failed to send deploy to device %s: %v", device.Name, err)
			if rerr := d.store.SoftwareUpdates.RevertToApproved(deployment.ID, deviceID); rerr != nil {
				log.Printf("patching: failed to revert updates for %s: %v", deviceID, rerr)
			}
			d.recordResult(deployment.ID, deviceID, "deploy", "failed", "deploy command not delivered (device unreachable)")
		}
	}

	if _, err := d.store.PatchDeployments.FinishIfComplete(deployment.ID); err != nil {
		log.Printf("patching: failed to check completion for %s: %v", deployment.ID, err)
	}

	log.Printf("patching: deployment %s started for %d devices", deployment.ID, len(deviceIDs))
	return deployment.ID, nil
}

// recordResult stores a per-device outcome and reconciles the deployment
// counters. It is idempotent: the same outcome reported twice moves the
// counters once. Rollback-kind results only update the stored row: they
// arrive after the deployment has finished, so they must not alter the
// deploy-phase counters or completion state.
func (d *Deployer) recordResult(deploymentID, deviceID, kind, result, message string) {
	if kind == "" {
		kind = "deploy"
	}
	changed, err := d.store.DeploymentResults.UpsertResult(deploymentID, deviceID, kind, result, message)
	if err != nil {
		log.Printf("patching: failed to record result for %s/%s: %v", deploymentID, deviceID, err)
		return
	}
	if kind == "rollback" {
		return
	}
	if changed {
		if err := d.store.SoftwareUpdates.FinishDeployment(deploymentID, deviceID, result == "success", message); err != nil {
			log.Printf("patching: failed to resolve updates for %s/%s: %v", deploymentID, deviceID, err)
		}
		success, failed, err := d.store.DeploymentResults.Counts(deploymentID)
		if err != nil {
			log.Printf("patching: failed to count results for %s: %v", deploymentID, err)
			return
		}
		if err := d.store.PatchDeployments.SetCounters(deploymentID, success, failed); err != nil {
			log.Printf("patching: failed to set counters for %s: %v", deploymentID, err)
			return
		}
	}
	if _, err := d.store.PatchDeployments.FinishIfComplete(deploymentID); err != nil {
		log.Printf("patching: failed to check completion for %s: %v", deploymentID, err)
	}
}

// ReportResult records a per-device deployment result and transitions the
// deployment to "completed" (all successes) or "failed" (any failure) once
// every targeted device has reported. Repeated reports for the same
// (deployment, device) pair are idempotent. kind is "deploy" or
// "rollback"; message is optional detail from the agent.
func (d *Deployer) ReportResult(deploymentID, deviceID, kind, result, message string) error {
	d.recordResult(deploymentID, deviceID, kind, result, message)
	return nil
}
