package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered agents",
	RunE:  runAgentList,
}

func init() {
	agentCmd.AddCommand(agentListCmd)
}

func runAgentList(cmd *cobra.Command, args []string) error {
	var agents []models.Agent
	if err := db.GetDB().Find(&agents).Error; err != nil {
		return err
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"count": len(agents), "agents": agents})
		return nil
	}

	if len(agents) == 0 {
		fmt.Println("No agents registered. Run 'gur agent scan' to auto-discover or 'gur agent add' to register manually.")
		return nil
	}

	fmt.Printf("Registered Agents (%d):\n", len(agents))
	for _, a := range agents {
		fmt.Printf("  [%d] %s", a.ID, a.Name)
		if a.Source != models.SourceCustom {
			fmt.Printf(" (%s)", a.Source)
		}
		if a.Description != "" {
			desc := a.Description
			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}
			fmt.Printf(" - %s", desc)
		}
		fmt.Println()
	}
	return nil
}
