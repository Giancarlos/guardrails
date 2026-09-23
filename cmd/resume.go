package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var resumeCmd = &cobra.Command{
	Use:   "resume <task-id>",
	Short: "Retrieve the latest checkpoint for a task",
	Long: `Retrieve the most recent checkpoint saved for a task.

Used to restore state when resuming work after a context window limit.

Examples:
  gur resume gur-abc12345
  gur resume gur-abc12345 --json`,
	Args: cobra.ExactArgs(1),
	RunE: runResume,
}

func init() {
	rootCmd.AddCommand(resumeCmd)
}

func runResume(cmd *cobra.Command, args []string) error {
	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}
	taskID := task.ID

	var chk models.Checkpoint
	err = db.GetDB().Where("task_id = ?", taskID).Order("created_at DESC").First(&chk).Error
	if err != nil {
		return fmt.Errorf("no checkpoints found for task '%s'", taskID)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"checkpoint": chk})
		return nil
	}

	if IsCompactOutput() {
		agent := ""
		if chk.AgentID != "" {
			agent = " | " + chk.AgentID
		}
		fmt.Printf("%s | %s | %s%s\n", chk.ID, chk.TaskID, chk.CreatedAt.Format(models.DateTimeShortFormat), agent)
		if chk.StateText != "" {
			fmt.Printf("state:%s\n", chk.StateText)
		}
		if chk.StateJSON != "" {
			fmt.Printf("data:%s\n", chk.StateJSON)
		}
		return nil
	}

	fmt.Printf("ID:      %s\n", chk.ID)
	fmt.Printf("Task:    %s\n", chk.TaskID)
	if chk.AgentID != "" {
		fmt.Printf("Agent:   %s\n", chk.AgentID)
	}
	fmt.Printf("Created: %s\n", chk.CreatedAt.Format(models.DateTimeShortFormat))
	if chk.StateText != "" {
		fmt.Printf("State:   %s\n", chk.StateText)
	}
	if chk.StateJSON != "" {
		fmt.Printf("Data:    %s\n", chk.StateJSON)
	}
	return nil
}
