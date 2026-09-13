package cmd

import (
	"testing"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

func TestBulkUpdateStatus(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create 3 open tasks
	tasks := []*models.Task{
		{ID: "gur-bulk0001", Title: "Bulk Task 1", Status: models.StatusOpen, Type: models.TypeTask, Priority: 2},
		{ID: "gur-bulk0002", Title: "Bulk Task 2", Status: models.StatusOpen, Type: models.TypeTask, Priority: 2},
		{ID: "gur-bulk0003", Title: "Bulk Task 3", Status: models.StatusOpen, Type: models.TypeTask, Priority: 2},
	}
	for _, task := range tasks {
		if err := database.Create(task).Error; err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
	}

	// Set bulk flags
	bulkStatus = "open"
	bulkPriority = -1
	bulkType = ""
	bulkAssignee = ""
	bulkDryRun = false
	bulkSetStatus = "in_progress"
	bulkSetPriority = -1
	bulkSetAssignee = ""

	// Execute bulk update using the command's RunE directly
	bulkUpdateCmd.Flags().Set("set-status", "in_progress")
	err := runBulkUpdate(bulkUpdateCmd, []string{})
	if err != nil {
		t.Fatalf("runBulkUpdate() error: %v", err)
	}

	// Verify all 3 tasks changed to in_progress
	for _, task := range tasks {
		var updated models.Task
		if err := database.Where("id = ?", task.ID).First(&updated).Error; err != nil {
			t.Fatalf("Failed to find task %s: %v", task.ID, err)
		}
		if updated.Status != models.StatusInProgress {
			t.Errorf("Task %s status = %s, want %s", task.ID, updated.Status, models.StatusInProgress)
		}
	}
}

func TestBulkClose(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create 2 open tasks
	tasks := []*models.Task{
		{ID: "gur-bulkc001", Title: "Close Task 1", Status: models.StatusOpen, Type: models.TypeTask, Priority: 2},
		{ID: "gur-bulkc002", Title: "Close Task 2", Status: models.StatusOpen, Type: models.TypeTask, Priority: 2},
	}
	for _, task := range tasks {
		if err := database.Create(task).Error; err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
	}

	// Create a gate and link+pass it for both tasks (gates are required to close)
	gate := &models.Gate{
		ID:    "gate-bulkc001",
		Title: "Bulk Close Gate",
		Type:  "test",
	}
	if err := database.Create(gate).Error; err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	for _, task := range tasks {
		link := &models.GateTaskLink{
			GateID: gate.ID,
			TaskID: task.ID,
			Status: models.GateLinkPassed,
		}
		if err := database.Create(link).Error; err != nil {
			t.Fatalf("Failed to create gate link: %v", err)
		}
	}

	// Set bulk flags
	bulkStatus = "open"
	bulkPriority = -1
	bulkType = ""
	bulkAssignee = ""
	bulkDryRun = false
	bulkCloseReason = "Sprint ended"

	err := runBulkClose(bulkCloseCmd, []string{})
	if err != nil {
		t.Fatalf("runBulkClose() error: %v", err)
	}

	// Verify both tasks closed with reason
	for _, task := range tasks {
		var updated models.Task
		if err := database.Where("id = ?", task.ID).First(&updated).Error; err != nil {
			t.Fatalf("Failed to find task %s: %v", task.ID, err)
		}
		if updated.Status != models.StatusClosed {
			t.Errorf("Task %s status = %s, want %s", task.ID, updated.Status, models.StatusClosed)
		}
		if updated.CloseReason != "Sprint ended" {
			t.Errorf("Task %s close_reason = %s, want 'Sprint ended'", task.ID, updated.CloseReason)
		}
		if updated.ClosedAt == nil {
			t.Errorf("Task %s ClosedAt should not be nil", task.ID)
		}
	}
}

func TestBulkLabels(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create 2 tasks
	tasks := []*models.Task{
		{ID: "gur-bulkl001", Title: "Label Task 1", Status: models.StatusOpen, Type: models.TypeTask, Priority: 2},
		{ID: "gur-bulkl002", Title: "Label Task 2", Status: models.StatusOpen, Type: models.TypeTask, Priority: 2},
	}
	for _, task := range tasks {
		if err := database.Create(task).Error; err != nil {
			t.Fatalf("Failed to create task: %v", err)
		}
	}

	// Bulk add label "urgent"
	bulkStatus = "open"
	bulkPriority = -1
	bulkType = ""
	bulkAssignee = ""
	bulkDryRun = false
	bulkLabelAdd = "urgent"
	bulkLabelRemove = ""

	err := runBulkLabel(bulkLabelCmd, []string{})
	if err != nil {
		t.Fatalf("runBulkLabel() add error: %v", err)
	}

	// Verify both have label "urgent"
	for _, task := range tasks {
		var updated models.Task
		if err := database.Where("id = ?", task.ID).First(&updated).Error; err != nil {
			t.Fatalf("Failed to find task %s: %v", task.ID, err)
		}
		found := false
		for _, l := range updated.Labels {
			if l == "urgent" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Task %s should have label 'urgent', got %v", task.ID, updated.Labels)
		}
	}

	// Bulk remove label "urgent"
	bulkLabelAdd = ""
	bulkLabelRemove = "urgent"

	err = runBulkLabel(bulkLabelCmd, []string{})
	if err != nil {
		t.Fatalf("runBulkLabel() remove error: %v", err)
	}

	// Verify both no longer have label "urgent"
	for _, task := range tasks {
		var updated models.Task
		if err := database.Where("id = ?", task.ID).First(&updated).Error; err != nil {
			t.Fatalf("Failed to find task %s: %v", task.ID, err)
		}
		for _, l := range updated.Labels {
			if l == "urgent" {
				t.Errorf("Task %s should NOT have label 'urgent', got %v", task.ID, updated.Labels)
			}
		}
	}
}

func TestBulkPartialFailure(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create 2 open tasks
	taskWithGate := &models.Task{
		ID:     "gur-bulkp001",
		Title:  "Task With Passed Gate",
		Status: models.StatusOpen,
		Type:   models.TypeTask,
	}
	taskWithoutGate := &models.Task{
		ID:     "gur-bulkp002",
		Title:  "Task Without Passed Gate",
		Status: models.StatusOpen,
		Type:   models.TypeTask,
	}
	if err := database.Create(taskWithGate).Error; err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}
	if err := database.Create(taskWithoutGate).Error; err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Create a gate
	gate := &models.Gate{
		ID:    "gate-bulkp001",
		Title: "Partial Gate",
		Type:  "test",
	}
	if err := database.Create(gate).Error; err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	// Link and pass gate for taskWithGate
	link1 := &models.GateTaskLink{
		GateID: gate.ID,
		TaskID: taskWithGate.ID,
		Status: models.GateLinkPassed,
	}
	if err := database.Create(link1).Error; err != nil {
		t.Fatalf("Failed to create gate link: %v", err)
	}

	// Link gate to taskWithoutGate but leave as pending (not passed)
	link2 := &models.GateTaskLink{
		GateID: gate.ID,
		TaskID: taskWithoutGate.ID,
		Status: models.GateLinkPending,
	}
	if err := database.Create(link2).Error; err != nil {
		t.Fatalf("Failed to create gate link: %v", err)
	}

	// Set bulk flags
	bulkStatus = "open"
	bulkPriority = -1
	bulkType = ""
	bulkAssignee = ""
	bulkDryRun = false
	bulkCloseReason = "Bulk partial close"

	err := runBulkClose(bulkCloseCmd, []string{})
	if err != nil {
		t.Fatalf("runBulkClose() error: %v", err)
	}

	// Verify taskWithGate is closed
	var closedTask models.Task
	if err := database.Where("id = ?", taskWithGate.ID).First(&closedTask).Error; err != nil {
		t.Fatalf("Failed to find task %s: %v", taskWithGate.ID, err)
	}
	if closedTask.Status != models.StatusClosed {
		t.Errorf("Task %s status = %s, want %s", taskWithGate.ID, closedTask.Status, models.StatusClosed)
	}

	// Verify taskWithoutGate is NOT closed (skipped)
	var skippedTask models.Task
	if err := database.Where("id = ?", taskWithoutGate.ID).First(&skippedTask).Error; err != nil {
		t.Fatalf("Failed to find task %s: %v", taskWithoutGate.ID, err)
	}
	if skippedTask.Status != models.StatusOpen {
		t.Errorf("Task %s status = %s, want %s (should have been skipped)", taskWithoutGate.ID, skippedTask.Status, models.StatusOpen)
	}
}
