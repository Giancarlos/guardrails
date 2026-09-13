package cmd

import (
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage AI agents",
	Long: `Manage AI agents that can be linked to tasks.

Agents are defined in files like AGENTS.md, CLAUDE.md, or custom agent
configurations. When a task has linked agents, the agent working on it
will be informed which agent to use or delegate to.`,
}

// Shared flag variables for agent subcommands
var (
	agentPath         string
	agentSource       string
	agentDescription  string
	agentCapabilities string
)

func init() {
	rootCmd.AddCommand(agentCmd)
}
