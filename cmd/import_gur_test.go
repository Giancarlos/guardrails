package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

// Upstream's gur-jsonl export had no importer, so a native dump could not be
// restored. This walks the full loop: export, wipe, import.
func TestGurJSONLRoundTrip(t *testing.T) {
	cleanup := setupTestDB(t)
	database := db.GetDB()

	parent := &models.Task{ID: "gur-0ab00001", Title: "Blocker", Status: models.StatusOpen, Type: models.TypeTask, Priority: 1, TokensUsed: 1500, TokensBudget: 5000}
	child := &models.Task{ID: "gur-0ab00002", Title: "Blocked", Status: models.StatusOpen, Type: models.TypeBug, Priority: 2, Labels: models.StringSlice{"api"}, ContextSummary: "needs the blocker first"}
	database.Create(parent)
	database.Create(child)
	database.Create(&models.Dependency{ParentID: parent.ID, ChildID: child.ID, Type: "blocks"})

	path := filepath.Join(t.TempDir(), "dump.jsonl")
	prevFormat, prevOutput := exportFormat, exportOutput
	exportFormat, exportOutput = "gur-jsonl", path
	if err := runExport(exportCmd, nil); err != nil {
		t.Fatalf("export: %v", err)
	}
	exportFormat, exportOutput = prevFormat, prevOutput
	cleanup()

	// Fresh database: nothing to conflict with.
	cleanup2 := setupTestDB(t)
	defer cleanup2()
	database = db.GetDB()

	prevImportFormat, prevConflict := importFormat, importOnConflict
	importFormat, importOnConflict = "gur-jsonl", "update"
	t.Cleanup(func() { importFormat, importOnConflict = prevImportFormat, prevConflict })

	if err := runImport(importCmd, []string{path}); err != nil {
		t.Fatalf("import: %v", err)
	}

	var restored models.Task
	if err := database.Where("id = ?", child.ID).First(&restored).Error; err != nil {
		t.Fatalf("child not restored: %v", err)
	}
	if restored.Title != "Blocked" || restored.Type != models.TypeBug || restored.Priority != 2 {
		t.Errorf("child = %+v, want title/type/priority preserved", restored)
	}
	if restored.ContextSummary != "needs the blocker first" || len(restored.Labels) != 1 || restored.Labels[0] != "api" {
		t.Errorf("child labels/context not preserved: %+v", restored)
	}

	var restoredParent models.Task
	database.Where("id = ?", parent.ID).First(&restoredParent)
	if restoredParent.TokensUsed != 1500 || restoredParent.TokensBudget != 5000 {
		t.Errorf("tokens = %d/%d, want 1500/5000", restoredParent.TokensUsed, restoredParent.TokensBudget)
	}

	var deps []models.Dependency
	database.Find(&deps)
	if len(deps) != 1 {
		t.Fatalf("deps = %d, want 1", len(deps))
	}
	if deps[0].ParentID != parent.ID || deps[0].ChildID != child.ID || deps[0].Type != "blocks" {
		t.Errorf("dep = %+v, want %s blocks %s", deps[0], parent.ID, child.ID)
	}

	// Re-importing the same file is a no-op, not a duplicate.
	if err := runImport(importCmd, []string{path}); err != nil {
		t.Fatalf("second import: %v", err)
	}
	var taskCount, depCount int64
	database.Model(&models.Task{}).Count(&taskCount)
	database.Model(&models.Dependency{}).Count(&depCount)
	if taskCount != 2 || depCount != 1 {
		t.Errorf("after reimport: %d tasks, %d deps; want 2 and 1", taskCount, depCount)
	}
}

func TestGurImportRejectsBeadsOnlyFlags(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	prevFormat := importFormat
	importFormat = "gur-jsonl"
	t.Cleanup(func() {
		importFormat = prevFormat
		importCmd.Flags().Set("label-prefix", "bd:")
		importCmd.Flags().Lookup("label-prefix").Changed = false
	})

	importCmd.Flags().Set("label-prefix", "x:")
	if err := runImport(importCmd, []string{path}); err == nil {
		t.Error("expected --label-prefix to be rejected for a native import")
	}
}
