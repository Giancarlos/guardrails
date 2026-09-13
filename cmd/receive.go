package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var receiveCmd = &cobra.Command{
	Use:   "receive <task-id>",
	Short: "Accept or reject a pending handoff",
	Long: `Accept or reject the most recent pending handoff for a task addressed to the specified agent.

Examples:
  gur receive gur-abc12345 --agent claude
  gur receive gur-abc12345 --agent gemini --reject`,
	Args: cobra.ExactArgs(1),
	RunE: runReceive,
}

var (
	receiveAgent  string
	receiveReject bool
)

func init() {
	rootCmd.AddCommand(receiveCmd)

	receiveCmd.Flags().StringVar(&receiveAgent, "agent", "", "Agent receiving the handoff (required)")
	receiveCmd.Flags().BoolVar(&receiveReject, "reject", false, "Reject the handoff instead of accepting")
	receiveCmd.MarkFlagRequired("agent")
}

func runReceive(cmd *cobra.Command, args []string) error {
	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}
	taskID := task.ID

	// Find most recent pending handoff for this task+agent
	var h models.Handoff
	err = db.GetDB().
		Where("task_id = ? AND to_agent = ? AND status = ?", taskID, receiveAgent, models.HandoffPending).
		Order("created_at DESC").
		First(&h).Error
	if err != nil {
		return fmt.Errorf("no pending handoff found for task '%s' to agent '%s'", taskID, receiveAgent)
	}

	now := time.Now()
	if receiveReject {
		h.Status = models.HandoffRejected
	} else {
		h.Status = models.HandoffAccepted
	}
	h.AcceptedAt = &now

	if err := db.GetDB().Save(&h).Error; err != nil {
		return fmt.Errorf("failed to update handoff: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "handoff": h})
	} else if IsCompactOutput() {
		fmt.Printf("ok: %s %s\n", h.ID, h.Status)
	} else {
		fmt.Printf("%s: %s (%s -> %s) for task %s\n", h.StatusString(), h.ID, h.FromAgent, h.ToAgent, taskID)
	}
	return nil
}
