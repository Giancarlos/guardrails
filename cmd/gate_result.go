package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var gatePassCmd = &cobra.Command{
	Use:   "pass <gate-id> <task-id>",
	Short: "Mark a gate as passed for a specific task",
	Long: `Mark a gate as passed for a specific task.

Each task requires its own gate verification - you cannot reuse a previous pass.

Examples:
  gur gate pass gate-abc123 gur-def456
  gur gate pass gate-abc123 gur-def456 --notes "All tests green"
  gur gate pass gate-abc123 gur-def456 --by agent`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGateResult(args[0], args[1], models.GateLinkPassed)
	},
}

var gateFailCmd = &cobra.Command{
	Use:   "fail <gate-id> <task-id>",
	Short: "Mark a gate as failed for a specific task",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGateResult(args[0], args[1], models.GateLinkFailed)
	},
}

var gateSkipCmd = &cobra.Command{
	Use:   "skip <gate-id> <task-id>",
	Short: "Mark a gate as skipped for a specific task (still blocks close)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGateResult(args[0], args[1], models.GateSkipped)
	},
}

func init() {
	gateCmd.AddCommand(gatePassCmd)
	gateCmd.AddCommand(gateFailCmd)
	gateCmd.AddCommand(gateSkipCmd)

	gatePassCmd.Flags().StringVar(&gateNotes, "notes", "", "Notes about the result")
	gatePassCmd.Flags().StringVar(&gateRunBy, "by", "human", "Who verified (human/agent/name)")
	gateFailCmd.Flags().StringVar(&gateNotes, "notes", "", "Notes about the result")
	gateFailCmd.Flags().StringVar(&gateRunBy, "by", "human", "Who verified (human/agent/name)")
	gateSkipCmd.Flags().StringVar(&gateNotes, "notes", "", "Notes about the result")
	gateSkipCmd.Flags().StringVar(&gateRunBy, "by", "human", "Who verified (human/agent/name)")
}

func runGateResult(gateID string, taskID string, result string) error {
	database := db.GetDB()

	// Validate gate exists
	gate, err := db.GetGateByID(gateID)
	if err != nil {
		return fmt.Errorf("cannot update gate: gate '%s' not found (use 'gur gate list' to see available gates)", gateID)
	}

	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(taskID)
	if err != nil {
		return err
	}
	taskID = task.ID

	// Find the link between gate and task
	var link models.GateTaskLink
	err = database.Where("gate_id = ? AND task_id = ?", gateID, taskID).First(&link).Error
	if err != nil {
		return fmt.Errorf("cannot update gate: gate '%s' is not linked to task '%s'\nLink it first: gur gate link %s %s", gateID, taskID, gateID, taskID)
	}

	// Update the per-task link status
	now := time.Now()
	link.Status = result
	link.VerifiedAt = &now
	link.VerifiedBy = gateRunBy
	link.Notes = gateNotes
	if err := database.Save(&link).Error; err != nil {
		return fmt.Errorf("failed to update gate link: %w", err)
	}

	// Also update global gate stats and save to GateRun history for audit
	gate.RecordRun(result, gateRunBy, gateNotes)
	if err := database.Save(&gate).Error; err != nil {
		return fmt.Errorf("failed to update gate stats: %w", err)
	}

	run := &models.GateRun{
		GateID: gateID,
		Result: result,
		RunBy:  gateRunBy,
		Notes:  gateNotes,
	}
	if err := database.Create(run).Error; err != nil {
		return fmt.Errorf("failed to save gate run history: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "gate": gate, "task": task, "link": link})
	} else {
		fmt.Printf("Verified: %s for task %s (%s by %s)\n", gate.Title, taskID, result, gateRunBy)
	}
	return nil
}
