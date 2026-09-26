package alerts

import (
	"testing"
	"time"

	"ourway/server/models"
)

// MockAlertStore is a mock implementation of AlertStoreInterface for testing
type MockAlertStore struct {
	alerts []*models.Alert
}

func (m *MockAlertStore) Create(a *models.Alert) error {
	m.alerts = append(m.alerts, a)
	return nil
}

func (m *MockAlertStore) List(deviceID *string, unresolvedOnly bool) ([]models.Alert, error) {
	var result []models.Alert
	for _, a := range m.alerts {
		if deviceID != nil && *deviceID != "" && a.DeviceID != *deviceID {
			continue
		}
		if unresolvedOnly && a.Resolved {
			continue
		}
		result = append(result, *a)
	}
	return result, nil
}

func (m *MockAlertStore) GetByID(id string) (*models.Alert, error) {
	for _, a := range m.alerts {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, nil
}

func (m *MockAlertStore) MarkResolved(id string) error {
	for _, a := range m.alerts {
		if a.ID == id {
			a.Resolved = true
			a.ResolvedAt = time.Now()
			return nil
		}
	}
	return nil
}

func (m *MockAlertStore) Acknowledge(id, userID string) error {
	for _, a := range m.alerts {
		if a.ID == id {
			a.Acknowledged = true
			a.AcknowledgedBy = userID
			a.AcknowledgedAt = time.Now()
			return nil
		}
	}
	return nil
}

func (m *MockAlertStore) Assign(id, userID string) error {
	for _, a := range m.alerts {
		if a.ID == id {
			a.AssignedTo = userID
			return nil
		}
	}
	return nil
}

func TestNewEngine(t *testing.T) {
	mockStore := &MockAlertStore{}
	engine := NewEngine(mockStore)

	if engine == nil {
		t.Fatal("NewEngine returned nil")
	}
	if len(engine.thresholds) != 3 {
		t.Errorf("Expected 3 default thresholds, got %d", len(engine.thresholds))
	}
}

func TestEvaluateHighCPU(t *testing.T) {
	mockStore := &MockAlertStore{}
	engine := NewEngine(mockStore)

	metrics := models.Metrics{
		CPU:       95,
		RAM:       50,
		DiskUsage: 50,
	}

	engine.Evaluate(metrics, "device-1", "Test Device")

	if len(mockStore.alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(mockStore.alerts))
	}
	alert := mockStore.alerts[0]
	if alert.Severity != "critical" {
		t.Errorf("Expected critical severity, got %s", alert.Severity)
	}
	if alert.Metric != "cpu" {
		t.Errorf("Expected cpu metric, got %s", alert.Metric)
	}
}

func TestEvaluateWarningCPU(t *testing.T) {
	mockStore := &MockAlertStore{}
	engine := NewEngine(mockStore)

	metrics := models.Metrics{
		CPU:       85,
		RAM:       50,
		DiskUsage: 50,
	}

	engine.Evaluate(metrics, "device-1", "Test Device")

	if len(mockStore.alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(mockStore.alerts))
	}
	alert := mockStore.alerts[0]
	if alert.Severity != "warning" {
		t.Errorf("Expected warning severity, got %s", alert.Severity)
	}
}

func TestEvaluateNormal(t *testing.T) {
	mockStore := &MockAlertStore{}
	engine := NewEngine(mockStore)

	metrics := models.Metrics{
		CPU:       50,
		RAM:       50,
		DiskUsage: 50,
	}

	engine.Evaluate(metrics, "device-1", "Test Device")

	if len(mockStore.alerts) != 0 {
		t.Errorf("Expected 0 alerts, got %d", len(mockStore.alerts))
	}
}

func TestDeduplication(t *testing.T) {
	mockStore := &MockAlertStore{}
	engine := NewEngine(mockStore)
	engine.SetDedupWindow(5 * time.Minute)

	metrics := models.Metrics{
		CPU:       95,
		RAM:       50,
		DiskUsage: 50,
	}

	// First evaluation should create an alert
	engine.Evaluate(metrics, "device-1", "Test Device")
	if len(mockStore.alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(mockStore.alerts))
	}

	// Second evaluation within dedup window should not create another alert
	engine.Evaluate(metrics, "device-1", "Test Device")
	if len(mockStore.alerts) != 1 {
		t.Errorf("Expected 1 alert (deduped), got %d", len(mockStore.alerts))
	}

	// Different device should create a new alert
	engine.Evaluate(metrics, "device-2", "Test Device 2")
	if len(mockStore.alerts) != 2 {
		t.Errorf("Expected 2 alerts, got %d", len(mockStore.alerts))
	}
}

func TestSetThresholds(t *testing.T) {
	mockStore := &MockAlertStore{}
	engine := NewEngine(mockStore)

	// Set custom threshold: warning at 50, critical at 70
	engine.SetThresholds("cpu", Thresholds{Warning: 50, Critical: 70})

	metrics := models.Metrics{
		CPU:       60,
		RAM:       50,
		DiskUsage: 50,
	}

	engine.Evaluate(metrics, "device-1", "Test Device")

	if len(mockStore.alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(mockStore.alerts))
	}
	if mockStore.alerts[0].Severity != "warning" {
		t.Errorf("Expected warning severity, got %s", mockStore.alerts[0].Severity)
	}
}
