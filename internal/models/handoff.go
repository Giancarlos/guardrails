package models

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Handoff status constants
const (
	HandoffPending  = "pending"
	HandoffAccepted = "accepted"
	HandoffRejected = "rejected"
)

// Handoff ID constants
const (
	HandoffIDByteLength = 4
	HandoffIDPrefix     = "hoff-"
)

// Handoff records a task handoff between agents
type Handoff struct {
	ID          string     `gorm:"primaryKey;size:20" json:"id"`
	TaskID      string     `gorm:"size:30;not null;index" json:"task_id"`
	FromAgent   string     `gorm:"size:100;not null" json:"from_agent"`
	ToAgent     string     `gorm:"size:100;not null" json:"to_agent"`
	Summary     string     `gorm:"type:text" json:"summary,omitempty"`
	ContextData string     `gorm:"type:text" json:"context_data,omitempty"`
	Status      string     `gorm:"size:20;default:pending;index" json:"status"`
	CreatedAt   time.Time  `gorm:"autoCreateTime" json:"created_at"`
	AcceptedAt  *time.Time `json:"accepted_at,omitempty"`
}

// TableName specifies the table name for Handoff
func (Handoff) TableName() string {
	return "handoffs"
}

// GenerateHandoffID creates a new hash-based handoff ID like "hoff-a1b2c3d4"
func GenerateHandoffID() string {
	bytes := make([]byte, HandoffIDByteLength)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return HandoffIDPrefix + hex.EncodeToString(bytes)
}

// BeforeCreate hook to generate ID if not set
func (h *Handoff) BeforeCreate(tx *gorm.DB) error {
	if h.ID == "" {
		h.ID = GenerateHandoffID()
	}
	return nil
}

// StatusString returns a human-readable status
func (h *Handoff) StatusString() string {
	switch h.Status {
	case HandoffAccepted:
		return "ACCEPTED"
	case HandoffRejected:
		return "REJECTED"
	default:
		return "PENDING"
	}
}
