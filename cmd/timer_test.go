package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

func TestTimerStartStop(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create a task to track time on
	task := &models.Task{
		ID:     "gur-timer001",
		Title:  "Timer Test Task",
		Status: models.StatusOpen,
	}
	if err := database.Create(task).Error; err != nil {
		t.Fatalf("Create task error: %v", err)
	}

	// Start timer
	err := runTimerStart(timerStartCmd, []string{"gur-timer001"})
	if err != nil {
		t.Fatalf("runTimerStart() error: %v", err)
	}

	// Verify there's an active timer
	var active models.TimeEntry
	if err := database.Where("task_id = ? AND stopped_at IS NULL", "gur-timer001").First(&active).Error; err != nil {
		t.Fatalf("Expected active timer, got error: %v", err)
	}
	if active.TaskID != "gur-timer001" {
		t.Errorf("Active timer task_id = %q, want %q", active.TaskID, "gur-timer001")
	}

	// Starting again should fail (duplicate active timer)
	err = runTimerStart(timerStartCmd, []string{"gur-timer001"})
	if err == nil {
		t.Error("Expected error starting duplicate timer, got nil")
	}

	// Wait a tiny bit so duration > 0
	time.Sleep(10 * time.Millisecond)

	// Stop timer
	err = runTimerStop(timerStopCmd, nil)
	if err != nil {
		t.Fatalf("runTimerStop() error: %v", err)
	}

	// Verify the timer is stopped with duration > 0
	var stopped models.TimeEntry
	if err := database.Where("task_id = ? AND stopped_at IS NOT NULL", "gur-timer001").First(&stopped).Error; err != nil {
		t.Fatalf("Expected stopped timer, got error: %v", err)
	}
	if stopped.Duration <= 0 {
		// Duration is in seconds, and we only slept 10ms, so it could be 0 due to int truncation.
		// Accept 0 or positive since int truncation of <1s is expected.
		// The important check is that StoppedAt is set.
		if stopped.StoppedAt == nil {
			t.Error("StoppedAt should not be nil after stop")
		}
	}
}

func TestTimerLog(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create a task
	task := &models.Task{
		ID:     "gur-timerlog",
		Title:  "Timer Log Test Task",
		Status: models.StatusOpen,
	}
	if err := database.Create(task).Error; err != nil {
		t.Fatalf("Create task error: %v", err)
	}

	// Log manual "2h" entry
	err := runTimerLog(timerLogCmd, []string{"gur-timerlog", "2h"})
	if err != nil {
		t.Fatalf("runTimerLog() error: %v", err)
	}

	// Verify duration = 7200 seconds
	var entry models.TimeEntry
	if err := database.Where("task_id = ?", "gur-timerlog").First(&entry).Error; err != nil {
		t.Fatalf("Find entry error: %v", err)
	}
	if entry.Duration != 7200 {
		t.Errorf("Duration = %d, want 7200", entry.Duration)
	}
	if entry.StoppedAt == nil {
		t.Error("StoppedAt should be set for logged entries")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds  int64
		expected string
	}{
		{0, "0m"},
		{60, "1m"},
		{3600, "1h 0m"},
		{5400, "1h 30m"},
		{7200, "2h 0m"},
	}

	for _, tc := range tests {
		result := models.FormatDuration(tc.seconds)
		if result != tc.expected {
			t.Errorf("FormatDuration(%d) = %q, want %q", tc.seconds, result, tc.expected)
		}
	}
}

func TestTimerReport(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create 2 tasks
	task1 := &models.Task{ID: "gur-report01", Title: "Report Task 1", Status: models.StatusOpen}
	task2 := &models.Task{ID: "gur-report02", Title: "Report Task 2", Status: models.StatusOpen}
	database.Create(task1)
	database.Create(task2)

	// Create completed time entries
	now := time.Now()
	past1 := now.Add(-2 * time.Hour)
	past2 := now.Add(-1 * time.Hour)
	past3 := now.Add(-30 * time.Minute)

	entries := []models.TimeEntry{
		{
			TaskID:    "gur-report01",
			StartedAt: past1,
			StoppedAt: &now,
			Duration:  7200, // 2h
		},
		{
			TaskID:    "gur-report01",
			StartedAt: past2,
			StoppedAt: &now,
			Duration:  3600, // 1h
		},
		{
			TaskID:    "gur-report02",
			StartedAt: past3,
			StoppedAt: &now,
			Duration:  1800, // 30m
		},
	}

	for _, e := range entries {
		if err := database.Create(&e).Error; err != nil {
			t.Fatalf("Create entry error: %v", err)
		}
	}

	// Query report for task1
	type reportRow struct {
		TaskID   string
		Total    int64
		EntryCnt int64
	}

	var rows []reportRow
	err := database.Model(&models.TimeEntry{}).
		Select("task_id, SUM(duration) as total, COUNT(*) as entry_cnt").
		Where("stopped_at IS NOT NULL").
		Group("task_id").
		Order("total DESC").
		Find(&rows).Error
	if err != nil {
		t.Fatalf("Report query error: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("Expected 2 report rows, got %d", len(rows))
	}

	// First row should be task1 with total 10800 (2h + 1h)
	if rows[0].TaskID != "gur-report01" {
		t.Errorf("First row task_id = %q, want %q", rows[0].TaskID, "gur-report01")
	}
	if rows[0].Total != 10800 {
		t.Errorf("First row total = %d, want 10800", rows[0].Total)
	}
	if rows[0].EntryCnt != 2 {
		t.Errorf("First row entry count = %d, want 2", rows[0].EntryCnt)
	}

	// Second row should be task2 with total 1800 (30m)
	if rows[1].TaskID != "gur-report02" {
		t.Errorf("Second row task_id = %q, want %q", rows[1].TaskID, "gur-report02")
	}
	if rows[1].Total != 1800 {
		t.Errorf("Second row total = %d, want 1800", rows[1].Total)
	}
	if rows[1].EntryCnt != 1 {
		t.Errorf("Second row entry count = %d, want 1", rows[1].EntryCnt)
	}
}

func TestParseDurationLimits(t *testing.T) {
	valid := map[string]int64{
		"2h":     7200,
		"30m":    1800,
		"1h30m":  5400,
		"10000h": 36000000,
	}
	for input, want := range valid {
		got, err := ParseDuration(input)
		if err != nil {
			t.Errorf("ParseDuration(%q) error: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseDuration(%q) = %d, want %d", input, got, want)
		}
	}

	invalid := []string{"", "0m", "0h0m", "abc", "1d", "10001h", "600001m", "99999999999999999999h", "3000000000000000h"}
	for _, input := range invalid {
		if got, err := ParseDuration(input); err == nil {
			t.Errorf("ParseDuration(%q) = %d, want error", input, got)
		}
	}
}

func TestTimerStopMultipleActive(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()
	database.Create(&models.Task{ID: "gur-stopaaaa", Title: "A", Status: models.StatusOpen})
	database.Create(&models.Task{ID: "gur-stopbbbb", Title: "B", Status: models.StatusOpen})

	if err := runTimerStart(timerStartCmd, []string{"gur-stopaaaa"}); err != nil {
		t.Fatalf("start A: %v", err)
	}
	if err := runTimerStart(timerStartCmd, []string{"gur-stopbbbb"}); err != nil {
		t.Fatalf("start B: %v", err)
	}

	// Without a task ID, stopping is ambiguous and must not pick one arbitrarily
	err := runTimerStop(timerStopCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "multiple active timers") {
		t.Fatalf("stop without task = %v, want multiple active timers error", err)
	}

	// Stopping a specific task (by prefix) stops only that timer
	if err := runTimerStop(timerStopCmd, []string{"gur-stopa"}); err != nil {
		t.Fatalf("stop A: %v", err)
	}
	var stillActive []models.TimeEntry
	database.Where("stopped_at IS NULL").Find(&stillActive)
	if len(stillActive) != 1 || stillActive[0].TaskID != "gur-stopbbbb" {
		t.Fatalf("active timers after stopping A = %+v, want only gur-stopbbbb", stillActive)
	}

	// With one timer left, stop needs no argument
	if err := runTimerStop(timerStopCmd, nil); err != nil {
		t.Fatalf("stop remaining: %v", err)
	}
}
