package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var agentRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Short:   "Unregister an agent",
	Aliases: []string{"rm"},
	Args:    cobra.ExactArgs(1),
	RunE:    runAgentRemove,
}

func init() {
	agentCmd.AddCommand(agentRemoveCmd)
}

func runAgentRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	var agent models.Agent
	if err := db.GetDB().Where("name = ?", name).First(&agent).Error; err != nil {
		return fmt.Errorf("cannot remove agent: agent '%s' not found (use 'gur agent list' to see registered agents)", name)
	}

	// Remove task links first
	if err := db.GetDB().Where("agent_id = ?", agent.ID).Delete(&models.TaskAgentLink{}).Error; err != nil {
		return fmt.Errorf("failed to remove agent '%s': could not delete task links: %w", name, err)
	}

	if err := db.GetDB().Delete(&agent).Error; err != nil {
		return fmt.Errorf("failed to remove agent '%s': database error: %w", name, err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "message": fmt.Sprintf("Removed agent: %s", name)})
	} else {
		fmt.Printf("Removed agent: %s\n", name)
	}
	return nil
}
