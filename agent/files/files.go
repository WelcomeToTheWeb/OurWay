package files

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Handler manages file transfers for the agent.
type Handler struct {
	deviceKey  string
	serverURL  string
	httpClient *http.Client
}

// NewHandler creates a new file transfer handler.
func NewHandler(deviceKey, serverURL string) *Handler {
	return &Handler{
		deviceKey: deviceKey,
		serverURL: strings.TrimSuffix(serverURL, "/"),
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
		},
	}
}

// HandlePush receives a file push command and downloads the file to the device.
func (h *Handler) HandlePush(ctx context.Context, data interface{}) {
	var payload map[string]interface{}
	if b, err := json.Marshal(data); err == nil {
		if err := json.Unmarshal(b, &payload); err != nil {
			log.Printf("file push: failed to parse payload: %v", err)
			return
		}
	}

	transferID, _ := payload["transfer_id"].(string)
	filename, _ := payload["filename"].(string)
	destination, _ := payload["destination"].(string)

	if destination == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			destination = "/tmp"
		} else {
			destination = home
		}
	}

	log.Printf("file push: %s -> %s/%s", transferID, destination, filename)

	// Download file from server
	downloadURL := fmt.Sprintf("%s/api/agent/files/%s/download", h.serverURL, transferID)
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		log.Printf("file push: create request error: %v", err)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("create request: %v", err))
		return
	}
	req.Header.Set("X-Device-Key", h.deviceKey)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("file push: download error: %v", err)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("download: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("file push: download returned %d", resp.StatusCode)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("download returned %d", resp.StatusCode))
		return
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(destination, 0755); err != nil {
		log.Printf("file push: mkdir error: %v", err)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("create destination: %v", err))
		return
	}

	// Sanitize the filename: only the base name may be used, and the final
	// path must stay inside the destination directory (path traversal guard).
	safeName := filepath.Base(filename)
	if safeName == "" || safeName == "." || safeName == ".." {
		log.Printf("file push: refusing invalid filename %q", filename)
		h.reportProgress(transferID, "failed", 0, "invalid filename")
		return
	}
	destDir, err := filepath.Abs(destination)
	if err != nil {
		log.Printf("file push: resolve destination error: %v", err)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("resolve destination: %v", err))
		return
	}
	destPath := filepath.Join(destDir, safeName)
	if !strings.HasPrefix(destPath, destDir+string(os.PathSeparator)) {
		log.Printf("file push: refusing path escape %q", destPath)
		h.reportProgress(transferID, "failed", 0, "invalid destination")
		return
	}

	// Write file to destination
	out, err := os.Create(destPath)
	if err != nil {
		log.Printf("file push: create file error: %v", err)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("create file: %v", err))
		return
	}
	defer out.Close()

	// Report progress as we copy
	var written int64
	total := resp.ContentLength
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				log.Printf("file push: write error: %v", werr)
				h.reportProgress(transferID, "failed", 0, fmt.Sprintf("write file: %v", werr))
				return
			}
			written += int64(n)
			if total > 0 {
				progress := int(float64(written) / float64(total) * 100)
				log.Printf("file push: %d%%", progress)
				h.reportProgress(transferID, "transferring", progress, "")
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("file push: read error: %v", err)
			h.reportProgress(transferID, "failed", 0, fmt.Sprintf("read download: %v", err))
			return
		}
	}

	log.Printf("file push: completed %s (%d bytes)", destPath, written)
	h.reportProgress(transferID, "completed", 100, "")
}

// reportProgress updates the transfer status on the server.
func (h *Handler) reportProgress(transferID string, status string, progress int, errMsg string) {
	payload := map[string]interface{}{
		"transfer_id":   transferID,
		"status":        status,
		"progress":      progress,
		"error_message": errMsg,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}

	req, err := http.NewRequest("POST", h.serverURL+"/api/agent/files/status", strings.NewReader(string(b)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Key", h.deviceKey)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("report_progress: %v", err)
		return
	}
	resp.Body.Close()
}

// HandlePull receives a file pull command and uploads the file from the device.
func (h *Handler) HandlePull(ctx context.Context, data interface{}) {
	var payload map[string]interface{}
	if b, err := json.Marshal(data); err == nil {
		if err := json.Unmarshal(b, &payload); err != nil {
			log.Printf("file pull: failed to parse payload: %v", err)
			return
		}
	}

	transferID, _ := payload["transfer_id"].(string)
	sourcePath, _ := payload["source_path"].(string)

	log.Printf("file pull: %s from %s", transferID, sourcePath)

	// Read file from device
	f, err := os.Open(sourcePath)
	if err != nil {
		log.Printf("file pull: open file error: %v", err)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("open source file: %v", err))
		return
	}
	defer f.Close()

	// Get file info
	info, err := f.Stat()
	if err != nil {
		log.Printf("file pull: stat file error: %v", err)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("stat source file: %v", err))
		return
	}
	size := info.Size()

	// Build multipart upload
	var body bytes.Buffer
	writer := NewMultipartWriter(&body)
	part, _ := writer.CreateFormFile("file", filepath.Base(sourcePath))
	_, err = io.Copy(part, f)
	if err != nil {
		log.Printf("file pull: copy error: %v", err)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("read source file: %v", err))
		return
	}
	writer.Close()

	// Upload to server
	uploadURL := fmt.Sprintf("%s/api/agent/files/%s/upload", h.serverURL, transferID)
	req, err := http.NewRequest("POST", uploadURL, &body)
	if err != nil {
		log.Printf("file pull: create request error: %v", err)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("create request: %v", err))
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Device-Key", h.deviceKey)
	// Use the full multipart body length, not the raw file size — setting
	// ContentLength to the raw size would truncate the trailing bytes of the
	// upload (multipart boundary + terminator + file tail) in flight.
	req.ContentLength = int64(body.Len())

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("file pull: upload error: %v", err)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("upload: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("file pull: upload returned %d", resp.StatusCode)
		h.reportProgress(transferID, "failed", 0, fmt.Sprintf("upload returned %d", resp.StatusCode))
		return
	}

	log.Printf("file pull: completed %s (%d bytes)", sourcePath, size)
	h.reportProgress(transferID, "completed", 100, "")
}

// Simple multipart writer to avoid extra dependencies
type MultipartWriter struct {
	w        io.Writer
	boundary string
}

func NewMultipartWriter(w io.Writer) *MultipartWriter {
	return &MultipartWriter{
		w:        w,
		boundary: "multipart-form-data-ourway",
	}
}

func (mw *MultipartWriter) CreateFormFile(fieldname, filename string) (io.Writer, error) {
	fmt.Fprintf(mw.w, "--%s\r\n", mw.boundary)
	fmt.Fprintf(mw.w, "Content-Disposition: form-data; name=\"%s\"; filename=\"%s\"\r\n", fieldname, filename)
	fmt.Fprintf(mw.w, "Content-Type: application/octet-stream\r\n\r\n")
	return mw.w, nil
}

func (mw *MultipartWriter) FormDataContentType() string {
	return "multipart/form-data; boundary=" + mw.boundary
}

func (mw *MultipartWriter) Close() error {
	fmt.Fprintf(mw.w, "--%s--\r\n", mw.boundary)
	return nil
}
