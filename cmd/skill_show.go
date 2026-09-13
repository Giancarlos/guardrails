package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var skillShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show skill details",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillShow,
}

func init() {
	skillCmd.AddCommand(skillShowCmd)
}

func runSkillShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	var skill models.Skill
	if err := db.GetDB().Where("name = ? OR id = ?", name, name).First(&skill).Error; err != nil {
		return fmt.Errorf("skill '%s' not found (use 'gur skill list' to see registered skills, or 'gur skill scan' to auto-discover)", name)
	}

	// Get linked tasks
	var links []models.TaskSkillLink
	db.GetDB().Where("skill_id = ?", skill.ID).Find(&links)

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"skill": skill, "linked_tasks": len(links)})
		return nil
	}

	fmt.Printf("ID:          %d\n", skill.ID)
	fmt.Printf("Name:        %s\n", skill.Name)
	fmt.Printf("Source:      %s\n", skill.Source)
	if skill.Path != "" {
		fmt.Printf("Path:        %s\n", skill.Path)
	}
	if skill.Description != "" {
		fmt.Printf("Description: %s\n", skill.Description)
	}
	fmt.Printf("Linked to:   %d task(s)\n", len(links))

	return nil
}
