package admin

import (
	"sync"
	"time"
)

const (
	ActivityLogin  = "login"
	ActivityUpload = "upload"
	ActivityConfig = "config"
	ActivityDelete = "delete"
	ActivityMkdir  = "mkdir"
)

type ActivityEntry struct {
	ID          int       `json:"id"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
	IP          string    `json:"ip"`
	Details     string    `json:"details,omitempty"`
}

type ActivityStore struct {
	mu         sync.RWMutex
	activities []ActivityEntry
	nextID     int
	maxEntries int
}

func NewActivityStore(maxEntries int) *ActivityStore {
	return &ActivityStore{
		activities: make([]ActivityEntry, 0, maxEntries),
		nextID:     1,
		maxEntries: maxEntries,
	}
}

func (as *ActivityStore) AddActivity(activityType, description, ip, details string) {
	as.mu.Lock()
	defer as.mu.Unlock()

	entry := ActivityEntry{
		ID:          as.nextID,
		Type:        activityType,
		Description: description,
		Timestamp:   time.Now(),
		IP:          ip,
		Details:     details,
	}

	as.activities = append(as.activities, entry)
	as.nextID++

	if len(as.activities) > as.maxEntries {
		as.activities = as.activities[len(as.activities)-as.maxEntries:]
	}
}

func (as *ActivityStore) GetRecentActivities(limit int) []ActivityEntry {
	as.mu.RLock()
	defer as.mu.RUnlock()

	if limit <= 0 || limit > len(as.activities) {
		limit = len(as.activities)
	}

	result := make([]ActivityEntry, limit)
	for i := 0; i < limit; i++ {
		result[i] = as.activities[len(as.activities)-1-i]
	}

	return result
}

func (as *ActivityStore) CountUploadsToday() int {
	today := time.Now().Truncate(24 * time.Hour)
	count := 0

	as.mu.RLock()
	defer as.mu.RUnlock()

	for _, activity := range as.activities {
		if activity.Type == ActivityUpload && activity.Timestamp.After(today) {
			count++
		}
	}

	return count
}

type UploadManager struct {
	mu            sync.RWMutex
	activeUploads map[string]*UploadProgress
	maxConcurrent int
}

type UploadProgress struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	TotalSize int64     `json:"total_size"`
	Uploaded  int64     `json:"uploaded"`
	Status    string    `json:"status"`
	StartTime time.Time `json:"start_time"`
	Error     string    `json:"error,omitempty"`
}

func NewUploadManager(maxConcurrent int) *UploadManager {
	return &UploadManager{
		activeUploads: make(map[string]*UploadProgress),
		maxConcurrent: maxConcurrent,
	}
}

func (um *UploadManager) ActiveUploadsCount() int {
	um.mu.RLock()
	defer um.mu.RUnlock()
	return len(um.activeUploads)
}

func (um *UploadManager) GetActiveUploads() []*UploadProgress {
	um.mu.RLock()
	defer um.mu.RUnlock()

	var uploads []*UploadProgress
	for _, upload := range um.activeUploads {
		uploads = append(uploads, upload)
	}
	return uploads
}

func (um *UploadManager) GetMaxConcurrent() int {
	um.mu.RLock()
	defer um.mu.RUnlock()
	return um.maxConcurrent
}
