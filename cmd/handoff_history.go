package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var handoffHistoryCmd = &cobra.Command{
	Use:     "handoff-history <task-id>",
	Short:   "List handoff history for a task",
	Aliases: []string{"handoffs"},
	Long: `List all handoffs for a task in chronological order.

Examples:
  gur handoff-history gur-abc12345
  gur handoffs gur-abc12345 --limit 10`,
	Args: cobra.ExactArgs(1),
	RunE: runHandoffHistory,
}

var handoffHistoryLimit int

func init() {
	rootCmd.AddCommand(handoffHistoryCmd)

	handoffHistoryCmd.Flags().IntVar(&handoffHistoryLimit, "limit", 50, "Maximum number of handoffs to show")
}

func runHandoffHistory(cmd *cobra.Command, args []string) error {
	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}
	taskID := task.ID

	var handoffs []models.Handoff
	err = db.GetDB().Where("task_id = ?", taskID).
		Order("created_at ASC").
		Limit(handoffHistoryLimit).
		Find(&handoffs).Error
	if err != nil {
		return fmt.Errorf("failed to query handoffs: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"count": len(handoffs), "handoffs": handoffs})
		return nil
	}

	if len(handoffs) == 0 {
		fmt.Println("No handoffs found")
		return nil
	}

	for _, h := range handoffs {
		summary := h.Summary
		if summary == "" && h.ContextData != "" {
			summary = "(context data only)"
		}
		if len(summary) > 60 {
			summary = summary[:57] + "..."
		}
		fmt.Printf("[%s] %s %s -> %s [%s] %s\n",
			h.ID,
			h.CreatedAt.Format(models.DateTimeShortFormat),
			h.FromAgent,
			h.ToAgent,
			h.StatusString(),
			summary,
		)
	}
	return nil
}
