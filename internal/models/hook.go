package models

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Valid hook events
const (
	HookEventOnCreate = "on-create"
	HookEventOnUpdate = "on-update"
	HookEventOnClose  = "on-close"
	HookEventOnReopen = "on-reopen"
)

// ValidHookEvents contains all valid hook event names
var ValidHookEvents = map[string]bool{
	HookEventOnCreate: true,
	HookEventOnUpdate: true,
	HookEventOnClose:  true,
	HookEventOnReopen: true,
}

// Hook represents a webhook/hook that fires on task events
type Hook struct {
	ID        string    `gorm:"primaryKey;size:30" json:"id"`
	Event     string    `gorm:"size:50;index;not null" json:"event"` // on-create, on-update, on-close, on-reopen
	Command   string    `gorm:"type:text;not null" json:"command"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// GenerateHookID creates a new hook ID
func GenerateHookID() string {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return "hook-" + hex.EncodeToString(bytes)
}

// BeforeCreate hook to generate ID if not set
func (h *Hook) BeforeCreate(tx *gorm.DB) error {
	if h.ID == "" {
		h.ID = GenerateHookID()
	}
	return nil
}

// IsValidHookEvent checks if an event name is valid
func IsValidHookEvent(event string) bool {
	return ValidHookEvents[event]
}
