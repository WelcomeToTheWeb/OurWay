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
			log.Printf("patching: failed to send deploy to device %s: %v", device.Name, err)
		}
	}

	log.Printf("patching: deployment %s started for %d devices", deployment.ID, len(deviceIDs))
	return deployment.ID, nil
}

// ReportResult records a per-device deployment result and transitions the
// deployment to "completed" (all successes) or "failed" (any failure) once
// every targeted device has reported.
func (d *Deployer) ReportResult(deploymentID, deviceID, result string) error {
	if result == "success" {
		if err := d.store.PatchDeployments.IncrementSuccess(deploymentID); err != nil {
			return err
		}
	} else {
		if err := d.store.PatchDeployments.IncrementFailed(deploymentID); err != nil {
			return err
		}
	}
	if _, err := d.store.PatchDeployments.FinishIfComplete(deploymentID); err != nil {
		return err
	}
	return nil
}
