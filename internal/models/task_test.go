package models

import (
	"testing"
	"time"
)

// StringSlice serialization tests

func TestStringSliceScan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []string
		wantErr  bool
	}{
		{"nil value", nil, []string{}, false},
		{"empty bytes", []byte{}, []string{}, false},
		{"empty string", "", []string{}, false},
		{"empty array", []byte("[]"), []string{}, false},
		{"single item", []byte(`["bug"]`), []string{"bug"}, false},
		{"multiple items", []byte(`["bug","urgent","security"]`), []string{"bug", "urgent", "security"}, false},
		{"special chars", []byte(`["label with spaces","quote\"here"]`), []string{"label with spaces", `quote"here`}, false},
		{"unicode", []byte(`["日本語","émoji 🎉"]`), []string{"日本語", "émoji 🎉"}, false},
		{"invalid json", []byte(`not json`), nil, true},
		{"wrong type int", 123, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s StringSlice
			err := s.Scan(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Scan() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Scan() unexpected error: %v", err)
				return
			}

			if len(s) != len(tt.expected) {
				t.Errorf("Scan() len = %d, want %d", len(s), len(tt.expected))
				return
			}

			for i, v := range s {
				if v != tt.expected[i] {
					t.Errorf("Scan()[%d] = %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestStringSliceValue(t *testing.T) {
	tests := []struct {
		name     string
		input    StringSlice
		expected string
	}{
		{"nil slice", nil, "[]"},
		{"empty slice", StringSlice{}, "[]"},
		{"single item", StringSlice{"bug"}, `["bug"]`},
		{"multiple items", StringSlice{"bug", "urgent"}, `["bug","urgent"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.input.Value()
			if err != nil {
				t.Errorf("Value() error: %v", err)
				return
			}

			str, ok := val.(string)
			if !ok {
				t.Errorf("Value() type = %T, want string", val)
				return
			}

			if str != tt.expected {
				t.Errorf("Value() = %q, want %q", str, tt.expected)
			}
		})
	}
}

func TestStringSliceRoundTrip(t *testing.T) {
	original := StringSlice{"bug", "urgent", "label with spaces", "quote\"here"}

	// Serialize
	val, err := original.Value()
	if err != nil {
		t.Fatalf("Value() error: %v", err)
	}

	// Deserialize
	var restored StringSlice
	err = restored.Scan([]byte(val.(string)))
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	// Compare
	if len(restored) != len(original) {
		t.Fatalf("Round-trip len = %d, want %d", len(restored), len(original))
	}

	for i, v := range restored {
		if v != original[i] {
			t.Errorf("Round-trip[%d] = %q, want %q", i, v, original[i])
		}
	}
}

func TestGenerateID(t *testing.T) {
	id := GenerateID()

	if !ValidateTaskID(id) {
		t.Errorf("GenerateID() produced invalid ID: %s", id)
	}

	if len(id) != 12 { // "gur-" + 8 hex chars
		t.Errorf("GenerateID() wrong length: got %d, want 12", len(id))
	}

	// Test uniqueness
	id2 := GenerateID()
	if id == id2 {
		t.Error("GenerateID() produced duplicate IDs")
	}
}

func TestValidateTaskID(t *testing.T) {
	tests := []struct {
		id    string
		valid bool
	}{
		{"gur-a1b2c3d4", true},
		{"gur-00000000", true},
		{"gur-ffffffff", true},
		{"gur-a1b2c3d4.1", true},
		{"gur-a1b2c3d4.1.2", true},
		{"gur-a1b2c3d4.1.2.3", true},
		{"", false},
		{"gur-", false},
		{"gur-abc", false},        // too short
		{"gur-a1b2c3d4g", false},  // invalid hex
		{"gur-A1B2C3D4", false},   // uppercase
		{"task-a1b2c3d4", false},  // wrong prefix
		{"gur-a1b2c3d4.", false},  // trailing dot
		{"gur-a1b2c3d4.a", false}, // non-numeric subtask
	}

	for _, tt := range tests {
		got := ValidateTaskID(tt.id)
		if got != tt.valid {
			t.Errorf("ValidateTaskID(%q) = %v, want %v", tt.id, got, tt.valid)
		}
	}
}

func TestGenerateSubtaskID(t *testing.T) {
	parent := "gur-a1b2c3d4"

	subtask1 := GenerateSubtaskID(parent, 1)
	if subtask1 != "gur-a1b2c3d4.1" {
		t.Errorf("GenerateSubtaskID() = %s, want gur-a1b2c3d4.1", subtask1)
	}

	subtask2 := GenerateSubtaskID(subtask1, 2)
	if subtask2 != "gur-a1b2c3d4.1.2" {
		t.Errorf("GenerateSubtaskID() = %s, want gur-a1b2c3d4.1.2", subtask2)
	}
}

func TestGetRootID(t *testing.T) {
	tests := []struct {
		id   string
		root string
	}{
		{"gur-a1b2c3d4", "gur-a1b2c3d4"},
		{"gur-a1b2c3d4.1", "gur-a1b2c3d4"},
		{"gur-a1b2c3d4.1.2", "gur-a1b2c3d4"},
		{"gur-a1b2c3d4.1.2.3", "gur-a1b2c3d4"},
	}

	for _, tt := range tests {
		got := GetRootID(tt.id)
		if got != tt.root {
			t.Errorf("GetRootID(%q) = %q, want %q", tt.id, got, tt.root)
		}
	}
}

func TestGetParentID(t *testing.T) {
	tests := []struct {
		id     string
		parent string
	}{
		{"gur-a1b2c3d4", ""},
		{"gur-a1b2c3d4.1", "gur-a1b2c3d4"},
		{"gur-a1b2c3d4.1.2", "gur-a1b2c3d4.1"},
		{"gur-a1b2c3d4.1.2.3", "gur-a1b2c3d4.1.2"},
	}

	for _, tt := range tests {
		got := GetParentID(tt.id)
		if got != tt.parent {
			t.Errorf("GetParentID(%q) = %q, want %q", tt.id, got, tt.parent)
		}
	}
}

func TestGetDepth(t *testing.T) {
	tests := []struct {
		id    string
		depth int
	}{
		{"gur-a1b2c3d4", 0},
		{"gur-a1b2c3d4.1", 1},
		{"gur-a1b2c3d4.1.2", 2},
		{"gur-a1b2c3d4.1.2.3", 3},
	}

	for _, tt := range tests {
		got := GetDepth(tt.id)
		if got != tt.depth {
			t.Errorf("GetDepth(%q) = %d, want %d", tt.id, got, tt.depth)
		}
	}
}

func TestTaskClose(t *testing.T) {
	task := &Task{
		ID:     "gur-a1b2c3d4",
		Status: StatusOpen,
	}

	task.Close("completed")

	if task.Status != StatusClosed {
		t.Errorf("Close() status = %s, want %s", task.Status, StatusClosed)
	}
	if task.CloseReason != "completed" {
		t.Errorf("Close() reason = %s, want completed", task.CloseReason)
	}
	if task.ClosedAt == nil {
		t.Error("Close() did not set ClosedAt")
	}
}

func TestTaskReopen(t *testing.T) {
	now := time.Now()
	task := &Task{
		ID:          "gur-a1b2c3d4",
		Status:      StatusClosed,
		CloseReason: "done",
		ClosedAt:    &now,
	}

	task.Reopen()

	if task.Status != StatusOpen {
		t.Errorf("Reopen() status = %s, want %s", task.Status, StatusOpen)
	}
	if task.CloseReason != "" {
		t.Errorf("Reopen() reason = %s, want empty", task.CloseReason)
	}
	if task.ClosedAt != nil {
		t.Error("Reopen() did not clear ClosedAt")
	}
}

func TestTaskIsClosed(t *testing.T) {
	tests := []struct {
		status string
		closed bool
	}{
		{StatusOpen, false},
		{StatusInProgress, false},
		{StatusClosed, true},
		{StatusArchived, false},
	}

	for _, tt := range tests {
		task := &Task{Status: tt.status}
		if got := task.IsClosed(); got != tt.closed {
			t.Errorf("IsClosed() with status %s = %v, want %v", tt.status, got, tt.closed)
		}
	}
}

func TestTaskLabels(t *testing.T) {
	task := &Task{ID: "gur-a1b2c3d4"}

	// Add labels
	task.AddLabel("bug")
	task.AddLabel("urgent")

	if len(task.Labels) != 2 {
		t.Errorf("AddLabel() count = %d, want 2", len(task.Labels))
	}

	// Add duplicate - should be ignored
	task.AddLabel("bug")
	if len(task.Labels) != 2 {
		t.Errorf("AddLabel() duplicate not ignored, count = %d", len(task.Labels))
	}

	// Remove label
	task.RemoveLabel("bug")
	if len(task.Labels) != 1 {
		t.Errorf("RemoveLabel() count = %d, want 1", len(task.Labels))
	}

	// Remove non-existent - should be no-op
	task.RemoveLabel("nonexistent")
	if len(task.Labels) != 1 {
		t.Errorf("RemoveLabel() non-existent changed count to %d", len(task.Labels))
	}
}

func TestTaskPriorityString(t *testing.T) {
	tests := []struct {
		priority int
		expected string
	}{
		{PriorityCritical, "P0 (Critical)"},
		{PriorityHigh, "P1 (High)"},
		{PriorityMedium, "P2 (Medium)"},
		{PriorityLow, "P3 (Low)"},
		{PriorityLowest, "P4 (Lowest)"},
		{99, "Unknown"},
	}

	for _, tt := range tests {
		task := &Task{Priority: tt.priority}
		if got := task.PriorityString(); got != tt.expected {
			t.Errorf("PriorityString() with %d = %s, want %s", tt.priority, got, tt.expected)
		}
	}
}

func TestTaskCompact(t *testing.T) {
	task := &Task{
		ID:          "gur-a1b2c3d4",
		Title:       "Fix bug",
		Description: "Long description here",
		Notes:       "Some notes",
		Type:        TypeBug,
		CloseReason: "Fixed in commit abc",
	}

	task.Compact()

	if !task.Compacted {
		t.Error("Compact() did not set Compacted flag")
	}
	if task.Description != "" {
		t.Error("Compact() did not clear Description")
	}
	if task.Notes != "" {
		t.Error("Compact() did not clear Notes")
	}
	if task.Summary == "" {
		t.Error("Compact() did not generate Summary")
	}

	// Compact again should be no-op
	task.Summary = "modified"
	task.Compact()
	if task.Summary != "modified" {
		t.Error("Compact() modified already compacted task")
	}
}

func TestTaskArchive(t *testing.T) {
	task := &Task{Status: StatusClosed}

	task.Archive()
	if task.Status != StatusArchived {
		t.Errorf("Archive() status = %s, want %s", task.Status, StatusArchived)
	}

	if !task.IsArchived() {
		t.Error("IsArchived() = false after Archive()")
	}

	task.Unarchive()
	if task.Status != StatusClosed {
		t.Errorf("Unarchive() status = %s, want %s", task.Status, StatusClosed)
	}
}

func TestTaskAppendNotes(t *testing.T) {
	task := &Task{ID: "gur-a1b2c3d4"}

	task.AppendNotes("First note")
	if task.Notes == "" {
		t.Error("AppendNotes() did not add note")
	}

	task.AppendNotes("Second note")
	if len(task.Notes) <= len("First note") {
		t.Error("AppendNotes() did not append second note")
	}
}

func TestTokenUsageString(t *testing.T) {
	tests := []struct {
		name         string
		tokensUsed   int64
		tokensBudget int64
		expected     string
	}{
		{"no budget zero used", 0, 0, "0"},
		{"no budget some used", 1500, 0, "1500"},
		{"with budget zero used", 0, 5000, "0/5000 (0%)"},
		{"with budget 30 pct used", 1500, 5000, "1500/5000 (30%)"},
		{"with budget 100 pct used", 5000, 5000, "5000/5000 (100%)"},
		{"over budget", 7000, 5000, "7000/5000 (140%)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{TokensUsed: tt.tokensUsed, TokensBudget: tt.tokensBudget}
			if got := task.TokenUsageString(); got != tt.expected {
				t.Errorf("TokenUsageString() = %s, want %s", got, tt.expected)
			}
		})
	}
}
