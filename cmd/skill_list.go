package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered skills",
	RunE:  runSkillList,
}

func init() {
	skillCmd.AddCommand(skillListCmd)
}

func runSkillList(cmd *cobra.Command, args []string) error {
	var skills []models.Skill
	if err := db.GetDB().Find(&skills).Error; err != nil {
		return err
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"count": len(skills), "skills": skills})
		return nil
	}

	if len(skills) == 0 {
		fmt.Println("No skills registered. Run 'gur skill scan' to auto-discover or 'gur skill add' to register manually.")
		return nil
	}

	fmt.Printf("Registered Skills (%d):\n", len(skills))
	for _, s := range skills {
		fmt.Printf("  [%d] %s", s.ID, s.Name)
		if s.Source != models.SourceCustom {
			fmt.Printf(" (%s)", s.Source)
		}
		if s.Description != "" {
			desc := s.Description
			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}
			fmt.Printf(" - %s", desc)
		}
		fmt.Println()
	}
	return nil
}
