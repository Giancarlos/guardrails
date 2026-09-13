package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var (
	compactBefore  string
	compactAll     bool
	compactSummary bool
)

var compactCmd = &cobra.Command{
	Use:   "compact [task-id]",
	Short: "Compact closed tasks to save context space",
	Long: `Compact closed tasks by generating a summary and clearing verbose fields.

This is useful for AI agents to reduce context window usage while preserving
essential task information.

Examples:
  gur compact gur-abc123          # Compact a specific task
  gur compact --all               # Compact all closed tasks
  gur compact --before 7d         # Compact tasks closed more than 7 days ago
  gur compact --summary           # Show what would be compacted (dry run)`,
	RunE: runCompact,
}

func init() {
	rootCmd.AddCommand(compactCmd)
	compactCmd.Flags().StringVar(&compactBefore, "before", "", "Compact tasks closed before duration (e.g., 7d, 30d)")
	compactCmd.Flags().BoolVar(&compactAll, "all", false, "Compact all closed tasks")
	compactCmd.Flags().BoolVar(&compactSummary, "dry-run", false, "Show what would be compacted without making changes")
}

func runCompact(cmd *cobra.Command, args []string) error {
	database := db.GetDB()

	// Compact specific task
	if len(args) == 1 {
		taskID := args[0]
		var task models.Task
		if err := database.First(&task, "id = ?", taskID).Error; err != nil {
			return fmt.Errorf("cannot compact task: task '%s' not found (use 'gur list' to see available tasks)", taskID)
		}
		if task.Status != models.StatusClosed && task.Status != models.StatusArchived {
			return fmt.Errorf("cannot compact task '%s': only closed or archived tasks can be compacted (current status: %s)",
				taskID, task.Status)
		}
		if task.Compacted {
			return fmt.Errorf("cannot compact task '%s': task already compacted (summary: %s)", taskID, task.Summary)
		}

		if compactSummary {
			fmt.Printf("Would compact: %s - %s\n", task.ID, task.Title)
			fmt.Printf("  Description length: %d chars\n", len(task.Description))
			fmt.Printf("  Notes length: %d chars\n", len(task.Notes))
			return nil
		}

		task.Compact()
		if err := database.Save(&task).Error; err != nil {
			return fmt.Errorf("failed to compact task '%s': database error: %w", taskID, err)
		}

		if IsJSONOutput() {
			OutputJSON(map[string]interface{}{"compacted": taskID, "summary": task.Summary})
			return nil
		}
		fmt.Printf("Compacted: %s\n", taskID)
		fmt.Printf("Summary: %s\n", task.Summary)
		return nil
	}

	// Bulk compact
	if !compactAll && compactBefore == "" {
		return fmt.Errorf("missing argument: specify a task ID, use --all for all closed tasks, or --before <duration> (e.g., --before 7d)")
	}

	query := database.Model(&models.Task{}).
		Where("status IN ?", []string{models.StatusClosed, models.StatusArchived}).
		Where("compacted = ?", false)

	if compactBefore != "" {
		duration, err := parseDuration(compactBefore)
		if err != nil {
			return err
		}
		cutoff := time.Now().Add(-duration)
		query = query.Where("closed_at < ?", cutoff)
	}

	// Get tasks to compact
	var tasks []models.Task
	if err := query.Find(&tasks).Error; err != nil {
		return err
	}

	if compactSummary {
		if len(tasks) == 0 {
			fmt.Println("No tasks to compact")
			return nil
		}
		totalDescLen := 0
		totalNotesLen := 0
		for _, t := range tasks {
			totalDescLen += len(t.Description)
			totalNotesLen += len(t.Notes)
			fmt.Printf("Would compact: %s - %s\n", t.ID, t.Title)
		}
		fmt.Printf("\nTotal: %d tasks, %d chars in descriptions, %d chars in notes\n",
			len(tasks), totalDescLen, totalNotesLen)
		return nil
	}

	// Compact tasks in a transaction with batch updates
	compactedCount := 0
	err := database.Transaction(func(tx *gorm.DB) error {
		// Process in batches for memory efficiency
		const batchSize = 100
		for i := 0; i < len(tasks); i += batchSize {
			end := i + batchSize
			if end > len(tasks) {
				end = len(tasks)
			}
			batch := tasks[i:end]

			// Build batch update using CASE expression for summaries
			ids := make([]string, len(batch))
			summaries := make(map[string]string)
			for j, task := range batch {
				ids[j] = task.ID
				// Generate summary
				summary := task.Title
				if task.CloseReason != "" {
					summary += " | Closed: " + task.CloseReason
				}
				if task.Type != models.TypeTask {
					summary = "[" + task.Type + "] " + summary
				}
				summaries[task.ID] = summary
			}

			// Build CASE expression for summary field
			caseExpr := "CASE id"
			args := make([]interface{}, 0, len(batch)*2+len(batch))
			for _, id := range ids {
				caseExpr += " WHEN ? THEN ?"
				args = append(args, id, summaries[id])
			}
			caseExpr += " END"

			// Add IDs for WHERE clause
			for _, id := range ids {
				args = append(args, id)
			}

			// Single UPDATE for entire batch
			sql := fmt.Sprintf(`UPDATE tasks SET summary = %s, description = '', notes = '', compacted = true, updated_at = ? WHERE id IN (?%s)`,
				caseExpr, strings.Repeat(",?", len(ids)-1))
			args = append([]interface{}{time.Now()}, args...)

			if err := tx.Exec(sql, args...).Error; err != nil {
				return err
			}
		}
		compactedCount = len(tasks)
		return nil
	})
	if err != nil {
		return err
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"compacted_count": compactedCount})
		return nil
	}
	fmt.Printf("Compacted %d tasks\n", compactedCount)
	return nil
}
