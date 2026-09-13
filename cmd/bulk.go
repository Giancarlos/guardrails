package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

// Shared flags for bulk subcommands
var (
	bulkStatus   string // --status filter (required for targeting)
	bulkPriority int    // --priority filter
	bulkType     string // --type filter
	bulkAssignee string // --assignee filter
	bulkDryRun   bool   // --dry-run
)

var bulkCmd = &cobra.Command{
	Use:   "bulk",
	Short: "Bulk operations on tasks",
	Long: `Perform bulk operations on multiple tasks at once.

All subcommands require at least one filter flag to target tasks.
Use --dry-run to preview changes before applying.

Examples:
  gur bulk update --status open --set-status in_progress
  gur bulk close --status open --reason "Sprint ended"
  gur bulk label --status open --add urgent
  gur bulk label --status in_progress --remove stale --dry-run`,
}

// Flags for bulk update
var (
	bulkSetStatus   string
	bulkSetPriority int
	bulkSetAssignee string
)

var bulkUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Bulk update tasks",
	Long: `Update multiple tasks matching the filter criteria.

At least one --set-* flag is required to specify what to change.

Examples:
  gur bulk update --status open --set-status in_progress
  gur bulk update --status open --set-priority 1 --set-assignee alice`,
	RunE: runBulkUpdate,
}

// Flag for bulk close
var bulkCloseReason string

var bulkCloseCmd = &cobra.Command{
	Use:   "close",
	Short: "Bulk close tasks",
	Long: `Close multiple tasks matching the filter criteria.

Tasks with gates that have not all passed will be skipped with a warning.
Use --dry-run to preview which tasks would be closed or skipped.

Examples:
  gur bulk close --status open --reason "Sprint cleanup"
  gur bulk close --status open --reason "Cancelled" --dry-run`,
	RunE: runBulkClose,
}

// Flags for bulk label
var (
	bulkLabelAdd    string
	bulkLabelRemove string
)

var bulkLabelCmd = &cobra.Command{
	Use:   "label",
	Short: "Bulk add or remove labels",
	Long: `Add or remove labels from multiple tasks matching the filter criteria.

At least one of --add or --remove is required.

Examples:
  gur bulk label --status open --add urgent
  gur bulk label --status in_progress --remove stale`,
	RunE: runBulkLabel,
}

func init() {
	rootCmd.AddCommand(bulkCmd)

	// Shared filter flags on the parent command (inherited by subcommands)
	bulkCmd.PersistentFlags().StringVar(&bulkStatus, "status", "", "Filter by status (open, in_progress, closed)")
	bulkCmd.PersistentFlags().IntVar(&bulkPriority, "priority", -1, "Filter by priority (0-4)")
	bulkCmd.PersistentFlags().StringVar(&bulkType, "type", "", "Filter by type (task, bug, feature, epic)")
	bulkCmd.PersistentFlags().StringVar(&bulkAssignee, "assignee", "", "Filter by assignee")
	bulkCmd.PersistentFlags().BoolVar(&bulkDryRun, "dry-run", false, "Preview changes without applying")

	// bulk update
	bulkCmd.AddCommand(bulkUpdateCmd)
	bulkUpdateCmd.Flags().StringVar(&bulkSetStatus, "set-status", "", "Set status on matching tasks")
	bulkUpdateCmd.Flags().IntVar(&bulkSetPriority, "set-priority", -1, "Set priority on matching tasks (0-4)")
	bulkUpdateCmd.Flags().StringVar(&bulkSetAssignee, "set-assignee", "", "Set assignee on matching tasks")

	// bulk close
	bulkCmd.AddCommand(bulkCloseCmd)
	bulkCloseCmd.Flags().StringVar(&bulkCloseReason, "reason", "", "Reason for closing (required)")
	bulkCloseCmd.MarkFlagRequired("reason")

	// bulk label
	bulkCmd.AddCommand(bulkLabelCmd)
	bulkLabelCmd.Flags().StringVar(&bulkLabelAdd, "add", "", "Label to add")
	bulkLabelCmd.Flags().StringVar(&bulkLabelRemove, "remove", "", "Label to remove")
}

// queryMatchingTasks returns tasks matching the bulk filter criteria.
func queryMatchingTasks() ([]models.Task, error) {
	database := db.GetDB()
	query := database.Model(&models.Task{})

	hasFilter := false

	if bulkStatus != "" {
		query = query.Where("status = ?", bulkStatus)
		hasFilter = true
	}
	if bulkPriority >= 0 {
		query = query.Where("priority = ?", bulkPriority)
		hasFilter = true
	}
	if bulkType != "" {
		query = query.Where("type = ?", bulkType)
		hasFilter = true
	}
	if bulkAssignee != "" {
		query = query.Where("assignee = ?", bulkAssignee)
		hasFilter = true
	}

	if !hasFilter {
		return nil, fmt.Errorf("at least one filter flag is required (--status, --priority, --type, --assignee)")
	}

	var tasks []models.Task
	if err := query.Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}

	return tasks, nil
}

func runBulkUpdate(cmd *cobra.Command, args []string) error {
	setStatusChanged := cmd.Flags().Changed("set-status")
	setPriorityChanged := cmd.Flags().Changed("set-priority")
	setAssigneeChanged := cmd.Flags().Changed("set-assignee")

	if !setStatusChanged && !setPriorityChanged && !setAssigneeChanged {
		return fmt.Errorf("at least one --set-* flag is required (--set-status, --set-priority, --set-assignee)")
	}

	tasks, err := queryMatchingTasks()
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		if IsJSONOutput() {
			OutputJSON(map[string]interface{}{"updated": 0, "tasks": []interface{}{}})
		} else {
			fmt.Println("No tasks match the filter criteria.")
		}
		return nil
	}

	if bulkDryRun {
		if IsJSONOutput() {
			var ids []string
			for _, t := range tasks {
				ids = append(ids, t.ID)
			}
			OutputJSON(map[string]interface{}{
				"dry_run":      true,
				"would_update": len(tasks),
				"task_ids":     ids,
			})
		} else {
			fmt.Printf("Dry run: would update %d task(s):\n", len(tasks))
			for _, t := range tasks {
				var changes []string
				if setStatusChanged {
					changes = append(changes, fmt.Sprintf("status: %s -> %s", t.Status, bulkSetStatus))
				}
				if setPriorityChanged {
					changes = append(changes, fmt.Sprintf("priority: %d -> %d", t.Priority, bulkSetPriority))
				}
				if setAssigneeChanged {
					changes = append(changes, fmt.Sprintf("assignee: %s -> %s", t.Assignee, bulkSetAssignee))
				}
				fmt.Printf("  %s (%s) - %s\n", t.ID, strings.Join(changes, ", "), t.Title)
			}
		}
		return nil
	}

	database := db.GetDB()
	tx := database.Begin()
	updated := 0

	for _, task := range tasks {
		if setStatusChanged {
			models.RecordChange(tx, task.ID, "status", task.Status, bulkSetStatus, "user")
			task.Status = bulkSetStatus
		}
		if setPriorityChanged {
			models.RecordChange(tx, task.ID, "priority", fmt.Sprintf("%d", task.Priority), fmt.Sprintf("%d", bulkSetPriority), "user")
			task.Priority = bulkSetPriority
		}
		if setAssigneeChanged {
			models.RecordChange(tx, task.ID, "assignee", task.Assignee, bulkSetAssignee, "user")
			task.Assignee = bulkSetAssignee
		}
		if err := tx.Save(&task).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update task '%s': %w", task.ID, err)
		}
		updated++
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit bulk update: %w", err)
	}

	if IsJSONOutput() {
		var ids []string
		for _, t := range tasks {
			ids = append(ids, t.ID)
		}
		OutputJSON(map[string]interface{}{
			"success":  true,
			"updated":  updated,
			"task_ids": ids,
		})
	} else {
		fmt.Printf("Updated %d task(s)\n", updated)
	}
	return nil
}

func runBulkClose(cmd *cobra.Command, args []string) error {
	tasks, err := queryMatchingTasks()
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		if IsJSONOutput() {
			OutputJSON(map[string]interface{}{"closed": 0, "skipped": 0, "tasks": []interface{}{}})
		} else {
			fmt.Println("No tasks match the filter criteria.")
		}
		return nil
	}

	// Determine which tasks can be closed (gates check)
	type closeResult struct {
		task     models.Task
		canClose bool
		reason   string
	}
	var results []closeResult

	for _, task := range tasks {
		if task.IsClosed() {
			results = append(results, closeResult{task: task, canClose: false, reason: "already closed"})
			continue
		}
		gateErr := CheckGatesBeforeClose(task.ID)
		if gateErr != nil {
			results = append(results, closeResult{task: task, canClose: false, reason: "gates not passed"})
		} else {
			results = append(results, closeResult{task: task, canClose: true})
		}
	}

	if bulkDryRun {
		closeable := 0
		skippable := 0
		for _, r := range results {
			if r.canClose {
				closeable++
			} else {
				skippable++
			}
		}
		if IsJSONOutput() {
			var closeIDs, skipIDs []string
			for _, r := range results {
				if r.canClose {
					closeIDs = append(closeIDs, r.task.ID)
				} else {
					skipIDs = append(skipIDs, r.task.ID)
				}
			}
			OutputJSON(map[string]interface{}{
				"dry_run":     true,
				"would_close": closeable,
				"would_skip":  skippable,
				"close_ids":   closeIDs,
				"skip_ids":    skipIDs,
			})
		} else {
			fmt.Printf("Dry run: would close %d task(s), skip %d task(s):\n", closeable, skippable)
			for _, r := range results {
				if r.canClose {
					fmt.Printf("  %s (close) - %s\n", r.task.ID, r.task.Title)
				} else {
					fmt.Printf("  %s (skip: %s) - %s\n", r.task.ID, r.reason, r.task.Title)
				}
			}
		}
		return nil
	}

	database := db.GetDB()
	tx := database.Begin()
	closed := 0
	skipped := 0

	for _, r := range results {
		if !r.canClose {
			skipped++
			if !IsJSONOutput() {
				fmt.Printf("  Skipped: %s (%s) - %s\n", r.task.ID, r.reason, r.task.Title)
			}
			continue
		}
		task := r.task
		models.RecordChange(tx, task.ID, "status", task.Status, models.StatusClosed, "user")
		models.RecordChange(tx, task.ID, "close_reason", "", bulkCloseReason, "user")
		task.Close(bulkCloseReason)
		if err := tx.Save(&task).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to close task '%s': %w", task.ID, err)
		}
		closed++
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit bulk close: %w", err)
	}

	if IsJSONOutput() {
		var closedIDs, skippedIDs []string
		for _, r := range results {
			if r.canClose {
				closedIDs = append(closedIDs, r.task.ID)
			} else {
				skippedIDs = append(skippedIDs, r.task.ID)
			}
		}
		OutputJSON(map[string]interface{}{
			"success":     true,
			"closed":      closed,
			"skipped":     skipped,
			"closed_ids":  closedIDs,
			"skipped_ids": skippedIDs,
		})
	} else {
		fmt.Printf("Closed %d task(s) (%d skipped)\n", closed, skipped)
	}
	return nil
}

func runBulkLabel(cmd *cobra.Command, args []string) error {
	if bulkLabelAdd == "" && bulkLabelRemove == "" {
		return fmt.Errorf("at least one of --add or --remove is required")
	}

	tasks, err := queryMatchingTasks()
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		if IsJSONOutput() {
			OutputJSON(map[string]interface{}{"updated": 0, "tasks": []interface{}{}})
		} else {
			fmt.Println("No tasks match the filter criteria.")
		}
		return nil
	}

	if bulkDryRun {
		if IsJSONOutput() {
			var ids []string
			for _, t := range tasks {
				ids = append(ids, t.ID)
			}
			OutputJSON(map[string]interface{}{
				"dry_run":      true,
				"would_update": len(tasks),
				"task_ids":     ids,
				"add_label":    bulkLabelAdd,
				"remove_label": bulkLabelRemove,
			})
		} else {
			fmt.Printf("Dry run: would update labels on %d task(s):\n", len(tasks))
			for _, t := range tasks {
				var ops []string
				if bulkLabelAdd != "" {
					ops = append(ops, fmt.Sprintf("+%s", bulkLabelAdd))
				}
				if bulkLabelRemove != "" {
					ops = append(ops, fmt.Sprintf("-%s", bulkLabelRemove))
				}
				fmt.Printf("  %s (%s) - %s\n", t.ID, strings.Join(ops, ", "), t.Title)
			}
		}
		return nil
	}

	database := db.GetDB()
	tx := database.Begin()
	updated := 0

	for _, task := range tasks {
		if bulkLabelAdd != "" {
			task.AddLabel(bulkLabelAdd)
		}
		if bulkLabelRemove != "" {
			task.RemoveLabel(bulkLabelRemove)
		}
		if err := tx.Save(&task).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update labels for task '%s': %w", task.ID, err)
		}
		updated++
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit bulk label update: %w", err)
	}

	if IsJSONOutput() {
		var ids []string
		for _, t := range tasks {
			ids = append(ids, t.ID)
		}
		OutputJSON(map[string]interface{}{
			"success":  true,
			"updated":  updated,
			"task_ids": ids,
		})
	} else {
		fmt.Printf("Updated %d task(s)\n", updated)
	}
	return nil
}
