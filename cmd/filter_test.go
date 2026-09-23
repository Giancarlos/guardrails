package cmd

import (
	"encoding/json"
	"testing"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

func TestFilterSave(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Save a filter
	filterSaveStatus = "open"
	filterSavePriority = 1
	filterSaveType = "bug"
	filterSaveAssignee = "alice"
	filterSaveLabel = "critical"
	defer func() {
		filterSaveStatus = ""
		filterSavePriority = -1
		filterSaveType = ""
		filterSaveAssignee = ""
		filterSaveLabel = ""
	}()

	err := runFilterSave(filterSaveCmd, []string{"my-filter"})
	if err != nil {
		t.Fatalf("runFilterSave() error: %v", err)
	}

	// Verify it persists in config
	val, err := db.GetConfig("filter:my-filter")
	if err != nil {
		t.Fatalf("GetConfig() error: %v", err)
	}

	var sf SavedFilter
	if err := json.Unmarshal([]byte(val), &sf); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if sf.Status != "open" {
		t.Errorf("Status = %q, want %q", sf.Status, "open")
	}
	if sf.Priority != 1 {
		t.Errorf("Priority = %d, want 1", sf.Priority)
	}
	if sf.Type != "bug" {
		t.Errorf("Type = %q, want %q", sf.Type, "bug")
	}
	if sf.Assignee != "alice" {
		t.Errorf("Assignee = %q, want %q", sf.Assignee, "alice")
	}
	if sf.Label != "critical" {
		t.Errorf("Label = %q, want %q", sf.Label, "critical")
	}
}

func TestFilterList(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Save 2 filters
	sf1 := SavedFilter{Status: "open", Priority: -1}
	sf2 := SavedFilter{Type: "bug", Priority: -1}

	data1, _ := json.Marshal(sf1)
	data2, _ := json.Marshal(sf2)

	if err := db.SetConfig("filter:view-open", string(data1)); err != nil {
		t.Fatalf("SetConfig() error: %v", err)
	}
	if err := db.SetConfig("filter:bugs-only", string(data2)); err != nil {
		t.Fatalf("SetConfig() error: %v", err)
	}

	// List and verify count
	var configs []models.Config
	if err := db.GetDB().Where("key LIKE ?", "filter:%").Find(&configs).Error; err != nil {
		t.Fatalf("Find() error: %v", err)
	}

	if len(configs) != 2 {
		t.Errorf("Expected 2 filters, got %d", len(configs))
	}
}

func TestFilterDelete(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Save a filter
	sf := SavedFilter{Status: "closed", Priority: -1}
	data, _ := json.Marshal(sf)
	if err := db.SetConfig("filter:to-delete", string(data)); err != nil {
		t.Fatalf("SetConfig() error: %v", err)
	}

	// Verify it exists
	_, err := db.GetConfig("filter:to-delete")
	if err != nil {
		t.Fatalf("Filter should exist before deletion: %v", err)
	}

	// Delete it
	err = runFilterDelete(filterDeleteCmd, []string{"to-delete"})
	if err != nil {
		t.Fatalf("runFilterDelete() error: %v", err)
	}

	// Verify it's gone
	_, err = db.GetConfig("filter:to-delete")
	if err == nil {
		t.Error("Filter should not exist after deletion")
	}
}

func TestFilterApply(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Save a filter with status=open
	sf := SavedFilter{Status: "open", Priority: -1}
	data, _ := json.Marshal(sf)
	if err := db.SetConfig("filter:open-only", string(data)); err != nil {
		t.Fatalf("SetConfig() error: %v", err)
	}

	// Create open and closed tasks
	openTask := &models.Task{
		ID:     "gur-open0001",
		Title:  "Open Task",
		Status: models.StatusOpen,
	}
	closedTask := &models.Task{
		ID:     "gur-close001",
		Title:  "Closed Task",
		Status: models.StatusClosed,
	}
	if err := database.Create(openTask).Error; err != nil {
		t.Fatalf("Create open task error: %v", err)
	}
	if err := database.Create(closedTask).Error; err != nil {
		t.Fatalf("Create closed task error: %v", err)
	}

	// Load the saved filter and apply it
	loaded, err := LoadSavedFilter("open-only")
	if err != nil {
		t.Fatalf("LoadSavedFilter() error: %v", err)
	}

	// Build query with filter applied
	query := database.Model(&models.Task{})
	if loaded.Status != "" {
		query = query.Where("status = ?", loaded.Status)
	}
	if loaded.Priority >= 0 {
		query = query.Where("priority = ?", loaded.Priority)
	}
	if loaded.Type != "" {
		query = query.Where("type = ?", loaded.Type)
	}
	if loaded.Assignee != "" {
		query = query.Where("assignee = ?", loaded.Assignee)
	}

	var tasks []models.Task
	if err := query.Find(&tasks).Error; err != nil {
		t.Fatalf("Find() error: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task (open only), got %d", len(tasks))
	}
	if tasks[0].ID != "gur-open0001" {
		t.Errorf("Expected task ID gur-open0001, got %s", tasks[0].ID)
	}
}
