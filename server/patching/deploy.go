package patching

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

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

// DeployToDevices deploys approved updates to devices.
func (d *Deployer) DeployToDevices(ctx context.Context, deviceIDs []string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

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

	// Send deploy command to each device
	for _, deviceID := range deviceIDs {
		device, err := d.store.Devices.GetByID(deviceID)
		if err != nil {
			log.Printf("patching: failed to get device %s: %v", deviceID, err)
			// Unknown device: count it as failed so the deployment can finish.
			d.recordResult(deployment.ID, deviceID, "failed")
			continue
		}

		// Get the approved updates for THIS device only (never cross-device)
		updates, err := d.store.SoftwareUpdates.ListByDeviceAndStatus(deviceID, "approved")
		if err != nil {
			log.Printf("patching: failed to list updates for device %s: %v", deviceID, err)
			continue
		}

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
			d.recordResult(deployment.ID, deviceID, "failed")
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
// counters once.
func (d *Deployer) recordResult(deploymentID, deviceID, result string) {
	changed, err := d.store.DeploymentResults.UpsertResult(deploymentID, deviceID, result)
	if err != nil {
		log.Printf("patching: failed to record result for %s/%s: %v", deploymentID, deviceID, err)
		return
	}
	if changed {
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
// (deployment, device) pair are idempotent.
func (d *Deployer) ReportResult(deploymentID, deviceID, result string) error {
	d.recordResult(deploymentID, deviceID, result)
	return nil
}
