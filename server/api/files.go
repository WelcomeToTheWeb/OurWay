package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ourway/server/files"
	"ourway/server/models"
	"ourway/server/store"
)

// FileHandler handles file transfer API endpoints.
type FileHandler struct {
	store   *store.Store
	service *files.Service
}

// NewFileHandler creates a new file handler.
func NewFileHandler(store *store.Store, service *files.Service) *FileHandler {
	return &FileHandler{
		store:   store,
		service: service,
	}
}

// UploadFile handles file upload from the client.
// POST /api/files/upload
func (h *FileHandler) UploadFile(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "no file provided"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to open file"})
		return
	}
	defer f.Close()

	// Read entire file
	data := make([]byte, file.Size)
	if _, err := f.Read(data); err != nil {
		c.JSON(500, gin.H{"error": "failed to read file"})
		return
	}

	// Store the bytes under a new transfer ID and create the transfer row
	// so that PushFile can find it (it looks transfers up by this ID).
	transferID := uuid.New().String()
	if _, err := h.service.StoreFile(transferID, data); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// The model has no dedicated uploader column: record the uploading user
	// in DeviceID (a real UUID) until a push assigns an actual device.
	uploader := c.GetString("user_id")
	if uploader == "" {
		uploader = "00000000-0000-0000-0000-000000000000"
	}
	transfer := &models.FileTransfer{
		ID:        transferID,
		DeviceID:  uploader,
		Filename:  file.Filename,
		SizeBytes: file.Size,
		Status:    "pending",
		Direction: "push",
	}
	if err := h.service.CreateTransfer(transfer); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"transfer_id": transferID,
		"filename":    file.Filename,
		"size_bytes":  file.Size,
	})
}

// PushFile initiates a file push to device(s).
// POST /api/files/push
func (h *FileHandler) PushFile(c *gin.Context) {
	var req struct {
		TransferID  string   `json:"transfer_id" binding:"required"`
		DeviceIDs   []string `json:"device_ids" binding:"required"`
		Destination string   `json:"destination"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	// Get transfer details
	transfer, err := h.service.GetTransfer(req.TransferID)
	if err != nil {
		// Transfer might not exist yet (just uploaded), create it
		transfer = nil
	}

	for _, deviceID := range req.DeviceIDs {
		dest := req.Destination
		if dest == "" {
			dest = "/home/" // default destination
		}

		var transferID string
		var filename string
		var size int64

		if transfer != nil {
			transferID = transfer.ID
			filename = transfer.Filename
			size = transfer.SizeBytes
		} else {
			transferID = uuid.New().String()
			filename = "uploaded_file"
			size = 0
		}

		_, err := h.service.PushFile(c.Request.Context(), transferID, deviceID, dest, filename, size)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(200, gin.H{"status": "push_initiated", "device_count": len(req.DeviceIDs)})
}

// PullFile initiates a file pull from a device.
// POST /api/files/pull
func (h *FileHandler) PullFile(c *gin.Context) {
	var req struct {
		DeviceID   string `json:"device_id" binding:"required"`
		SourcePath string `json:"source_path" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	transfer, err := h.service.PullFile(c.Request.Context(), req.DeviceID, req.SourcePath)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"transfer_id": transfer.ID, "status": "pull_initiated"})
}

// authorizeAgentDevice validates the X-Device-Key header and returns the
// registered device, following the pattern of the other /api/agent/* handlers.
func (h *AgentFileHandler) authorizeAgentDevice(c *gin.Context) *models.Device {
	deviceKey := c.GetHeader("X-Device-Key")
	if deviceKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing device key"})
		return nil
	}
	device, err := h.store.Devices.GetByKey(deviceKey)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid device key"})
		return nil
	}
	return device
}

// DownloadForAgent serves a file to the agent.
// GET /api/agent/files/:transfer_id/download
func (h *AgentFileHandler) DownloadForAgent(c *gin.Context) {
	device := h.authorizeAgentDevice(c)
	if device == nil {
		return
	}

	transferID := c.Param("transfer_id")

	// Verify transfer exists and belongs to this device
	transfer, err := h.service.GetTransfer(transferID)
	if err != nil {
		c.JSON(404, gin.H{"error": "transfer not found"})
		return
	}
	if transfer.DeviceID != device.ID {
		c.JSON(404, gin.H{"error": "transfer not found"})
		return
	}

	// Get the file path
	filePath, err := h.service.GetFile(transferID)
	if err != nil {
		c.JSON(404, gin.H{"error": "file not found"})
		return
	}

	c.File(filePath)
}

// UploadFromAgent handles file upload from agent (for pull transfers).
// POST /api/agent/files/:transfer_id/upload
func (h *AgentFileHandler) UploadFromAgent(c *gin.Context) {
	device := h.authorizeAgentDevice(c)
	if device == nil {
		return
	}

	transferID := c.Param("transfer_id")

	// Verify transfer exists and belongs to this device
	if transfer, err := h.service.GetTransfer(transferID); err != nil {
		c.JSON(404, gin.H{"error": "transfer not found"})
		return
	} else if transfer.DeviceID != device.ID {
		c.JSON(404, gin.H{"error": "transfer not found"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "no file provided"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to open file"})
		return
	}
	defer f.Close()

	data := make([]byte, file.Size)
	if _, err := f.Read(data); err != nil {
		c.JSON(500, gin.H{"error": "failed to read file"})
		return
	}

	// Store the file under the transfer ID the agent was told about
	if _, err := h.service.StoreFile(transferID, data); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Update transfer status
	h.service.UpdateStatus(transferID, "completed", 100, "")

	c.JSON(200, gin.H{"status": "uploaded"})
}

// ListTransfers returns recent file transfers.
// GET /api/files/transfers
func (h *FileHandler) ListTransfers(c *gin.Context) {
	transfers, err := h.service.ListTransfers()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"transfers": transfers})
}

// GetTransfer returns details for a specific transfer.
// GET /api/files/transfers/:id
func (h *FileHandler) GetTransfer(c *gin.Context) {
	transferID := c.Param("id")

	transfer, err := h.service.GetTransfer(transferID)
	if err != nil {
		c.JSON(404, gin.H{"error": "transfer not found"})
		return
	}

	c.JSON(200, gin.H{"transfer": transfer})
}

// DownloadFile serves a retrieved file to the user.
// GET /api/files/:transfer_id/file
func (h *FileHandler) DownloadFile(c *gin.Context) {
	transferID := c.Param("transfer_id")

	// Get transfer for filename
	transfer, err := h.service.GetTransfer(transferID)
	if err != nil {
		c.JSON(404, gin.H{"error": "transfer not found"})
		return
	}

	// Get the file
	filePath, err := h.service.GetFile(transferID)
	if err != nil {
		c.JSON(404, gin.H{"error": "file not found"})
		return
	}

	// Set download headers
	c.Header("Content-Disposition", "attachment; filename="+transfer.Filename)
	c.File(filePath)
}

// Cleanup periodically removes old uploaded files.
func (h *FileHandler) Cleanup() {
	for {
		h.service.CleanupOldFiles(24 * time.Hour)
		time.Sleep(time.Hour)
	}
}
