package cmd

import (
	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage AI skills",
	Long: `Manage AI skills that can be linked to tasks.

Skills are SKILL.md files that provide domain-specific instructions for AI agents.
When a task has linked skills, the agent working on it will be informed which skills to use.`,
}

// Shared flag variables for skill subcommands
var (
	skillPath        string
	skillSource      string
	skillDescription string
)

func init() {
	rootCmd.AddCommand(skillCmd)
}
