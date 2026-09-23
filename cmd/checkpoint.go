package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var checkpointCmd = &cobra.Command{
	Use:   "checkpoint <task-id>",
	Short: "Save a checkpoint for a task",
	Long: `Save a checkpoint to preserve task state across context windows.

At least one of --state or --data must be provided.

Examples:
  gur checkpoint gur-abc12345 --state "Step 1 complete, starting step 2"
  gur checkpoint gur-abc12345 --data '{"step":2,"files":["main.go"]}' --agent claude
  gur checkpoint gur-abc12345 --state "Halfway done" --data '{"progress":50}'`,
	Args: cobra.ExactArgs(1),
	RunE: runCheckpoint,
}

var (
	checkpointState string
	checkpointData  string
	checkpointAgent string
)

func init() {
	rootCmd.AddCommand(checkpointCmd)

	checkpointCmd.Flags().StringVar(&checkpointState, "state", "", "State description (text)")
	checkpointCmd.Flags().StringVar(&checkpointData, "data", "", "State data (JSON)")
	checkpointCmd.Flags().StringVar(&checkpointAgent, "agent", "", "Agent that created this checkpoint")
}

func runCheckpoint(cmd *cobra.Command, args []string) error {
	if checkpointState == "" && checkpointData == "" {
		return fmt.Errorf("at least one of --state or --data is required")
	}

	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}
	taskID := task.ID

	// Validate JSON if provided
	if checkpointData != "" {
		if !json.Valid([]byte(checkpointData)) {
			return fmt.Errorf("--data must be valid JSON")
		}
	}

	chk := &models.Checkpoint{
		TaskID:    taskID,
		AgentID:   checkpointAgent,
		StateText: checkpointState,
		StateJSON: checkpointData,
	}

	if err := db.GetDB().Create(chk).Error; err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "checkpoint": chk})
	} else if IsCompactOutput() {
		fmt.Printf("ok: %s\n", chk.ID)
	} else {
		fmt.Printf("Saved: %s for task %s\n", chk.ID, taskID)
		if chk.AgentID != "" {
			fmt.Printf("  Agent: %s\n", chk.AgentID)
		}
	}
	return nil
}
