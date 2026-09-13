package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var gateDeleteCmd = &cobra.Command{
	Use:     "delete <gate-id>",
	Aliases: []string{"rm"},
	Short:   "Delete a gate",
	Long: `Delete a gate permanently.

Cannot delete a gate that is linked to open tasks.
Unlink from open tasks first, or close those tasks.`,
	Args: cobra.ExactArgs(1),
	RunE: runGateDelete,
}

func init() {
	gateCmd.AddCommand(gateDeleteCmd)
}

func runGateDelete(cmd *cobra.Command, args []string) error {
	gateID := args[0]
	database := db.GetDB()

	// Check gate exists
	gate, err := db.GetGateByID(gateID)
	if err != nil {
		return fmt.Errorf("cannot delete gate: gate '%s' not found", gateID)
	}

	// Check for linked open tasks
	var openTaskLinks []models.GateTaskLink
	err = database.
		Joins("JOIN tasks ON tasks.id = gate_task_links.task_id").
		Where("gate_task_links.gate_id = ? AND gate_task_links.deleted_at IS NULL", gateID).
		Where("tasks.status NOT IN ?", []string{models.StatusClosed, models.StatusArchived}).
		Find(&openTaskLinks).Error
	if err != nil {
		return fmt.Errorf("failed to check linked tasks: %w", err)
	}

	if len(openTaskLinks) > 0 {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Cannot delete gate '%s': linked to %d open task(s):\n", gateID, len(openTaskLinks)))
		for _, link := range openTaskLinks {
			sb.WriteString(fmt.Sprintf("  - %s\n", link.TaskID))
		}
		sb.WriteString("\nUnlink from these tasks first, or close them:\n")
		for _, link := range openTaskLinks {
			sb.WriteString(fmt.Sprintf("  gur gate unlink %s %s\n", gateID, link.TaskID))
		}
		return fmt.Errorf("%s", sb.String())
	}

	// Delete all links to this gate (for closed/archived tasks)
	database.Where("gate_id = ?", gateID).Delete(&models.GateTaskLink{})

	// Delete gate runs
	database.Where("gate_id = ?", gateID).Delete(&models.GateRun{})

	// Delete the gate
	if err := database.Delete(gate).Error; err != nil {
		return fmt.Errorf("failed to delete gate: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "deleted": gateID})
	} else {
		fmt.Printf("Deleted gate: %s - %s\n", gate.ID, gate.Title)
	}
	return nil
}
