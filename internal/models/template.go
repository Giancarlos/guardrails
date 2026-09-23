package models

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Template represents a reusable task template
type Template struct {
	ID          string      `gorm:"primaryKey;size:30" json:"id"`
	Name        string      `gorm:"size:100;uniqueIndex;not null" json:"name"`
	Title       string      `gorm:"size:255" json:"title,omitempty"`
	Description string      `gorm:"type:text" json:"description,omitempty"`
	Priority    int         `json:"priority"`
	Type        string      `gorm:"size:20;default:task" json:"type"`
	Labels      StringSlice `gorm:"type:text" json:"labels,omitempty"`
	Variables   StringSlice `gorm:"type:text" json:"variables,omitempty"`
	CreatedAt   time.Time   `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time   `gorm:"autoUpdateTime" json:"updated_at"`
}

// GenerateTemplateID creates a new template ID
func GenerateTemplateID() string {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		// crypto/rand failure indicates serious system issues - fail fast
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return "tmpl-" + hex.EncodeToString(bytes)
}

// BeforeCreate hook to generate ID if not set
func (t *Template) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = GenerateTemplateID()
	}
	return nil
}

// ToTask creates a new task from this template, substituting {{key}} placeholders
// with values from the vars map. Missing variables are left as-is.
func (t *Template) ToTask(vars map[string]string) *Task {
	title := t.Title
	description := t.Description

	for k, v := range vars {
		placeholder := "{{" + k + "}}"
		title = strings.ReplaceAll(title, placeholder, v)
		description = strings.ReplaceAll(description, placeholder, v)
	}

	task := &Task{
		Title:       title,
		Description: description,
		Priority:    t.Priority,
		Type:        t.Type,
		Labels:      make(StringSlice, len(t.Labels)),
		Status:      StatusOpen,
	}
	copy(task.Labels, t.Labels)
	return task
}
