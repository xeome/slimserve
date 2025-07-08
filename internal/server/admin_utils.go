package server

import (
	"fmt"
	"time"
)

// AdminUtils provides utility functions for admin operations
type AdminUtils struct {
	startTime time.Time
}

// NewAdminUtils creates a new AdminUtils instance
func NewAdminUtils() *AdminUtils {
	return &AdminUtils{
		startTime: time.Now(),
	}
}

// formatBytes formats byte counts into human-readable strings
func (au *AdminUtils) formatBytes(bytes uint64) string {
	if bytes == 0 {
		return "0 B"
	}
	
	const unit = 1024
	sizes := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	
	i := 0
	size := float64(bytes)
	for size >= unit && i < len(sizes)-1 {
		size /= unit
		i++
	}
	
	return fmt.Sprintf("%.1f %s", size, sizes[i])
}

// GetUptime returns the server uptime as a formatted string
func (au *AdminUtils) GetUptime() string {
	uptime := time.Since(au.startTime)
	
	days := int(uptime.Hours()) / 24
	hours := int(uptime.Hours()) % 24
	minutes := int(uptime.Minutes()) % 60
	
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	} else {
		return fmt.Sprintf("%dm", minutes)
	}
}

// GetUptimeSeconds returns the server uptime in seconds
func (au *AdminUtils) GetUptimeSeconds() int64 {
	return int64(time.Since(au.startTime).Seconds())
}
