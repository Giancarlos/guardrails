package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var gateLinkCmd = &cobra.Command{
	Use:   "link <gate-id> <task-id>",
	Short: "Link a gate to a task",
	Long: `Link a gate to a task as a requirement.

The task cannot be closed until this gate passes.

Example:
  gur gate link gate-abc123 gur-def456`,
	Args: cobra.ExactArgs(2),
	RunE: runGateLink,
}

func init() {
	gateCmd.AddCommand(gateLinkCmd)
}

func runGateLink(cmd *cobra.Command, args []string) error {
	gateID, taskID := args[0], args[1]
	database := db.GetDB()

	// Validate gate exists
	if _, err := db.GetGateByID(gateID); err != nil {
		return fmt.Errorf("cannot link gate: gate '%s' not found (use 'gur gate list' to see available gates)", gateID)
	}

	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(taskID)
	if err != nil {
		return err
	}
	taskID = task.ID

	// Check if already linked
	var existing models.GateTaskLink
	lookupErr := database.Where("gate_id = ? AND task_id = ?", gateID, taskID).First(&existing).Error
	if lookupErr == nil {
		return fmt.Errorf("cannot link gate: gate '%s' is already linked to task '%s'", gateID, taskID)
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return fmt.Errorf("cannot link gate: failed to check existing link: %w", lookupErr)
	}

	link := &models.GateTaskLink{
		GateID: gateID,
		TaskID: taskID,
		Status: models.GateLinkPending,
	}
	if err := database.Create(link).Error; err != nil {
		return fmt.Errorf("failed to link gate '%s' to task '%s': database error: %w", gateID, taskID, err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "link": link})
	} else if IsCompactOutput() {
		fmt.Printf("ok: %s -> %s\n", gateID, taskID)
	} else {
		fmt.Printf("Linked: %s -> %s (status: pending)\n", gateID, taskID)
		fmt.Println("Task cannot be closed until this gate is verified for this task.")
		fmt.Printf("Verify with: gur gate pass %s %s\n", gateID, taskID)
	}
	return nil
}
