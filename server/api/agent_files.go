package api

import (
	"github.com/gin-gonic/gin"

	"ourway/server/files"
	"ourway/server/store"
)

// AgentFileHandler handles agent file status endpoints.
type AgentFileHandler struct {
	store   *store.Store
	service *files.Service
}

// NewAgentFileHandler creates a new agent file handler.
func NewAgentFileHandler(store *store.Store, service *files.Service) *AgentFileHandler {
	return &AgentFileHandler{
		store:   store,
		service: service,
	}
}

// ReportStatus receives a transfer status update from an agent.
// POST /api/agent/files/status
func (h *AgentFileHandler) ReportStatus(c *gin.Context) {
	var req struct {
		TransferID   string `json:"transfer_id" binding:"required"`
		Status       string `json:"status" binding:"required"`
		Progress     int    `json:"progress"`
		ErrorMessage string `json:"error_message"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	if err := h.service.UpdateStatus(req.TransferID, req.Status, req.Progress, req.ErrorMessage); err != nil {
		c.JSON(500, gin.H{"error": "failed to update status"})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}
