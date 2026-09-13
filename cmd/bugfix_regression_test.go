package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

// withJSONOutput forces JSON output for the duration of the test.
func withJSONOutput(t *testing.T) {
	t.Helper()
	prev := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = prev })
}

func resetBulkFilters() {
	bulkStatus = ""
	bulkPriority = -1
	bulkType = ""
	bulkAssignee = ""
	bulkDryRun = false
}

func TestBulkUpdateRejectsInvalidValuesAndClosedTasks(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	open := &models.Task{ID: "gur-bulkv001", Title: "Open", Status: models.StatusOpen, Type: models.TypeTask, Priority: 2, Assignee: "bulk-validate"}
	closed := &models.Task{ID: "gur-bulkv002", Title: "Closed", Status: models.StatusClosed, Type: models.TypeTask, Priority: 2, Assignee: "bulk-validate"}
	database.Create(open)
	database.Create(closed)

	resetBulkFilters()
	bulkAssignee = "bulk-validate"
	flags := bulkUpdateCmd.Flags()
	t.Cleanup(func() {
		resetBulkFilters()
		flags.Set("set-priority", "-1")
		flags.Lookup("set-priority").Changed = false
		flags.Lookup("set-status").Changed = false
	})

	flags.Set("set-status", "bogus")
	if err := runBulkUpdate(bulkUpdateCmd, nil); err == nil {
		t.Error("expected error for invalid --set-status")
	}

	flags.Set("set-status", "closed")
	if err := runBulkUpdate(bulkUpdateCmd, nil); err == nil || !strings.Contains(err.Error(), "bulk close") {
		t.Errorf("--set-status closed error = %v, want pointer to 'gur bulk close'", err)
	}

	flags.Set("set-status", "open")
	flags.Set("set-priority", "99")
	if err := runBulkUpdate(bulkUpdateCmd, nil); err == nil {
		t.Error("expected error for out-of-range --set-priority")
	}

	// Matching set includes a closed task: status change must be refused entirely
	flags.Set("set-priority", "1")
	if err := runBulkUpdate(bulkUpdateCmd, nil); err == nil || !strings.Contains(err.Error(), "closed/archived") {
		t.Errorf("status change on closed task error = %v, want closed/archived refusal", err)
	}
	var stillClosed models.Task
	database.Where("id = ?", closed.ID).First(&stillClosed)
	if stillClosed.Status != models.StatusClosed || stillClosed.Priority != 2 {
		t.Errorf("closed task modified: status=%s priority=%d", stillClosed.Status, stillClosed.Priority)
	}
}

func TestBulkCloseRespectsSubtasksAndArchived(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	parent := &models.Task{ID: "gur-bulks001", Title: "Parent", Status: models.StatusOpen, Type: models.TypeTask, Assignee: "bulk-sub"}
	child := &models.Task{ID: "gur-bulks001.1", ParentID: "gur-bulks001", Title: "Child", Status: models.StatusOpen, Type: models.TypeTask}
	archived := &models.Task{ID: "gur-bulks002", Title: "Archived", Status: models.StatusArchived, Type: models.TypeTask, Assignee: "bulk-sub"}
	for _, task := range []*models.Task{parent, child, archived} {
		if err := database.Create(task).Error; err != nil {
			t.Fatalf("create task: %v", err)
		}
	}
	gate := &models.Gate{ID: "gate-bulks001", Title: "Gate", Type: "test"}
	database.Create(gate)
	for _, id := range []string{parent.ID, archived.ID} {
		database.Create(&models.GateTaskLink{GateID: gate.ID, TaskID: id, Status: models.GateLinkPassed})
	}

	resetBulkFilters()
	t.Cleanup(resetBulkFilters)
	bulkAssignee = "bulk-sub"
	bulkCloseReason = "cleanup"

	if err := runBulkClose(bulkCloseCmd, nil); err != nil {
		t.Fatalf("runBulkClose: %v", err)
	}
	var p models.Task
	database.Where("id = ?", parent.ID).First(&p)
	if p.Status != models.StatusOpen {
		t.Errorf("parent with open subtask status = %s, want open (like 'gur close')", p.Status)
	}

	// Explicitly targeting archived tasks must not turn them back into closed
	bulkStatus = models.StatusArchived
	if err := runBulkClose(bulkCloseCmd, nil); err != nil {
		t.Fatalf("runBulkClose archived: %v", err)
	}
	var a models.Task
	database.Where("id = ?", archived.ID).First(&a)
	if a.Status != models.StatusArchived {
		t.Errorf("archived task status = %s, want archived", a.Status)
	}
}

func TestBulkLabelRecordsHistory(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	database.Create(&models.Task{ID: "gur-bulkh001", Title: "Label", Status: models.StatusOpen, Type: models.TypeTask, Assignee: "bulk-hist"})

	resetBulkFilters()
	t.Cleanup(func() { resetBulkFilters(); bulkLabelAdd = ""; bulkLabelRemove = "" })
	bulkAssignee = "bulk-hist"
	bulkLabelAdd = "urgent"
	bulkLabelRemove = ""

	if err := runBulkLabel(bulkLabelCmd, nil); err != nil {
		t.Fatalf("runBulkLabel: %v", err)
	}
	var count int64
	database.Model(&models.TaskHistory{}).Where("task_id = ? AND field = ?", "gur-bulkh001", "label_added").Count(&count)
	if count != 1 {
		t.Errorf("label_added history rows = %d, want 1", count)
	}
}

func writeImportFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "import.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write import file: %v", err)
	}
	return path
}

func TestImportValidatesAndIsAtomic(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	invalid := []string{
		`[{"id":"gur-impv0001","title":"ok"},{"id":"gur-impv0002","title":"bad","status":"done"}]`,
		`[{"id":"not-an-id","title":"bad id"}]`,
		`[{"id":"gur-impv0003","title":""}]`,
		`[{"id":"gur-impv0004","title":"bad","priority":42}]`,
		`[{"id":"gur-impv0005","title":"bad","type":"whatever"}]`,
	}
	for _, content := range invalid {
		if err := runImport(importCmd, []string{writeImportFile(t, content)}); err == nil {
			t.Errorf("import %s: expected error", content)
		}
	}

	var count int64
	database.Model(&models.Task{}).Count(&count)
	if count != 0 {
		t.Errorf("tasks after failed imports = %d, want 0 (imports must be atomic)", count)
	}

	// Missing fields get the same defaults as 'gur create'
	if err := runImport(importCmd, []string{writeImportFile(t, `[{"id":"gur-impd0001","title":"defaults"}]`)}); err != nil {
		t.Fatalf("import defaults: %v", err)
	}
	var task models.Task
	database.Where("id = ?", "gur-impd0001").First(&task)
	if task.Priority != models.PriorityMedium || task.Status != models.StatusOpen || task.Type != models.TypeTask {
		t.Errorf("defaults: priority=%d status=%s type=%s, want 2/open/task", task.Priority, task.Status, task.Type)
	}
}

func TestImportMergeOnlyChangesPresentFields(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	original := &models.Task{ID: "gur-impm0001", Title: "Keep me", Description: "desc", Assignee: "alice", Status: models.StatusOpen, Type: models.TypeBug, Priority: 3}
	database.Create(original)
	var before models.Task
	database.Where("id = ?", original.ID).First(&before)

	prevMerge := importMerge
	importMerge = true
	t.Cleanup(func() { importMerge = prevMerge })

	if err := runImport(importCmd, []string{writeImportFile(t, `[{"id":"gur-impm0001","priority":1}]`)}); err != nil {
		t.Fatalf("merge import: %v", err)
	}
	var after models.Task
	database.Where("id = ?", original.ID).First(&after)
	if after.Priority != 1 {
		t.Errorf("priority = %d, want 1", after.Priority)
	}
	if after.Title != "Keep me" || after.Description != "desc" || after.Assignee != "alice" || after.Type != models.TypeBug {
		t.Errorf("unspecified fields changed: %+v", after)
	}
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("created_at changed from %v to %v", before.CreatedAt, after.CreatedAt)
	}
	var history int64
	database.Model(&models.TaskHistory{}).Where("task_id = ? AND field = ?", original.ID, "priority").Count(&history)
	if history != 1 {
		t.Errorf("priority history rows = %d, want 1", history)
	}

	// Merge cannot close a task (would bypass gates)
	if err := runImport(importCmd, []string{writeImportFile(t, `[{"id":"gur-impm0001","status":"closed"}]`)}); err == nil {
		t.Error("expected error closing a task via import --merge")
	}
	database.Where("id = ?", original.ID).First(&after)
	if after.Status != models.StatusOpen {
		t.Errorf("status = %s, want open", after.Status)
	}
}

func TestListLabelFilterAndSortValidation(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()
	withJSONOutput(t)

	database.Create(&models.Task{ID: "gur-listl001", Title: "Match", Status: models.StatusOpen, Type: models.TypeTask, Labels: models.StringSlice{"a_b"}})
	database.Create(&models.Task{ID: "gur-listl002", Title: "Wildcard lookalike", Status: models.StatusOpen, Type: models.TypeTask, Labels: models.StringSlice{"axb"}})
	database.Create(&models.Task{ID: "gur-listl003", Title: "Other", Status: models.StatusOpen, Type: models.TypeTask, Labels: models.StringSlice{"a_b_c"}})

	prevStatus, prevPriority, prevType, prevAssignee := listStatus, listPriority, listType, listAssignee
	listStatus, listPriority, listType, listAssignee = "", -1, "", ""
	t.Cleanup(func() {
		listStatus, listPriority, listType, listAssignee = prevStatus, prevPriority, prevType, prevAssignee
		listLabel, listFilter, listSort = "", "", ""
	})

	countTasks := func() int {
		out := captureStdout(t, func() {
			if err := runList(listCmd, nil); err != nil {
				t.Fatalf("runList: %v", err)
			}
		})
		var parsed struct {
			Count int `json:"count"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out)
		}
		return parsed.Count
	}

	listLabel = "a_b"
	if got := countTasks(); got != 1 {
		t.Errorf("list --label a_b count = %d, want 1 (exact label, no LIKE wildcards)", got)
	}

	// A saved filter's label must be applied
	listLabel = ""
	if err := db.SetConfig(filterKeyPrefix+"labeled", `{"priority":-1,"label":"a_b"}`); err != nil {
		t.Fatalf("save filter: %v", err)
	}
	listFilter = "labeled"
	if got := countTasks(); got != 1 {
		t.Errorf("list --filter with label count = %d, want 1", got)
	}

	listFilter = ""
	listSort = "bogus"
	if err := runList(listCmd, nil); err == nil {
		t.Error("expected error for invalid --sort")
	}
}

func TestCreateTemplateVarValidation(t *testing.T) {
	tmpl := &models.Template{Name: "bugtpl", Title: "Fix {{area}}", Variables: models.StringSlice{"area"}}

	if err := validateTemplateVars(tmpl, map[string]string{}); err == nil {
		t.Error("expected error for missing declared variable")
	}
	if err := validateTemplateVars(tmpl, map[string]string{"area": "x", "typo": "y"}); err == nil {
		t.Error("expected error for undeclared variable")
	}
	if err := validateTemplateVars(tmpl, map[string]string{"area": "login"}); err != nil {
		t.Errorf("valid vars: %v", err)
	}
	legacy := &models.Template{Name: "legacy", Title: "Fix {{area}}"}
	if err := validateTemplateVars(legacy, map[string]string{"anything": "ok"}); err != nil {
		t.Errorf("template without declared variables should accept any vars: %v", err)
	}
}

func TestUpdateRejectsNegativeTokens(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()
	database.Create(&models.Task{ID: "gur-tokneg01", Title: "Tokens", Status: models.StatusOpen, Type: models.TypeTask, TokensUsed: 500})

	flags := updateCmd.Flags()
	t.Cleanup(func() {
		for _, name := range []string{"tokens-used", "tokens-add", "tokens-budget"} {
			flags.Lookup(name).Changed = false
		}
		updateTokensUsed, updateTokensAdd, updateTokensBudget = -1, 0, -1
	})

	cases := []struct{ flag, value string }{
		{"tokens-used", "-5"},
		{"tokens-budget", "-7"},
		{"tokens-add", "-1000"},
	}
	for _, c := range cases {
		for _, name := range []string{"tokens-used", "tokens-add", "tokens-budget"} {
			flags.Lookup(name).Changed = false
		}
		flags.Set(c.flag, c.value)
		if err := runUpdate(updateCmd, []string{"gur-tokneg01"}); err == nil {
			t.Errorf("--%s %s: expected error", c.flag, c.value)
		}
	}

	var task models.Task
	database.Where("id = ?", "gur-tokneg01").First(&task)
	if task.TokensUsed != 500 || task.TokensBudget != 0 {
		t.Errorf("tokens changed despite errors: used=%d budget=%d", task.TokensUsed, task.TokensBudget)
	}
}

func TestHandoffRules(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	database.Create(&models.Task{ID: "gur-hoffc001", Title: "Closed", Status: models.StatusClosed, Type: models.TypeTask})
	database.Create(&models.Task{ID: "gur-hoffo001", Title: "Open", Status: models.StatusOpen, Type: models.TypeTask})

	prevFrom, prevTo, prevSummary, prevContext := handoffFrom, handoffTo, handoffSummary, handoffContext
	t.Cleanup(func() {
		handoffFrom, handoffTo, handoffSummary, handoffContext = prevFrom, prevTo, prevSummary, prevContext
		receiveAgent, receiveReject, handoffHistoryLimit = "", false, 50
	})
	handoffFrom, handoffTo, handoffSummary, handoffContext = "claude", "copilot", "work", ""

	if err := runHandoff(handoffCmd, []string{"gur-hoffc001"}); err == nil {
		t.Error("expected error handing off a closed task")
	}

	// Rejecting a handoff must not stamp accepted_at
	if err := runHandoff(handoffCmd, []string{"gur-hoffo001"}); err != nil {
		t.Fatalf("handoff: %v", err)
	}
	receiveAgent, receiveReject = "copilot", true
	if err := runReceive(receiveCmd, []string{"gur-hoffo001"}); err != nil {
		t.Fatalf("receive --reject: %v", err)
	}
	var rejected models.Handoff
	database.Where("task_id = ?", "gur-hoffo001").First(&rejected)
	if rejected.Status != models.HandoffRejected || rejected.AcceptedAt != nil {
		t.Errorf("rejected handoff status=%s accepted_at=%v, want rejected/nil", rejected.Status, rejected.AcceptedAt)
	}

	// --limit keeps the newest handoffs, shown oldest-first
	base := time.Now().Add(-time.Hour)
	for i, id := range []string{"hoff-hist0001", "hoff-hist0002", "hoff-hist0003"} {
		database.Create(&models.Handoff{ID: id, TaskID: "gur-hoffo001", FromAgent: "a", ToAgent: "b", Summary: "s", Status: models.HandoffPending, CreatedAt: base.Add(time.Duration(i) * time.Minute)})
	}
	withJSONOutput(t)
	handoffHistoryLimit = 2
	out := captureStdout(t, func() {
		if err := runHandoffHistory(handoffHistoryCmd, []string{"gur-hoffo001"}); err != nil {
			t.Fatalf("handoff-history: %v", err)
		}
	})
	var parsed struct {
		Handoffs []models.Handoff `json:"handoffs"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(parsed.Handoffs) != 2 {
		t.Fatalf("handoffs = %d, want 2", len(parsed.Handoffs))
	}
	for _, h := range parsed.Handoffs {
		if h.ID == "hoff-hist0001" || h.ID == "hoff-hist0002" {
			t.Errorf("limited history includes older handoff %s instead of the newest: %+v", h.ID, parsed.Handoffs)
		}
	}
	if parsed.Handoffs[0].CreatedAt.After(parsed.Handoffs[1].CreatedAt) {
		t.Errorf("handoffs not in chronological order: %+v", parsed.Handoffs)
	}
}
