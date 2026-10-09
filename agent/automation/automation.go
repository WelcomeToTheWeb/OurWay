// Package automation executes runbook commands pushed by the server and
// reports the result back over HTTP.
package automation

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// maxOutput caps what is reported back so a chatty command cannot flood
// the database (the server truncates again at 64 KB).
const maxOutput = 65536

// Handler runs automation commands for the agent.
type Handler struct {
	deviceKey  string
	deviceID   string
	serverURL  string
	httpClient *http.Client
}

// NewHandler creates an automation handler. deviceID is this agent's own
// device ID; server payloads addressed to a different device are refused
// (defense in depth — the WebSocket connection is key-authenticated).
func NewHandler(deviceKey, serverURL, deviceID string) *Handler {
	return &Handler{
		deviceKey:  deviceKey,
		deviceID:   deviceID,
		serverURL:  strings.TrimSuffix(serverURL, "/ws"),
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// RunCommand executes a runbook command and reports the result.
// The command runs with the system shell (sh -c / cmd /c), capped at 10
// minutes; the timeout kills the process and reports failure.
func (h *Handler) RunCommand(data interface{}) {
	var payload map[string]interface{}
	if b, err := json.Marshal(data); err == nil {
		if err := json.Unmarshal(b, &payload); err != nil {
			log.Printf("run_command: failed to parse payload: %v", err)
			return
		}
	}
	runID, _ := payload["run_id"].(string)
	deviceID, _ := payload["device_id"].(string)
	command, _ := payload["command"].(string)
	if runID == "" || command == "" {
		log.Printf("run_command: missing run_id or command; ignoring")
		return
	}
	if h.deviceID != "" && deviceID != h.deviceID {
		log.Printf("run_command: payload device_id %q is not this device; ignoring", deviceID)
		return
	}

	log.Printf("run_command: executing run %s", runID)
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	exitCode := 0
	success := err == nil
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	output := out.String()
	if len(output) > maxOutput {
		output = output[:maxOutput]
	}
	h.reportResult(runID, success, exitCode, output)
}

// reportResult posts the outcome to /api/agent/runbooks/result.
func (h *Handler) reportResult(runID string, success bool, exitCode int, output string) {
	body, err := json.Marshal(map[string]interface{}{
		"run_id":    runID,
		"device_id": h.deviceID,
		"success":   success,
		"exit_code": exitCode,
		"output":    output,
	})
	if err != nil {
		log.Printf("run_command: failed to marshal result: %v", err)
		return
	}
	req, err := http.NewRequest("POST", h.serverURL+"/api/agent/runbooks/result", bytes.NewReader(body))
	if err != nil {
		log.Printf("run_command: failed to build result request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Key", h.deviceKey)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("run_command: failed to report result for run %s: %v", runID, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("run_command: server rejected result for run %s: %s", runID, resp.Status)
	}
	if !success {
		log.Printf("run_command: run %s failed (exit %d): %s", runID, exitCode, firstLine(output))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
