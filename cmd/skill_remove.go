package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var skillRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Short:   "Unregister a skill",
	Aliases: []string{"rm"},
	Args:    cobra.ExactArgs(1),
	RunE:    runSkillRemove,
}

func init() {
	skillCmd.AddCommand(skillRemoveCmd)
}

func runSkillRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	var skill models.Skill
	if err := db.GetDB().Where("name = ?", name).First(&skill).Error; err != nil {
		return fmt.Errorf("cannot remove skill: skill '%s' not found (use 'gur skill list' to see registered skills)", name)
	}

	// Remove task links first
	if err := db.GetDB().Where("skill_id = ?", skill.ID).Delete(&models.TaskSkillLink{}).Error; err != nil {
		return fmt.Errorf("failed to remove skill '%s': could not delete task links: %w", name, err)
	}

	if err := db.GetDB().Delete(&skill).Error; err != nil {
		return fmt.Errorf("failed to remove skill '%s': database error: %w", name, err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "message": fmt.Sprintf("Removed skill: %s", name)})
	} else {
		fmt.Printf("Removed skill: %s\n", name)
	}
	return nil
}
