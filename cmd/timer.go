package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var timerCmd = &cobra.Command{
	Use:   "timer",
	Short: "Track time spent on tasks",
}

var timerStartCmd = &cobra.Command{
	Use:   "start <task-id>",
	Short: "Start a timer for a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTimerStart,
}

var timerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the active timer",
	RunE:  runTimerStop,
}

var timerLogCmd = &cobra.Command{
	Use:   "log <task-id> <duration>",
	Short: "Log time manually (e.g. 2h, 30m, 1h30m)",
	Args:  cobra.ExactArgs(2),
	RunE:  runTimerLog,
}

var timerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active timer",
	RunE:  runTimerStatus,
}

var timerReportTaskID string

var timerReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Show time report",
	RunE:  runTimerReport,
}

func init() {
	rootCmd.AddCommand(timerCmd)
	timerCmd.AddCommand(timerStartCmd)
	timerCmd.AddCommand(timerStopCmd)
	timerCmd.AddCommand(timerLogCmd)
	timerCmd.AddCommand(timerStatusCmd)
	timerCmd.AddCommand(timerReportCmd)

	timerReportCmd.Flags().StringVar(&timerReportTaskID, "task", "", "Show report for a specific task")
}

func runTimerStart(cmd *cobra.Command, args []string) error {
	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}
	taskID := task.ID

	// Check if there's already an active timer for this task
	var count int64
	if err := db.GetDB().Model(&models.TimeEntry{}).
		Where("task_id = ? AND stopped_at IS NULL", taskID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check active timers: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("task '%s' already has an active timer", taskID)
	}

	now := time.Now()
	entry := models.TimeEntry{
		TaskID:    taskID,
		StartedAt: now,
	}
	if err := db.GetDB().Create(&entry).Error; err != nil {
		return fmt.Errorf("failed to start timer: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"started": true, "id": entry.ID, "task_id": taskID, "started_at": now})
		return nil
	}

	fmt.Printf("Timer started for task %s at %s\n", taskID, now.Format("15:04:05"))
	return nil
}

func runTimerStop(cmd *cobra.Command, args []string) error {
	var entry models.TimeEntry
	if err := db.GetDB().Where("stopped_at IS NULL").First(&entry).Error; err != nil {
		return fmt.Errorf("no active timer found")
	}

	now := time.Now()
	duration := int64(now.Sub(entry.StartedAt).Seconds())
	entry.StoppedAt = &now
	entry.Duration = duration

	if err := db.GetDB().Save(&entry).Error; err != nil {
		return fmt.Errorf("failed to stop timer: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{
			"stopped":    true,
			"id":         entry.ID,
			"task_id":    entry.TaskID,
			"duration":   duration,
			"formatted":  models.FormatDuration(duration),
			"stopped_at": now,
		})
		return nil
	}

	fmt.Printf("Timer stopped for task %s: %s\n", entry.TaskID, models.FormatDuration(duration))
	return nil
}

// durationPattern matches durations like "2h", "30m", "1h30m"
var durationPattern = regexp.MustCompile(`^(?:(\d+)h)?(?:(\d+)m)?$`)

// ParseDuration parses a duration string like "2h", "30m", "1h30m" into seconds
func ParseDuration(s string) (int64, error) {
	matches := durationPattern.FindStringSubmatch(s)
	if matches == nil || (matches[1] == "" && matches[2] == "") {
		return 0, fmt.Errorf("invalid duration format '%s' (use e.g. 2h, 30m, 1h30m)", s)
	}

	var total int64
	if matches[1] != "" {
		h, _ := strconv.ParseInt(matches[1], 10, 64)
		total += h * 3600
	}
	if matches[2] != "" {
		m, _ := strconv.ParseInt(matches[2], 10, 64)
		total += m * 60
	}
	return total, nil
}

func runTimerLog(cmd *cobra.Command, args []string) error {
	durationStr := args[1]

	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}
	taskID := task.ID

	duration, err := ParseDuration(durationStr)
	if err != nil {
		return err
	}

	now := time.Now()
	startedAt := now.Add(-time.Duration(duration) * time.Second)
	entry := models.TimeEntry{
		TaskID:    taskID,
		StartedAt: startedAt,
		StoppedAt: &now,
		Duration:  duration,
	}
	if err := db.GetDB().Create(&entry).Error; err != nil {
		return fmt.Errorf("failed to log time: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{
			"logged":    true,
			"id":        entry.ID,
			"task_id":   taskID,
			"duration":  duration,
			"formatted": models.FormatDuration(duration),
		})
		return nil
	}

	fmt.Printf("Logged %s for task %s\n", models.FormatDuration(duration), taskID)
	return nil
}

func runTimerStatus(cmd *cobra.Command, args []string) error {
	var entry models.TimeEntry
	if err := db.GetDB().Where("stopped_at IS NULL").First(&entry).Error; err != nil {
		if IsJSONOutput() {
			OutputJSON(map[string]interface{}{"active": false})
			return nil
		}
		fmt.Println("No active timer")
		return nil
	}

	elapsed := int64(time.Since(entry.StartedAt).Seconds())

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{
			"active":     true,
			"id":         entry.ID,
			"task_id":    entry.TaskID,
			"started_at": entry.StartedAt,
			"elapsed":    elapsed,
			"formatted":  models.FormatDuration(elapsed),
		})
		return nil
	}

	fmt.Printf("Active timer for task %s: %s (started %s)\n",
		entry.TaskID,
		models.FormatDuration(elapsed),
		entry.StartedAt.Format("15:04:05"))
	return nil
}

func runTimerReport(cmd *cobra.Command, args []string) error {
	type reportRow struct {
		TaskID   string
		Total    int64
		EntryCnt int64
	}

	query := db.GetDB().Model(&models.TimeEntry{}).
		Select("task_id, SUM(duration) as total, COUNT(*) as entry_cnt").
		Where("stopped_at IS NOT NULL").
		Group("task_id").
		Order("total DESC")

	if timerReportTaskID != "" {
		query = query.Where("task_id = ?", timerReportTaskID)
	}

	var rows []reportRow
	if err := query.Find(&rows).Error; err != nil {
		return fmt.Errorf("failed to generate report: %w", err)
	}

	if IsJSONOutput() {
		type jsonRow struct {
			TaskID    string `json:"task_id"`
			Total     int64  `json:"total_seconds"`
			Formatted string `json:"formatted"`
			Entries   int64  `json:"entries"`
		}
		var jrows []jsonRow
		for _, r := range rows {
			jrows = append(jrows, jsonRow{
				TaskID:    r.TaskID,
				Total:     r.Total,
				Formatted: models.FormatDuration(r.Total),
				Entries:   r.EntryCnt,
			})
		}
		OutputJSON(map[string]interface{}{"count": len(jrows), "report": jrows})
		return nil
	}

	if len(rows) == 0 {
		fmt.Println("No time entries found")
		return nil
	}

	fmt.Printf("%-20s %-12s %s\n", "Task", "Time", "Entries")
	fmt.Printf("%-20s %-12s %s\n", "----", "----", "-------")
	for _, r := range rows {
		fmt.Printf("%-20s %-12s %d\n", r.TaskID, models.FormatDuration(r.Total), r.EntryCnt)
	}
	return nil
}
