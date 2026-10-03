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

	// Send rollback command to each device. Send failures are recorded as
	// rollback results immediately (M14); agent-reported outcomes arrive
	// later via /api/agent/deployments/result with kind "rollback".
	recorded := 0
	for _, deviceID := range deviceIDs {
		device, err := r.store.Devices.GetByID(deviceID)
		if err != nil {
			log.Printf("rollback: failed to get device %s: %v", deviceID, err)
			r.recordRollbackResult(deploymentID, deviceID, "rollback device not found")
			continue
		}

		if err := r.hub.SendToDevice(device.DeviceKey, "rollback_updates", map[string]interface{}{
			"deployment_id": deploymentID,
			"device_id":     deviceID,
		}); err != nil {
			log.Printf("rollback: failed to send rollback to device %s: %v", device.Name, err)
			r.recordRollbackResult(deploymentID, deviceID, "rollback command not delivered (device unreachable)")
			continue
		}
		recorded++
	}

	log.Printf("rollback: rollback initiated for deployment %s on %d devices", deploymentID, recorded)
	return "rollback_initiated", nil
}

// recordRollbackResult stores a failed rollback outcome for one device so
// the failure is visible even if the agent never reports back.
func (r *RollbackManager) recordRollbackResult(deploymentID, deviceID, message string) {
	if _, err := r.store.DeploymentResults.UpsertResult(deploymentID, deviceID, "rollback", "failed", message); err != nil {
		log.Printf("rollback: failed to record rollback result for %s/%s: %v", deploymentID, deviceID, err)
	}
}
