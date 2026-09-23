package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var skillAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Register a skill",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillAdd,
}

func init() {
	skillCmd.AddCommand(skillAddCmd)

	skillAddCmd.Flags().StringVar(&skillPath, "path", "", "Full path to skill file")
	skillAddCmd.Flags().StringVar(&skillSource, "source", models.SourceCustom, "Source (claude/cursor/windsurf/copilot/custom)")
	skillAddCmd.Flags().StringVar(&skillDescription, "description", "", "Skill description")
}

func runSkillAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Check if already exists
	var existing models.Skill
	if err := db.GetDB().Where("name = ?", name).First(&existing).Error; err == nil {
		return fmt.Errorf("cannot add skill: skill '%s' already exists (use 'gur skill show %s' to view it)", name, name)
	}

	skill := models.Skill{
		Name:        name,
		Path:        skillPath,
		Source:      skillSource,
		Description: skillDescription,
	}

	// If path provided, try to read description from SKILL.md
	if skillPath != "" && skillDescription == "" {
		if desc := extractSkillDescription(skillPath); desc != "" {
			skill.Description = desc
		}
	}

	if err := db.GetDB().Create(&skill).Error; err != nil {
		return fmt.Errorf("failed to register skill '%s': database error: %w", name, err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "skill": skill})
	} else {
		fmt.Printf("Registered skill: %s\n", name)
	}
	return nil
}
