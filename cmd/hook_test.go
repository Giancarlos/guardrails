package cmd

import (
	"testing"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

func TestHookRegister(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	hook := &models.Hook{
		Event:   "on-create",
		Command: "echo test",
		Enabled: true,
	}

	if err := database.Create(hook).Error; err != nil {
		t.Fatalf("failed to create hook: %v", err)
	}

	if hook.ID == "" {
		t.Error("hook ID should be set after creation")
	}

	// Verify it persists
	var retrieved models.Hook
	if err := database.Where("id = ?", hook.ID).First(&retrieved).Error; err != nil {
		t.Fatalf("failed to retrieve hook: %v", err)
	}

	if retrieved.Event != "on-create" {
		t.Errorf("event = %q, want %q", retrieved.Event, "on-create")
	}
	if retrieved.Command != "echo test" {
		t.Errorf("command = %q, want %q", retrieved.Command, "echo test")
	}
	if !retrieved.Enabled {
		t.Error("hook should be enabled by default")
	}
}

func TestHookList(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Add 2 hooks
	hook1 := &models.Hook{Event: "on-create", Command: "echo created", Enabled: true}
	hook2 := &models.Hook{Event: "on-close", Command: "echo closed", Enabled: true}

	if err := database.Create(hook1).Error; err != nil {
		t.Fatalf("failed to create hook1: %v", err)
	}
	if err := database.Create(hook2).Error; err != nil {
		t.Fatalf("failed to create hook2: %v", err)
	}

	// List them
	var hooks []models.Hook
	if err := database.Find(&hooks).Error; err != nil {
		t.Fatalf("failed to list hooks: %v", err)
	}

	if len(hooks) != 2 {
		t.Errorf("hook count = %d, want 2", len(hooks))
	}
}

func TestHookRemove(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	hook := &models.Hook{Event: "on-update", Command: "echo updated", Enabled: true}
	if err := database.Create(hook).Error; err != nil {
		t.Fatalf("failed to create hook: %v", err)
	}

	hookID := hook.ID

	// Remove it
	result := database.Where("id = ?", hookID).Delete(&models.Hook{})
	if result.Error != nil {
		t.Fatalf("failed to delete hook: %v", result.Error)
	}
	if result.RowsAffected != 1 {
		t.Errorf("rows affected = %d, want 1", result.RowsAffected)
	}

	// Verify deleted
	var count int64
	database.Model(&models.Hook{}).Where("id = ?", hookID).Count(&count)
	if count != 0 {
		t.Error("hook still exists after deletion")
	}
}

func TestHookEventValidation(t *testing.T) {
	// Test valid events
	validEvents := []string{"on-create", "on-update", "on-close", "on-reopen"}
	for _, event := range validEvents {
		if !models.IsValidHookEvent(event) {
			t.Errorf("IsValidHookEvent(%q) = false, want true", event)
		}
	}

	// Test invalid events
	invalidEvents := []string{"on-delete", "create", "invalid", "", "on_create"}
	for _, event := range invalidEvents {
		if models.IsValidHookEvent(event) {
			t.Errorf("IsValidHookEvent(%q) = true, want false", event)
		}
	}
}
