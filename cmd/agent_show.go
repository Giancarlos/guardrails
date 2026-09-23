package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var agentShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show agent details",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentShow,
}

func init() {
	agentCmd.AddCommand(agentShowCmd)
}

func runAgentShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	var agent models.Agent
	if err := db.GetDB().Where("name = ? OR id = ?", name, name).First(&agent).Error; err != nil {
		return fmt.Errorf("agent '%s' not found (use 'gur agent list' to see registered agents, or 'gur agent scan' to auto-discover)", name)
	}

	// Get linked tasks
	var links []models.TaskAgentLink
	db.GetDB().Where("agent_id = ?", agent.ID).Find(&links)

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"agent": agent, "linked_tasks": len(links)})
		return nil
	}

	fmt.Printf("ID:           %d\n", agent.ID)
	fmt.Printf("Name:         %s\n", agent.Name)
	fmt.Printf("Source:       %s\n", agent.Source)
	if agent.Path != "" {
		fmt.Printf("Path:         %s\n", agent.Path)
	}
	if agent.Description != "" {
		fmt.Printf("Description:  %s\n", agent.Description)
	}
	if agent.Capabilities != "" {
		fmt.Printf("Capabilities: %s\n", agent.Capabilities)
	}
	fmt.Printf("Linked to:    %d task(s)\n", len(links))

	return nil
}
