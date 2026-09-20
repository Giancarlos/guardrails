package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// gurImportColumns and updateGurTask's switch list the same fields in two
// places; this fails if they ever drift apart, which would silently drop a
// field from an import.
func TestGurImportAppliesEveryMappedColumn(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	database.Create(&models.Task{ID: "gur-0cc00001", Title: "Parent", Status: models.StatusOpen, Type: models.TypeTask})
	database.Create(&models.Task{
		ID: "gur-0cc00002", Title: "Before", Description: "before", Status: models.StatusOpen,
		Priority: 4, Type: models.TypeTask, Assignee: "alice", Notes: "before",
		Labels: models.StringSlice{"old"}, Source: models.SourceLocal,
	})

	withGurImportFormat(t)
	record := `[{
		"id":"gur-0cc00002","parent_id":"gur-0cc00001","title":"After","description":"after",
		"status":"in_progress","priority":0,"type":"bug","labels":["new"],"assignee":"bob",
		"notes":"after","close_reason":"reason","summary":"summary","source":"beads",
		"source_id":"bd-xyz","tokens_used":42,"tokens_budget":100,"context_summary":"ctx",
		"closed_at":"2026-01-02T03:04:05Z"
	}]`
	if err := runImport(importCmd, []string{writeImportFile(t, record)}); err != nil {
		t.Fatalf("import: %v", err)
	}

	var got models.Task
	database.Where("id = ?", "gur-0cc00002").First(&got)
	checks := map[string]bool{
		"parent_id": got.ParentID == "gur-0cc00001", "title": got.Title == "After",
		"description": got.Description == "after", "status": got.Status == models.StatusInProgress,
		"priority": got.Priority == 0, "type": got.Type == models.TypeBug,
		"labels": len(got.Labels) == 1 && got.Labels[0] == "new", "assignee": got.Assignee == "bob",
		"notes": got.Notes == "after", "close_reason": got.CloseReason == "reason",
		"summary": got.Summary == "summary", "source": got.Source == models.SourceBeads,
		"source_id": derefString(got.SourceID) == "bd-xyz", "tokens_used": got.TokensUsed == 42,
		"tokens_budget": got.TokensBudget == 100, "context_summary": got.ContextSummary == "ctx",
		"closed_at": got.ClosedAt != nil && got.ClosedAt.UTC().Format(time.RFC3339) == "2026-01-02T03:04:05Z",
	}
	for field := range gurImportColumns {
		ok, covered := checks[field]
		if !covered {
			t.Errorf("field %q is in gurImportColumns but this test does not check it", field)
			continue
		}
		if !ok {
			t.Errorf("field %q was not applied by the import (task: %+v)", field, got)
		}
		var history int64
		database.Model(&models.TaskHistory{}).Where("task_id = ? AND field = ?", got.ID, field).Count(&history)
		if history != 1 {
			t.Errorf("field %q: %d history row(s), want 1", field, history)
		}
	}
}

func TestGurImportRefreshesUpdatedAt(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()
	withGurImportFormat(t)

	database.Create(&models.Task{ID: "gur-0dd00002", Title: "Stale", Status: models.StatusOpen, Type: models.TypeTask})
	var before models.Task
	database.Where("id = ?", "gur-0dd00002").First(&before)

	// No updated_at in the file: the row still changed, so it must not keep
	// the old timestamp.
	if err := runImport(importCmd, []string{writeImportFile(t, `[{"id":"gur-0dd00002","title":"Fresh"}]`)}); err != nil {
		t.Fatalf("import: %v", err)
	}
	var after models.Task
	database.Where("id = ?", "gur-0dd00002").First(&after)
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Errorf("updated_at = %v, want later than %v", after.UpdatedAt, before.UpdatedAt)
	}

	// An updated_at in the file wins, so a restore keeps the original.
	stamped := `[{"id":"gur-0dd00002","title":"Restored","updated_at":"2020-05-06T07:08:09Z"}]`
	if err := runImport(importCmd, []string{writeImportFile(t, stamped)}); err != nil {
		t.Fatalf("import with updated_at: %v", err)
	}
	database.Where("id = ?", "gur-0dd00002").First(&after)
	if after.UpdatedAt.UTC().Format(time.RFC3339) != "2020-05-06T07:08:09Z" {
		t.Errorf("updated_at = %v, want the file's value", after.UpdatedAt)
	}
}

func TestGurImportSkipsSelfAndCyclicDeps(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()
	withGurImportFormat(t)

	// a blocks b already; the file tries to add b blocks a (a cycle), a self
	// dep, and one pointing at a task that isn't there.
	database.Create(&models.Task{ID: "gur-0ee00001", Title: "A", Status: models.StatusOpen, Type: models.TypeTask})
	database.Create(&models.Task{ID: "gur-0ee00002", Title: "B", Status: models.StatusOpen, Type: models.TypeTask})
	database.Create(&models.Dependency{ParentID: "gur-0ee00001", ChildID: "gur-0ee00002", Type: "blocks"})

	file := `[{"id":"gur-0ee00002","title":"B","dependencies":[
		{"parent_id":"gur-0ee00002","child_id":"gur-0ee00001","type":"blocks"},
		{"parent_id":"gur-0ee00002","child_id":"gur-0ee00002","type":"blocks"},
		{"parent_id":"gur-0ee00002","child_id":"gur-0ee09999","type":"blocks"}]}]`
	if err := runImport(importCmd, []string{writeImportFile(t, file)}); err != nil {
		t.Fatalf("import: %v", err)
	}

	var deps []models.Dependency
	database.Find(&deps)
	if len(deps) != 1 || deps[0].ParentID != "gur-0ee00001" {
		t.Errorf("deps = %+v, want only the original a->b edge", deps)
	}
}

func TestGurImportDryRunWritesNothing(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()
	withGurImportFormat(t)

	prevDryRun := importDryRun
	importDryRun = true
	t.Cleanup(func() { importDryRun = prevDryRun })

	path := writeImportFile(t, `[{"id":"gur-0ff00001","title":"Not written"}]`)
	captureStdout(t, func() {
		if err := runImport(importCmd, []string{path}); err != nil {
			t.Fatalf("dry run: %v", err)
		}
	})

	var count int64
	database.Model(&models.Task{}).Count(&count)
	if count != 0 {
		t.Errorf("tasks = %d, want 0 after a dry run", count)
	}

	// A dry run still rejects a bad file, so it works as a check.
	bad := writeImportFile(t, `[{"id":"gur-0ff00002","title":"bad","status":"nope"}]`)
	if err := runImport(importCmd, []string{bad}); err == nil {
		t.Error("expected a dry run to report invalid records")
	}
}

// source_id carries a unique index; a collision must be reported as such
// rather than as a raw constraint error from the middle of the transaction.
func TestGurImportRejectsSourceIDCollisions(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()
	withGurImportFormat(t)

	existingSource := "bd-taken"
	database.Create(&models.Task{ID: "gur-0ba00001", Title: "Owner", Status: models.StatusOpen, Type: models.TypeTask, SourceID: &existingSource})

	stolen := `[{"id":"gur-0ba00002","title":"Thief","source_id":"bd-taken"}]`
	err := runImport(importCmd, []string{writeImportFile(t, stolen)})
	if err == nil {
		t.Fatal("expected an error for a source_id owned by another task")
	}
	if !strings.Contains(err.Error(), "already belongs to task gur-0ba00001") {
		t.Errorf("error = %v, want it to name the owning task", err)
	}

	duplicate := `[{"id":"gur-0ba00003","title":"One","source_id":"bd-new"},{"id":"gur-0ba00004","title":"Two","source_id":"bd-new"}]`
	err = runImport(importCmd, []string{writeImportFile(t, duplicate)})
	if err == nil {
		t.Fatal("expected an error for a source_id used twice in one file")
	}
	if !strings.Contains(err.Error(), "same file") {
		t.Errorf("error = %v, want it to flag the in-file duplicate", err)
	}

	var count int64
	database.Model(&models.Task{}).Count(&count)
	if count != 1 {
		t.Errorf("tasks = %d, want 1 (nothing written)", count)
	}

	// Re-importing the owner's own record is fine.
	same := `[{"id":"gur-0ba00001","title":"Owner renamed","source_id":"bd-taken"}]`
	if err := runImport(importCmd, []string{writeImportFile(t, same)}); err != nil {
		t.Errorf("re-import of the same task: %v", err)
	}
}
