package cmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

func createTestTasks(t *testing.T, count int) []models.Task {
	t.Helper()
	database := db.GetDB()

	var tasks []models.Task
	for i := 0; i < count; i++ {
		task := models.Task{
			ID:          models.GenerateID(),
			Title:       "Test Task " + string(rune('A'+i)),
			Status:      models.StatusOpen,
			Priority:    i,
			Type:        models.TypeFeature,
			Assignee:    "tester",
			Labels:      models.StringSlice{"bug", "urgent"},
			Description: "Description for task " + string(rune('A'+i)),
		}
		if err := database.Create(&task).Error; err != nil {
			t.Fatalf("Failed to create test task: %v", err)
		}
		// Re-read from DB to get timestamps
		var saved models.Task
		database.Where("id = ?", task.ID).First(&saved)
		tasks = append(tasks, saved)
	}
	return tasks
}

func TestExportJSON(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tasks := createTestTasks(t, 2)

	var buf bytes.Buffer
	if err := ExportJSON(&buf, tasks); err != nil {
		t.Fatalf("ExportJSON() error: %v", err)
	}

	output := buf.String()

	// Verify valid JSON
	var parsed []models.Task
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("ExportJSON() produced invalid JSON: %v", err)
	}

	// Verify correct count
	if len(parsed) != 2 {
		t.Errorf("ExportJSON() returned %d tasks, want 2", len(parsed))
	}

	// Verify task titles are present
	for _, task := range tasks {
		if !strings.Contains(output, task.Title) {
			t.Errorf("ExportJSON() output missing task title %q", task.Title)
		}
	}
}

func TestExportCSV(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tasks := createTestTasks(t, 2)

	var buf bytes.Buffer
	if err := ExportCSV(&buf, tasks); err != nil {
		t.Fatalf("ExportCSV() error: %v", err)
	}

	// Parse the CSV output
	reader := csv.NewReader(strings.NewReader(buf.String()))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ExportCSV() produced invalid CSV: %v", err)
	}

	// Verify header row + 2 data rows = 3 total
	if len(records) != 3 {
		t.Errorf("ExportCSV() returned %d rows, want 3 (1 header + 2 data)", len(records))
	}

	// Verify header columns
	expectedHeader := []string{"ID", "Title", "Status", "Priority", "Type", "Assignee", "Labels", "CreatedAt"}
	if len(records) > 0 {
		for i, col := range expectedHeader {
			if records[0][i] != col {
				t.Errorf("ExportCSV() header[%d] = %q, want %q", i, records[0][i], col)
			}
		}
	}

	// Verify data rows contain task titles
	for i, task := range tasks {
		if records[i+1][1] != task.Title {
			t.Errorf("ExportCSV() row %d title = %q, want %q", i+1, records[i+1][1], task.Title)
		}
	}
}

func TestExportMarkdown(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tasks := createTestTasks(t, 1)

	var buf bytes.Buffer
	if err := ExportMarkdown(&buf, tasks); err != nil {
		t.Fatalf("ExportMarkdown() error: %v", err)
	}

	output := buf.String()

	// Verify markdown heading
	if !strings.Contains(output, "## [") {
		t.Error("ExportMarkdown() output missing '## [' heading")
	}

	// Verify field labels
	expectedLabels := []string{"**Status:**", "**Priority:**", "**Type:**", "**Assignee:**", "**Labels:**", "**Created:**"}
	for _, label := range expectedLabels {
		if !strings.Contains(output, label) {
			t.Errorf("ExportMarkdown() output missing field label %q", label)
		}
	}

	// Verify task title is present
	if !strings.Contains(output, tasks[0].Title) {
		t.Errorf("ExportMarkdown() output missing task title %q", tasks[0].Title)
	}

	// Verify description is present
	if !strings.Contains(output, tasks[0].Description) {
		t.Errorf("ExportMarkdown() output missing description %q", tasks[0].Description)
	}
}

func TestImportJSON(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create two tasks to import
	importTasks := []models.Task{
		{
			ID:       "gur-import01",
			Title:    "Imported Task 1",
			Status:   models.StatusOpen,
			Priority: models.PriorityHigh,
			Type:     models.TypeBug,
		},
		{
			ID:       "gur-import02",
			Title:    "Imported Task 2",
			Status:   models.StatusInProgress,
			Priority: models.PriorityMedium,
			Type:     models.TypeFeature,
		},
	}

	created, updated, skipped, err := ImportTasks(importTasks, false)
	if err != nil {
		t.Fatalf("ImportTasks() error: %v", err)
	}

	if created != 2 {
		t.Errorf("ImportTasks() created = %d, want 2", created)
	}
	if updated != 0 {
		t.Errorf("ImportTasks() updated = %d, want 0", updated)
	}
	if skipped != 0 {
		t.Errorf("ImportTasks() skipped = %d, want 0", skipped)
	}

	// Verify tasks exist in DB
	for _, it := range importTasks {
		var task models.Task
		if err := database.Where("id = ?", it.ID).First(&task).Error; err != nil {
			t.Errorf("Imported task %s not found in DB: %v", it.ID, err)
			continue
		}
		if task.Title != it.Title {
			t.Errorf("Imported task %s title = %q, want %q", it.ID, task.Title, it.Title)
		}
	}

	// Test skip behavior: import same tasks again
	created2, _, skipped2, err := ImportTasks(importTasks, false)
	if err != nil {
		t.Fatalf("ImportTasks() second call error: %v", err)
	}
	if created2 != 0 {
		t.Errorf("ImportTasks() second call created = %d, want 0", created2)
	}
	if skipped2 != 2 {
		t.Errorf("ImportTasks() second call skipped = %d, want 2", skipped2)
	}

	// Test merge behavior: update existing tasks
	importTasks[0].Title = "Updated Task 1"
	_, updated3, _, err := ImportTasks(importTasks, true)
	if err != nil {
		t.Fatalf("ImportTasks() merge call error: %v", err)
	}
	// Only the first task changed; unchanged records count as skipped
	if updated3 != 1 {
		t.Errorf("ImportTasks() merge call updated = %d, want 1", updated3)
	}

	// Verify the update took effect
	var updatedTask models.Task
	database.Where("id = ?", importTasks[0].ID).First(&updatedTask)
	if updatedTask.Title != "Updated Task 1" {
		t.Errorf("Merged task title = %q, want %q", updatedTask.Title, "Updated Task 1")
	}
}

func TestRoundTrip(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create tasks
	originalTasks := createTestTasks(t, 3)

	// Export to JSON
	var buf bytes.Buffer
	if err := ExportJSON(&buf, originalTasks); err != nil {
		t.Fatalf("ExportJSON() error: %v", err)
	}
	exportedJSON := buf.Bytes()

	// Clear DB (delete all tasks)
	database.Exec("DELETE FROM tasks")

	// Verify DB is empty
	var count int64
	database.Model(&models.Task{}).Count(&count)
	if count != 0 {
		t.Fatalf("DB not empty after clear: %d tasks remain", count)
	}

	// Parse exported JSON
	var tasksToImport []models.Task
	if err := json.Unmarshal(exportedJSON, &tasksToImport); err != nil {
		t.Fatalf("Failed to parse exported JSON: %v", err)
	}

	// Import
	created, _, _, err := ImportTasks(tasksToImport, false)
	if err != nil {
		t.Fatalf("ImportTasks() error: %v", err)
	}
	if created != len(originalTasks) {
		t.Errorf("ImportTasks() created = %d, want %d", created, len(originalTasks))
	}

	// Verify same data
	for _, orig := range originalTasks {
		var task models.Task
		if err := database.Where("id = ?", orig.ID).First(&task).Error; err != nil {
			t.Errorf("Round-trip task %s not found: %v", orig.ID, err)
			continue
		}
		if task.Title != orig.Title {
			t.Errorf("Round-trip task %s title = %q, want %q", orig.ID, task.Title, orig.Title)
		}
		if task.Status != orig.Status {
			t.Errorf("Round-trip task %s status = %q, want %q", orig.ID, task.Status, orig.Status)
		}
		if task.Priority != orig.Priority {
			t.Errorf("Round-trip task %s priority = %d, want %d", orig.ID, task.Priority, orig.Priority)
		}
		if task.Type != orig.Type {
			t.Errorf("Round-trip task %s type = %q, want %q", orig.ID, task.Type, orig.Type)
		}
		if task.Assignee != orig.Assignee {
			t.Errorf("Round-trip task %s assignee = %q, want %q", orig.ID, task.Assignee, orig.Assignee)
		}
	}
}
