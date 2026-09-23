package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var handoffCmd = &cobra.Command{
	Use:   "handoff <task-id>",
	Short: "Create a handoff to transfer a task between agents",
	Long: `Create a structured handoff to transfer a task from one agent to another.

At least one of --summary or --context must be provided.

Examples:
  gur handoff gur-abc12345 --from copilot --to claude --summary "API endpoints done, need tests"
  gur handoff gur-abc12345 --from claude --to gemini --context '{"completed":["api"],"remaining":["tests"]}'
  gur handoff gur-abc12345 --from claude --to copilot --summary "Auth ready" --context '{"files":["auth.go"]}'`,
	Args: cobra.ExactArgs(1),
	RunE: runHandoff,
}

var (
	handoffFrom    string
	handoffTo      string
	handoffSummary string
	handoffContext string
)

func init() {
	rootCmd.AddCommand(handoffCmd)

	handoffCmd.Flags().StringVar(&handoffFrom, "from", "", "Source agent (required)")
	handoffCmd.Flags().StringVar(&handoffTo, "to", "", "Target agent (required)")
	handoffCmd.Flags().StringVar(&handoffSummary, "summary", "", "Handoff summary")
	handoffCmd.Flags().StringVar(&handoffContext, "context", "", "Context data (JSON)")
	handoffCmd.MarkFlagRequired("from")
	handoffCmd.MarkFlagRequired("to")
}

func runHandoff(cmd *cobra.Command, args []string) error {
	if handoffSummary == "" && handoffContext == "" {
		return fmt.Errorf("at least one of --summary or --context is required")
	}
	if strings.TrimSpace(handoffFrom) == "" || strings.TrimSpace(handoffTo) == "" {
		return fmt.Errorf("--from and --to must not be empty")
	}

	// Resolve task (supports prefix matching).
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}
	taskID := task.ID
	if task.IsClosed() {
		return fmt.Errorf("cannot hand off task '%s': task is closed (reopen it first with 'gur reopen %s')", taskID, taskID)
	}
	if task.IsArchived() {
		return fmt.Errorf("cannot hand off task '%s': task is archived (restore it first with 'gur unarchive %s')", taskID, taskID)
	}

	// Validate JSON if provided
	if handoffContext != "" {
		if !json.Valid([]byte(handoffContext)) {
			return fmt.Errorf("--context must be valid JSON")
		}
	}

	h := &models.Handoff{
		TaskID:      taskID,
		FromAgent:   handoffFrom,
		ToAgent:     handoffTo,
		Summary:     handoffSummary,
		ContextData: handoffContext,
		Status:      models.HandoffPending,
	}

	if err := db.GetDB().Create(h).Error; err != nil {
		return fmt.Errorf("failed to create handoff: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "handoff": h})
	} else if IsCompactOutput() {
		fmt.Printf("ok: %s\n", h.ID)
	} else {
		fmt.Printf("Created: %s (%s -> %s) for task %s\n", h.ID, h.FromAgent, h.ToAgent, taskID)
	}
	return nil
}
