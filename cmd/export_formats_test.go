package cmd

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/ioformat"
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
			t.Fatalf("create test task: %v", err)
		}
		var saved models.Task
		database.Where("id = ?", task.ID).First(&saved)
		tasks = append(tasks, saved)
	}
	return tasks
}

func TestEncodeJSONArray(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tasks := createTestTasks(t, 2)
	var buf bytes.Buffer
	if err := ioformat.EncodeJSON(&buf, tasks); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded []models.Task
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("tasks = %d, want 2", len(decoded))
	}
	if decoded[0].ID != tasks[0].ID || decoded[0].Title != tasks[0].Title {
		t.Errorf("first task = %+v, want id %s", decoded[0], tasks[0].ID)
	}
}

func TestEncodeCSV(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tasks := createTestTasks(t, 2)
	var buf bytes.Buffer
	if err := ioformat.EncodeCSV(&buf, tasks); err != nil {
		t.Fatalf("encode: %v", err)
	}

	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header + 2)", len(rows))
	}
	if rows[0][0] != "ID" || rows[0][1] != "Title" {
		t.Errorf("header = %v", rows[0])
	}
	if rows[1][0] != tasks[0].ID {
		t.Errorf("first row id = %q, want %q", rows[1][0], tasks[0].ID)
	}
	if rows[1][6] != "bug, urgent" {
		t.Errorf("labels = %q, want %q", rows[1][6], "bug, urgent")
	}
}

func TestEncodeMarkdown(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tasks := createTestTasks(t, 2)
	var buf bytes.Buffer
	if err := ioformat.EncodeMarkdown(&buf, tasks); err != nil {
		t.Fatalf("encode: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"## [" + tasks[0].ID + "]", "**Status:** open", "**Labels:** bug, urgent", "\n---\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// Ported from the branch's own export tests: an unsupported format must be
// rejected before the output file is created, or a typo destroys the target.
func TestExportInvalidFormatKeepsExistingFile(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "out.json")
	if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	prevFormat, prevOutput := exportFormat, exportOutput
	t.Cleanup(func() { exportFormat, exportOutput = prevFormat, prevOutput })
	exportFormat, exportOutput = "xml", path

	if err := runExport(exportCmd, nil); err == nil {
		t.Error("expected error for unsupported format")
	}
	if data, _ := os.ReadFile(path); string(data) != "keep" {
		t.Errorf("existing file = %q, want it untouched", data)
	}
}

func TestExportStatusFilter(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	database.Create(&models.Task{ID: "gur-0e500001", Title: "open one", Status: models.StatusOpen, Type: models.TypeTask})
	database.Create(&models.Task{ID: "gur-0e500002", Title: "closed one", Status: models.StatusClosed, Type: models.TypeTask})

	path := filepath.Join(t.TempDir(), "out.json")
	prevFormat, prevOutput, prevStatus := exportFormat, exportOutput, exportStatus
	t.Cleanup(func() { exportFormat, exportOutput, exportStatus = prevFormat, prevOutput, prevStatus })
	exportFormat, exportOutput, exportStatus = "json", path, models.StatusOpen

	if err := runExport(exportCmd, nil); err != nil {
		t.Fatalf("export: %v", err)
	}
	data, _ := os.ReadFile(path)
	var tasks []models.Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "gur-0e500001" {
		t.Errorf("exported %d task(s) %v, want only gur-0e500001", len(tasks), tasks)
	}

	exportStatus = "bogus"
	if err := runExport(exportCmd, nil); err == nil {
		t.Error("expected error for invalid --status")
	}
}
