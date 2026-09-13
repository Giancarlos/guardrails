package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var agentAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Register an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentAdd,
}

func init() {
	agentCmd.AddCommand(agentAddCmd)

	agentAddCmd.Flags().StringVar(&agentPath, "path", "", "Full path to agent file")
	agentAddCmd.Flags().StringVar(&agentSource, "source", models.SourceCustom, "Source (claude/cursor/windsurf/copilot/custom)")
	agentAddCmd.Flags().StringVar(&agentDescription, "description", "", "Agent description")
	agentAddCmd.Flags().StringVar(&agentCapabilities, "capabilities", "", "Agent capabilities")
}

func runAgentAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Check if already exists
	var existing models.Agent
	if err := db.GetDB().Where("name = ?", name).First(&existing).Error; err == nil {
		return fmt.Errorf("cannot add agent: agent '%s' already exists (use 'gur agent show %s' to view it)", name, name)
	}

	agent := models.Agent{
		Name:         name,
		Path:         agentPath,
		Source:       agentSource,
		Description:  agentDescription,
		Capabilities: agentCapabilities,
	}

	if err := db.GetDB().Create(&agent).Error; err != nil {
		return fmt.Errorf("failed to register agent '%s': database error: %w", name, err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "agent": agent})
	} else {
		fmt.Printf("Registered agent: %s\n", name)
	}
	return nil
}
