package models

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Checkpoint ID constants
const (
	CheckpointIDByteLength = 4
	CheckpointIDPrefix     = "chk-"
)

// Checkpoint records a saved state for a task (immutable audit record)
type Checkpoint struct {
	ID        string    `gorm:"primaryKey;size:20" json:"id"`
	TaskID    string    `gorm:"size:30;not null;index" json:"task_id"`
	AgentID   string    `gorm:"size:100" json:"agent_id,omitempty"`
	StateText string    `gorm:"type:text" json:"state_text,omitempty"`
	StateJSON string    `gorm:"type:text" json:"state_json,omitempty"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName specifies the table name for Checkpoint
func (Checkpoint) TableName() string {
	return "checkpoints"
}

// GenerateCheckpointID creates a new hash-based checkpoint ID like "chk-a1b2c3d4"
func GenerateCheckpointID() string {
	bytes := make([]byte, CheckpointIDByteLength)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return CheckpointIDPrefix + hex.EncodeToString(bytes)
}

// BeforeCreate hook to generate ID if not set
func (c *Checkpoint) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = GenerateCheckpointID()
	}
	return nil
}
