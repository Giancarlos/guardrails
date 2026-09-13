package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var gateUnlinkCmd = &cobra.Command{
	Use:   "unlink <gate-id> <task-id>",
	Short: "Unlink a gate from a task",
	Args:  cobra.ExactArgs(2),
	RunE:  runGateUnlink,
}

func init() {
	gateCmd.AddCommand(gateUnlinkCmd)
}

func runGateUnlink(cmd *cobra.Command, args []string) error {
	gateID, taskID := args[0], args[1]
	database := db.GetDB()

	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(taskID)
	if err != nil {
		return err
	}
	taskID = task.ID

	// Find the link first to check its status
	var link models.GateTaskLink
	if err := database.Where("gate_id = ? AND task_id = ?", gateID, taskID).First(&link).Error; err != nil {
		return fmt.Errorf("cannot unlink gate: no link exists between gate '%s' and task '%s' (use 'gur gate show %s' to see linked tasks)",
			gateID, taskID, gateID)
	}

	// Warn if the link has verification status
	if link.Status == models.GateLinkPassed {
		fmt.Fprintf(os.Stderr, "WARNING: This gate was verified as PASSED for this task.\n")
		fmt.Fprintf(os.Stderr, "Unlinking will delete this verification status.\n")
		fmt.Fprintf(os.Stderr, "If you re-link, the gate will need to be verified again.\n\n")
	} else if link.Status == models.GateLinkFailed {
		fmt.Fprintf(os.Stderr, "WARNING: This gate has a FAILED status for this task.\n")
		fmt.Fprintf(os.Stderr, "Unlinking will delete this status record.\n\n")
	}

	// Delete the link
	if err := database.Delete(&link).Error; err != nil {
		return fmt.Errorf("failed to unlink gate: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "warning": link.Status != "" && link.Status != models.GateLinkPending})
	} else if IsCompactOutput() {
		fmt.Println("ok: unlinked")
	} else {
		fmt.Println("Unlinked gate from task")
		fmt.Println("Note: Any verification status for this task-gate pair has been deleted.")
	}
	return nil
}
