package patching

import (
	"log"

	"ourway/server/store"
	"ourway/server/ws"
)

// RollbackManager handles deployment rollbacks.
type RollbackManager struct {
	store *store.Store
	hub   *ws.Hub
}

// NewRollbackManager creates a new rollback manager.
func NewRollbackManager(store *store.Store, hub *ws.Hub) *RollbackManager {
	return &RollbackManager{store: store, hub: hub}
}

// RollbackDeployment rolls back a deployment on the given devices.
// It returns "rollback_initiated" when rollback commands were sent, or
// "no_action" when the deployment is not in a rollback-eligible state.
func (r *RollbackManager) RollbackDeployment(deploymentID string, deviceIDs []string) (string, error) {
	deployment, err := r.store.PatchDeployments.GetByID(deploymentID)
	if err != nil {
		return "", err
	}

	if deployment.Status != "failed" && deployment.Status != "completed" {
		return "no_action", nil
	}

	// Send rollback command to each device
	for _, deviceID := range deviceIDs {
		device, err := r.store.Devices.GetByID(deviceID)
		if err != nil {
			log.Printf("rollback: failed to get device %s: %v", deviceID, err)
			continue
		}

		if err := r.hub.SendToDevice(device.DeviceKey, "rollback_updates", map[string]interface{}{
			"deployment_id": deploymentID,
			"device_id":     deviceID,
		}); err != nil {
			log.Printf("rollback: failed to send rollback to device %s: %v", device.Name, err)
		}
	}

	log.Printf("rollback: rollback initiated for deployment %s on %d devices", deploymentID, len(deviceIDs))
	return "rollback_initiated", nil
}
