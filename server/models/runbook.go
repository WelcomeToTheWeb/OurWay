package models

import (
	"time"
)

// Runbook is a scheduled automation: a shell command run on a scope of
// devices on a daily/weekly/monthly schedule, with per-device results
// recorded on RunbookRun.
type Runbook struct {
	ID         string `gorm:"type:uuid;primaryKey" json:"id"`
	Name       string `gorm:"not null;index" json:"name"`
	Scope      string `gorm:"not null;default:all" json:"scope"`       // all, tags, devices
	ScopeValue string `json:"scope_value"`                             // comma-separated tags/ids/names/hostnames
	Schedule   string `gorm:"not null;default:weekly" json:"schedule"` // daily, weekly, monthly
	// Command is executed by the agent with the system shell. Timeout is
	// enforced on both sides; the agent kills the process at AgentTimeout.
	Command string `gorm:"type:text;not null" json:"command"`
	// Enabled gates the scheduler; disabled runbooks can still be run
	// manually via POST /api/automation/runbooks/:id/run.
	// No gorm default tag on purpose: a default tag makes GORM substitute
	// the column default for the struct's zero value on create, so
	// enabled=false could never be stored (see PatchPolicy.AutoReboot).
	// The API layer applies the safe default instead.
	Enabled     bool       `gorm:"not null" json:"enabled"`
	WindowStart string     `json:"window_start"` // "HH:MM" (local, see Timezone)
	WindowHours int        `json:"window_hours"`
	Timezone    string     `json:"timezone"`
	LastRunAt   *time.Time `json:"last_run_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// RunbookRun records one runbook execution on one device.
type RunbookRun struct {
	ID         string     `gorm:"type:uuid;primaryKey" json:"id"`
	RunbookID  string     `gorm:"type:uuid;not null;index" json:"runbook_id"`
	DeviceID   string     `gorm:"type:uuid;not null;index" json:"device_id"`
	Status     string     `gorm:"not null;default:pending" json:"status"` // pending, running, success, failed
	Output     string     `gorm:"type:text" json:"output"`
	ExitCode   *int       `json:"exit_code"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  time.Time  `json:"created_at"`
}
