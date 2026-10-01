package models

// Metrics holds system metrics reported by an agent for a device.
type Metrics struct {
	CPU       float64 `json:"cpu"`
	RAM       float64 `json:"ram"`
	RAMUsed   uint64  `json:"ram_used"`
	RAMTotal  uint64  `json:"ram_total"`
	DiskUsage float64 `json:"disk_usage"`
	DiskUsed  uint64  `json:"disk_used"`
	DiskTotal uint64  `json:"disk_total"`
	NetIn     uint64  `json:"net_in"`
	NetOut    uint64  `json:"net_out"`
	Uptime    uint64  `json:"uptime"`
	Processes int     `json:"processes"`
}
