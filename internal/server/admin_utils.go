package server

import (
	"fmt"
	"time"
)

type AdminUtils struct {
	startTime time.Time
}

func NewAdminUtils() *AdminUtils {
	return &AdminUtils{
		startTime: time.Now(),
	}
}

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
