package models

import (
	"crypto/rand"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"gorm.io/gorm"
)

// Task status constants
const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusClosed     = "closed"
	StatusArchived   = "archived"
)

// Task source constants
const (
	SourceLocal  = "local"
	SourceGitHub = "github"
	SourceBeads  = "beads"
)

// Task type constants
const (
	TypeTask    = "task"
	TypeBug     = "bug"
	TypeFeature = "feature"
	TypeEpic    = "epic"
)

// Priority constants
const (
	PriorityCritical = 0
	PriorityHigh     = 1
	PriorityMedium   = 2
	PriorityLow      = 3
	PriorityLowest   = 4
)

// Date format constants
const (
	DateTimeFormat      = "2006-01-02 15:04:05"
	DateTimeShortFormat = "2006-01-02 15:04"
)

// ID generation constants
const (
	IDByteLength = 4
	IDPrefix     = "gur-"
)

// ID validation pattern for task IDs (supports hierarchical IDs like gur-abc12345.1.2)
var taskIDPattern = regexp.MustCompile(`^gur-[a-f0-9]{8}(\.\d+)*$`)

// ValidateTaskID validates that a task ID has the correct format
func ValidateTaskID(id string) bool {
	return taskIDPattern.MatchString(id)
}

// Task represents a task/issue in the system
type Task struct {
	ID          string         `gorm:"primaryKey;size:30" json:"id"`
	ParentID    string         `gorm:"size:30;index" json:"parent_id,omitempty"`
	Title       string         `gorm:"size:255;not null" json:"title"`
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Status      string         `gorm:"size:20;default:open;index;index:idx_status_priority" json:"status"`
	Priority    int            `gorm:"index;index:idx_status_priority" json:"priority"` // 0=highest, 4=lowest
	Type        string         `gorm:"size:20;default:task;index" json:"type"`
	Labels      StringSlice    `gorm:"type:text" json:"labels,omitempty"`
	Assignee    string         `gorm:"size:100;index" json:"assignee,omitempty"`
	Notes       string         `gorm:"type:text" json:"notes,omitempty"`
	CloseReason string         `gorm:"size:255" json:"close_reason,omitempty"`
	Summary     string         `gorm:"type:text" json:"summary,omitempty"`
	Compacted   bool           `gorm:"default:false" json:"compacted"`
	Synced      bool           `gorm:"default:false;index" json:"synced"`
	Source      string         `gorm:"size:20;default:local;index" json:"source"` // local, github, or beads
	SourceID    *string        `gorm:"size:64;uniqueIndex" json:"source_id,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	ClosedAt    *time.Time     `json:"closed_at,omitempty"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// StringSlice is a custom type for storing string slices as JSON in the database
type StringSlice []string

// Scan implements the sql.Scanner interface
func (s *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = []string{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("StringSlice.Scan: unexpected type %T", value)
		}
		bytes = []byte(str)
	}
	if len(bytes) == 0 {
		*s = []string{}
		return nil
	}
	if err := json.Unmarshal(bytes, s); err != nil {
		return fmt.Errorf("StringSlice.Scan: invalid JSON: %w", err)
	}
	return nil
}

// Value implements the driver.Valuer interface
func (s StringSlice) Value() (driver.Value, error) {
	if len(s) == 0 {
		return "[]", nil
	}
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return string(bytes), nil
}

// GenerateID creates a new hash-based task ID like "gur-a1b2c3d4"
func GenerateID() string {
	bytes := make([]byte, IDByteLength)
	if _, err := rand.Read(bytes); err != nil {
		// crypto/rand failure indicates serious system issues - fail fast
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return IDPrefix + hex.EncodeToString(bytes)
}

// GenerateSubtaskID creates a hierarchical subtask ID like "gur-a1b2c3d4.1"
func GenerateSubtaskID(parentID string, subtaskNumber int) string {
	return fmt.Sprintf("%s.%d", parentID, subtaskNumber)
}

// GetRootID extracts the root task ID from a hierarchical ID
func GetRootID(id string) string {
	// Find first dot after the base ID
	for i := len(IDPrefix) + 8; i < len(id); i++ {
		if id[i] == '.' {
			return id[:i]
		}
	}
	return id
}

// GetParentID returns the parent ID for a hierarchical task ID
func GetParentID(id string) string {
	// Find last dot
	lastDot := -1
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '.' {
			lastDot = i
			break
		}
	}
	if lastDot == -1 {
		return "" // No parent (root task)
	}
	return id[:lastDot]
}

// GetDepth returns the nesting depth (0 for root tasks)
func GetDepth(id string) int {
	depth := 0
	for _, c := range id {
		if c == '.' {
			depth++
		}
	}
	return depth
}

// BeforeCreate hook to generate ID if not set
func (t *Task) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = GenerateID()
	}
	return nil
}

// AfterDelete hook to clean up orphaned dependencies when a task is deleted
func (t *Task) AfterDelete(tx *gorm.DB) error {
	// Soft-delete all dependencies where this task is the parent (blocker)
	if err := tx.Where("parent_id = ?", t.ID).Delete(&Dependency{}).Error; err != nil {
		return err
	}
	// Soft-delete all dependencies where this task is the child (blocked)
	if err := tx.Where("child_id = ?", t.ID).Delete(&Dependency{}).Error; err != nil {
		return err
	}
	return nil
}

// IsClosed returns true if the task is closed
func (t *Task) IsClosed() bool {
	return t.Status == StatusClosed
}

// IsArchived returns true if the task is archived
func (t *Task) IsArchived() bool {
	return t.Status == StatusArchived
}

// Archive marks the task as archived
func (t *Task) Archive() {
	t.Status = StatusArchived
}

// Unarchive restores an archived task to closed status
func (t *Task) Unarchive() {
	t.Status = StatusClosed
}

// Compact generates a summary and clears verbose fields
func (t *Task) Compact() {
	if t.Compacted {
		return
	}
	// Generate summary from available info
	summary := t.Title
	if t.CloseReason != "" {
		summary += " | Closed: " + t.CloseReason
	}
	if t.Type != TypeTask {
		summary = "[" + t.Type + "] " + summary
	}
	t.Summary = summary
	t.Description = ""
	t.Notes = ""
	t.Compacted = true
}

// Close marks the task as closed with the given reason
func (t *Task) Close(reason string) {
	t.Status = StatusClosed
	t.CloseReason = reason
	now := time.Now()
	t.ClosedAt = &now
}

// Reopen reopens a closed task
func (t *Task) Reopen() {
	t.Status = StatusOpen
	t.CloseReason = ""
	t.ClosedAt = nil
}

// AddLabel adds a label if it doesn't already exist
func (t *Task) AddLabel(label string) {
	for _, l := range t.Labels {
		if l == label {
			return
		}
	}
	t.Labels = append(t.Labels, label)
}

// RemoveLabel removes a label if it exists
func (t *Task) RemoveLabel(label string) {
	for i, l := range t.Labels {
		if l == label {
			t.Labels = append(t.Labels[:i], t.Labels[i+1:]...)
			return
		}
	}
}

// AppendNotes appends a timestamped note to the notes field
func (t *Task) AppendNotes(note string) {
	timestamp := time.Now().Format(DateTimeFormat)
	entry := "[" + timestamp + "] " + note + "\n"
	t.Notes += entry
}

// PriorityString returns a human-readable priority string
func (t *Task) PriorityString() string {
	switch t.Priority {
	case PriorityCritical:
		return "P0 (Critical)"
	case PriorityHigh:
		return "P1 (High)"
	case PriorityMedium:
		return "P2 (Medium)"
	case PriorityLow:
		return "P3 (Low)"
	case PriorityLowest:
		return "P4 (Lowest)"
	default:
		return "Unknown"
	}
}
