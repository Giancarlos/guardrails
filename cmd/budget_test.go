package cmd

import (
	"testing"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

func TestUpdateTokensUsed(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create a task with TokensUsed = 0
	task := &models.Task{
		ID:         "gur-budget01",
		Title:      "Budget Test Task",
		Status:     models.StatusOpen,
		TokensUsed: 0,
	}
	if err := database.Create(task).Error; err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Simulate --tokens-used 3000: set TokensUsed to 3000 and save
	task.TokensUsed = 3000
	if err := database.Save(task).Error; err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Reload from DB and verify
	var reloaded models.Task
	if err := database.Where("id = ?", "gur-budget01").First(&reloaded).Error; err != nil {
		t.Fatalf("Failed to reload task: %v", err)
	}

	if reloaded.TokensUsed != 3000 {
		t.Errorf("TokensUsed = %d, want 3000", reloaded.TokensUsed)
	}
}

func TestUpdateTokensAdd(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create a task with TokensUsed = 3000
	task := &models.Task{
		ID:         "gur-budget02",
		Title:      "Budget Add Test",
		Status:     models.StatusOpen,
		TokensUsed: 3000,
	}
	if err := database.Create(task).Error; err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Simulate --tokens-add 2000: increment TokensUsed by 2000 and save
	task.TokensUsed += 2000
	if err := database.Save(task).Error; err != nil {
		t.Fatalf("Failed to save task: %v", err)
	}

	// Reload from DB and verify
	var reloaded models.Task
	if err := database.Where("id = ?", "gur-budget02").First(&reloaded).Error; err != nil {
		t.Fatalf("Failed to reload task: %v", err)
	}

	if reloaded.TokensUsed != 5000 {
		t.Errorf("TokensUsed = %d, want 5000", reloaded.TokensUsed)
	}
}

func TestUpdateTokensBudget(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create a task and set TokensBudget = 10000
	task := &models.Task{
		ID:           "gur-budget03",
		Title:        "Budget Limit Test",
		Status:       models.StatusOpen,
		TokensBudget: 10000,
	}
	if err := database.Create(task).Error; err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Reload from DB and verify TokensBudget persisted
	var reloaded models.Task
	if err := database.Where("id = ?", "gur-budget03").First(&reloaded).Error; err != nil {
		t.Fatalf("Failed to reload task: %v", err)
	}

	if reloaded.TokensBudget != 10000 {
		t.Errorf("TokensBudget = %d, want 10000", reloaded.TokensBudget)
	}
}

func TestBudgetQuery(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create 3 tasks with different statuses and token values
	tasks := []*models.Task{
		{
			ID:           "gur-budgetqa",
			Title:        "Task A",
			Status:       models.StatusOpen,
			TokensUsed:   5000,
			TokensBudget: 10000,
		},
		{
			ID:           "gur-budgetqb",
			Title:        "Task B",
			Status:       models.StatusInProgress,
			TokensUsed:   3000,
			TokensBudget: 0,
		},
		{
			ID:           "gur-budgetqc",
			Title:        "Task C",
			Status:       models.StatusClosed,
			TokensUsed:   8000,
			TokensBudget: 10000,
		},
	}
	for _, task := range tasks {
		if err := database.Create(task).Error; err != nil {
			t.Fatalf("Failed to create task %s: %v", task.ID, err)
		}
	}

	// Query: active tasks with token usage or budget set
	var results []models.Task
	err := database.Where(
		"status IN (?,?) AND (tokens_used > 0 OR tokens_budget > 0)",
		models.StatusOpen, models.StatusInProgress,
	).Find(&results).Error
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Verify only tasks A and B returned (not C, which is closed)
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	foundIDs := map[string]bool{}
	var totalUsed, totalBudget int64
	for _, r := range results {
		foundIDs[r.ID] = true
		totalUsed += r.TokensUsed
		totalBudget += r.TokensBudget
	}

	if !foundIDs["gur-budgetqa"] {
		t.Error("Expected Task A (gur-budgetqa) in results")
	}
	if !foundIDs["gur-budgetqb"] {
		t.Error("Expected Task B (gur-budgetqb) in results")
	}
	if foundIDs["gur-budgetqc"] {
		t.Error("Task C (gur-budgetqc) should not be in results (closed)")
	}

	// Verify totals
	if totalUsed != 8000 {
		t.Errorf("Total tokens used = %d, want 8000", totalUsed)
	}
	if totalBudget != 10000 {
		t.Errorf("Total tokens budget = %d, want 10000", totalBudget)
	}
}

func TestListSortTokens(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create 3 tasks with different token counts and priorities
	tasks := []*models.Task{
		{
			ID:         "gur-sorttkna",
			Title:      "Task A",
			Status:     models.StatusOpen,
			TokensUsed: 100,
			Priority:   2,
		},
		{
			ID:         "gur-sorttknb",
			Title:      "Task B",
			Status:     models.StatusOpen,
			TokensUsed: 5000,
			Priority:   1,
		},
		{
			ID:         "gur-sorttknc",
			Title:      "Task C",
			Status:     models.StatusOpen,
			TokensUsed: 3000,
			Priority:   0,
		},
	}
	for _, task := range tasks {
		if err := database.Create(task).Error; err != nil {
			t.Fatalf("Failed to create task %s: %v", task.ID, err)
		}
	}

	// Query with order: tokens_used DESC, priority ASC
	var results []models.Task
	err := database.Where("status = ?", models.StatusOpen).
		Order("tokens_used DESC, priority ASC").
		Find(&results).Error
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// Verify order: B (5000), C (3000), A (100)
	expectedOrder := []struct {
		id         string
		tokensUsed int64
	}{
		{"gur-sorttknb", 5000},
		{"gur-sorttknc", 3000},
		{"gur-sorttkna", 100},
	}

	for i, expected := range expectedOrder {
		if results[i].ID != expected.id {
			t.Errorf("Result[%d].ID = %s, want %s", i, results[i].ID, expected.id)
		}
		if results[i].TokensUsed != expected.tokensUsed {
			t.Errorf("Result[%d].TokensUsed = %d, want %d", i, results[i].TokensUsed, expected.tokensUsed)
		}
	}
}

func TestShowTokenDisplay(t *testing.T) {
	// Test task with budget set
	task1 := &models.Task{
		TokensUsed:   5000,
		TokensBudget: 10000,
	}
	got := task1.TokenUsageString()
	expected := "5000/10000 (50%)"
	if got != expected {
		t.Errorf("TokenUsageString() = %q, want %q", got, expected)
	}

	// Test task with no budget
	task2 := &models.Task{
		TokensUsed:   1500,
		TokensBudget: 0,
	}
	got = task2.TokenUsageString()
	expected = "1500"
	if got != expected {
		t.Errorf("TokenUsageString() = %q, want %q", got, expected)
	}
}
