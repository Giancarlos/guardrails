package models

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// TimeEntry ID constants
const (
	TimeEntryIDByteLength = 4
	TimeEntryIDPrefix     = "time-"
)

// TimeEntry records time spent on a task
type TimeEntry struct {
	ID        string     `gorm:"primaryKey;size:30" json:"id"`
	TaskID    string     `gorm:"size:30;index;not null" json:"task_id"`
	StartedAt time.Time  `json:"started_at"`
	StoppedAt *time.Time `json:"stopped_at,omitempty"`
	Duration  int64      `gorm:"default:0" json:"duration"` // seconds
	Note      string     `gorm:"type:text" json:"note,omitempty"`
	CreatedAt time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

// TableName specifies the table name for TimeEntry
func (TimeEntry) TableName() string {
	return "time_entries"
}

// GenerateTimeEntryID creates a new hash-based time entry ID like "time-a1b2c3d4"
func GenerateTimeEntryID() string {
	bytes := make([]byte, TimeEntryIDByteLength)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return TimeEntryIDPrefix + hex.EncodeToString(bytes)
}

// BeforeCreate hook to generate ID if not set
func (te *TimeEntry) BeforeCreate(tx *gorm.DB) error {
	if te.ID == "" {
		te.ID = GenerateTimeEntryID()
	}
	return nil
}

// FormatDuration returns a human-readable duration string from seconds
func FormatDuration(seconds int64) string {
	if seconds <= 0 {
		return "0m"
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
