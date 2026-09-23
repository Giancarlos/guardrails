package cmd

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

func TestBulkCloseClosesDependentsInSameBatch(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	parent := &models.Task{ID: "gur-batch001", Title: "Parent", Status: models.StatusOpen, Type: models.TypeTask, Assignee: "batch"}
	child := &models.Task{ID: "gur-batch001.1", ParentID: "gur-batch001", Title: "Child", Status: models.StatusOpen, Type: models.TypeTask, Assignee: "batch"}
	blocker := &models.Task{ID: "gur-batch002", Title: "Blocker", Status: models.StatusOpen, Type: models.TypeTask, Assignee: "batch"}
	blocked := &models.Task{ID: "gur-batch003", Title: "Blocked", Status: models.StatusOpen, Type: models.TypeTask, Assignee: "batch"}
	outside := &models.Task{ID: "gur-batch004", Title: "Outside blocker", Status: models.StatusOpen, Type: models.TypeTask}
	stuck := &models.Task{ID: "gur-batch005", Title: "Blocked from outside", Status: models.StatusOpen, Type: models.TypeTask, Assignee: "batch"}
	gate := &models.Gate{ID: "gate-batch001", Title: "Gate", Type: "test"}
	database.Create(gate)
	for _, task := range []*models.Task{parent, child, blocker, blocked, outside, stuck} {
		if err := database.Create(task).Error; err != nil {
			t.Fatalf("create task: %v", err)
		}
		database.Create(&models.GateTaskLink{GateID: gate.ID, TaskID: task.ID, Status: models.GateLinkPassed})
	}
	database.Create(&models.Dependency{ParentID: blocker.ID, ChildID: blocked.ID, Type: models.DepTypeBlocks})
	database.Create(&models.Dependency{ParentID: outside.ID, ChildID: stuck.ID, Type: models.DepTypeBlocks})

	resetBulkFilters()
	t.Cleanup(resetBulkFilters)
	bulkAssignee = "batch"
	bulkCloseReason = "done"

	if err := runBulkClose(bulkCloseCmd, nil); err != nil {
		t.Fatalf("runBulkClose: %v", err)
	}
	want := map[string]string{
		parent.ID:  models.StatusClosed,
		child.ID:   models.StatusClosed,
		blocker.ID: models.StatusClosed,
		blocked.ID: models.StatusClosed,
		stuck.ID:   models.StatusOpen,
	}
	for id, status := range want {
		var got models.Task
		database.Where("id = ?", id).First(&got)
		if got.Status != status {
			t.Errorf("%s status = %s, want %s", id, got.Status, status)
		}
	}

	bulkCloseReason = "  "
	if err := runBulkClose(bulkCloseCmd, nil); err == nil {
		t.Error("expected error for empty --reason")
	}
}

func TestHandoffAndReceiveStatusRules(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	database.Create(&models.Task{ID: "gur-hoffa001", Title: "Archived", Status: models.StatusArchived, Type: models.TypeTask})
	database.Create(&models.Task{ID: "gur-hoffo002", Title: "Open", Status: models.StatusOpen, Type: models.TypeTask})
	database.Create(&models.Task{ID: "gur-hoffc002", Title: "Closed", Status: models.StatusClosed, Type: models.TypeTask})
	database.Create(&models.Handoff{ID: "hoff-closed01", TaskID: "gur-hoffc002", FromAgent: "a", ToAgent: "b", Summary: "s", Status: models.HandoffPending})

	prevFrom, prevTo, prevSummary, prevContext := handoffFrom, handoffTo, handoffSummary, handoffContext
	t.Cleanup(func() {
		handoffFrom, handoffTo, handoffSummary, handoffContext = prevFrom, prevTo, prevSummary, prevContext
		receiveAgent, receiveReject = "", false
	})
	handoffFrom, handoffTo, handoffSummary, handoffContext = "a", "b", "work", ""

	if err := runHandoff(handoffCmd, []string{"gur-hoffa001"}); err == nil {
		t.Error("expected error handing off an archived task")
	}
	handoffFrom = " "
	if err := runHandoff(handoffCmd, []string{"gur-hoffo002"}); err == nil {
		t.Error("expected error for empty --from")
	}

	receiveAgent, receiveReject = "", false
	if err := runReceive(receiveCmd, []string{"gur-hoffc002"}); err == nil {
		t.Error("expected error for empty --agent")
	}
	receiveAgent = "b"
	if err := runReceive(receiveCmd, []string{"gur-hoffc002"}); err == nil {
		t.Error("expected error accepting a handoff on a closed task")
	}
	receiveReject = true
	if err := runReceive(receiveCmd, []string{"gur-hoffc002"}); err != nil {
		t.Errorf("rejecting a stale handoff on a closed task should work: %v", err)
	}
}

func TestImportRejectsDuplicateIDsAndOrphanParents(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()
	withGurImportFormat(t)

	invalid := []string{
		`[{"id":"gur-0dd00001","title":"one"},{"id":"gur-0dd00001","title":"two"}]`,
		`[{"id":"gur-00ab0001.1","parent_id":"gur-00ab0001","title":"orphan"}]`,
	}
	for _, content := range invalid {
		if err := runImport(importCmd, []string{writeImportFile(t, content)}); err == nil {
			t.Errorf("import %s: expected error", content)
		}
	}
	var count int64
	database.Model(&models.Task{}).Count(&count)
	if count != 0 {
		t.Fatalf("tasks after failed imports = %d, want 0", count)
	}

	// A parent later in the same file is fine
	valid := `[{"id":"gur-0fa00001.1","parent_id":"gur-0fa00001","title":"child"},{"id":"gur-0fa00001","title":"parent"}]`
	if err := runImport(importCmd, []string{writeImportFile(t, valid)}); err != nil {
		t.Errorf("import with parent in same file: %v", err)
	}
}

func resetUpdateFlags(t *testing.T) {
	t.Helper()
	updateCmd.LocalFlags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
}

func TestUpdateNoChangesSkipsHooksAndTokenOverflow(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	outFile := filepath.Join(t.TempDir(), "hook.out")
	database.Create(&models.Hook{Event: models.HookEventOnUpdate, Command: `echo fired >> "` + outFile + `"`, Enabled: true})
	database.Create(&models.Task{ID: "gur-updnoop1", Title: "Noop", Status: models.StatusOpen, Type: models.TypeTask, TokensUsed: math.MaxInt64 - 10})

	resetUpdateFlags(t)
	t.Cleanup(func() {
		resetUpdateFlags(t)
		updateAssignee, updateTokensAdd = "", 0
	})

	captureStdout(t, func() {
		if err := runUpdate(updateCmd, []string{"gur-updnoop1"}); err != nil {
			t.Fatalf("update with no flags: %v", err)
		}
	})
	if _, err := os.Stat(outFile); err == nil {
		t.Error("on-update hook fired for an update with no changes")
	}

	updateCmd.Flags().Set("assignee", "bob")
	captureStdout(t, func() {
		if err := runUpdate(updateCmd, []string{"gur-updnoop1"}); err != nil {
			t.Fatalf("update --assignee: %v", err)
		}
	})
	if _, err := os.Stat(outFile); err != nil {
		t.Error("on-update hook did not fire for a real change")
	}

	resetUpdateFlags(t)
	updateCmd.Flags().Set("tokens-add", "100")
	if err := runUpdate(updateCmd, []string{"gur-updnoop1"}); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Errorf("--tokens-add overflow error = %v, want overflow error", err)
	}
}

func TestMCPTaskUpdateHooksOnlyOnChange(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	outFile := filepath.Join(t.TempDir(), "hook.out")
	database.Create(&models.Hook{Event: models.HookEventOnUpdate, Command: `echo fired >> "` + outFile + `"`, Enabled: true})
	database.Create(&models.Task{ID: "gur-mcpnoop1", Title: "Noop", Status: models.StatusOpen, Type: models.TypeTask})

	if _, err := toolTaskUpdate(map[string]interface{}{"id": "gur-mcpnoop1"}); err != nil {
		t.Fatalf("task_update: %v", err)
	}
	mcpHooks.Wait()
	if _, err := os.Stat(outFile); err == nil {
		t.Error("on-update hook fired for an MCP update with no changes")
	}

	if _, err := toolTaskUpdate(map[string]interface{}{"id": "gur-mcpnoop1", "assignee": "bob"}); err != nil {
		t.Fatalf("task_update: %v", err)
	}
	mcpHooks.Wait()
	if _, err := os.Stat(outFile); err != nil {
		t.Error("on-update hook did not fire for an MCP change")
	}
}

func TestTemplateVarAcceptsCommaSeparatedNames(t *testing.T) {
	flag := templateCreateCmd.Flags().Lookup("var")
	t.Cleanup(func() {
		flag.Value.(pflag.SliceValue).Replace([]string{})
		flag.Changed = false
		tmplVars = nil
	})
	if err := templateCreateCmd.Flags().Set("var", "a,b"); err != nil {
		t.Fatal(err)
	}
	if len(tmplVars) != 2 || tmplVars[0] != "a" || tmplVars[1] != "b" {
		t.Errorf("tmplVars = %q, want [a b]", tmplVars)
	}
}

func TestHookEnableDisable(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	database := db.GetDB()

	hook := &models.Hook{Event: models.HookEventOnCreate, Command: "true", Enabled: true}
	database.Create(hook)

	captureStdout(t, func() {
		if err := setHookEnabled(hook.ID, false); err != nil {
			t.Fatalf("disable: %v", err)
		}
	})
	var got models.Hook
	database.Where("id = ?", hook.ID).First(&got)
	if got.Enabled {
		t.Error("hook still enabled after disable")
	}
	if err := setHookEnabled("hook-missing", true); err == nil {
		t.Error("expected error for unknown hook")
	}
}

func TestBudgetValidatesArgsAndWarn(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if err := budgetCmd.Args(budgetCmd, []string{"extra"}); err == nil {
		t.Error("expected error for extra budget argument")
	}
	prev := budgetWarn
	t.Cleanup(func() { budgetWarn = prev })
	budgetWarn = -1
	if err := runBudget(budgetCmd, nil); err == nil {
		t.Error("expected error for negative --warn")
	}
}

func TestEmptyListsEncodeAsJSONArrays(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	withJSONOutput(t)

	out := captureStdout(t, func() {
		if err := runFilterList(filterListCmd, nil); err != nil {
			t.Fatalf("filter list: %v", err)
		}
	})
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if string(parsed["filters"]) != "[]" {
		t.Errorf("filters = %s, want []", parsed["filters"])
	}
}
