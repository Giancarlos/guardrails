package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var checkpointsCmd = &cobra.Command{
	Use:   "checkpoints <task-id>",
	Short: "List checkpoints for a task",
	Long: `List all checkpoints saved for a task, newest first.

Examples:
  gur checkpoints gur-abc12345
  gur checkpoints gur-abc12345 --limit 10`,
	Args: cobra.ExactArgs(1),
	RunE: runCheckpoints,
}

var checkpointsLimit int

func init() {
	rootCmd.AddCommand(checkpointsCmd)

	checkpointsCmd.Flags().IntVar(&checkpointsLimit, "limit", 50, "Maximum number of checkpoints to show")
}

func runCheckpoints(cmd *cobra.Command, args []string) error {
	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}
	taskID := task.ID

	var checkpoints []models.Checkpoint
	err = db.GetDB().Where("task_id = ?", taskID).
		Order("created_at DESC").
		Limit(checkpointsLimit).
		Find(&checkpoints).Error
	if err != nil {
		return fmt.Errorf("failed to query checkpoints: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"count": len(checkpoints), "checkpoints": checkpoints})
		return nil
	}

	if len(checkpoints) == 0 {
		fmt.Println("No checkpoints found")
		return nil
	}

	for _, chk := range checkpoints {
		agent := ""
		if chk.AgentID != "" {
			agent = " (" + chk.AgentID + ")"
		}
		state := chk.StateText
		if state == "" && chk.StateJSON != "" {
			state = chk.StateJSON
		}
		// Truncate long state for list view
		if len(state) > 80 {
			state = state[:77] + "..."
		}
		fmt.Printf("[%s] %s%s - %s\n", chk.ID, chk.CreatedAt.Format(models.DateTimeShortFormat), agent, state)
	}
	return nil
}
