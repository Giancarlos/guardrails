package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Manage hooks that fire on task events",
}

var hookAddCmd = &cobra.Command{
	Use:   "add <event> <command>",
	Short: "Register a hook for an event",
	Long: `Register a command to run when a task event occurs.

Valid events: on-create, on-update, on-close, on-reopen

The command receives environment variables:
  GUR_TASK_ID, GUR_TASK_TITLE, GUR_TASK_STATUS, GUR_EVENT`,
	Args: cobra.ExactArgs(2),
	RunE: runHookAdd,
}

var hookListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all hooks",
	RunE:    runHookList,
}

var hookRemoveCmd = &cobra.Command{
	Use:     "remove <id>",
	Aliases: []string{"rm"},
	Short:   "Remove a hook",
	Args:    cobra.ExactArgs(1),
	RunE:    runHookRemove,
}

var hookTestCmd = &cobra.Command{
	Use:   "test <event>",
	Short: "Dry-run all hooks for an event with a test task",
	Args:  cobra.ExactArgs(1),
	RunE:  runHookTest,
}

func init() {
	rootCmd.AddCommand(hookCmd)
	hookCmd.AddCommand(hookAddCmd)
	hookCmd.AddCommand(hookListCmd)
	hookCmd.AddCommand(hookRemoveCmd)
	hookCmd.AddCommand(hookTestCmd)
}

func runHookAdd(cmd *cobra.Command, args []string) error {
	event := args[0]
	command := args[1]

	if !models.IsValidHookEvent(event) {
		validEvents := []string{
			models.HookEventOnCreate,
			models.HookEventOnUpdate,
			models.HookEventOnClose,
			models.HookEventOnReopen,
		}
		return fmt.Errorf("invalid event '%s': must be one of: %s", event, strings.Join(validEvents, ", "))
	}

	hook := &models.Hook{
		Event:   event,
		Command: command,
		Enabled: true,
	}

	if err := db.GetDB().Create(hook).Error; err != nil {
		return fmt.Errorf("failed to create hook: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "hook": hook})
		return nil
	}
	fmt.Printf("Registered hook: %s on %s\n", hook.ID, event)
	return nil
}

func runHookList(cmd *cobra.Command, args []string) error {
	var hooks []models.Hook
	if err := db.GetDB().Order("created_at ASC").Find(&hooks).Error; err != nil {
		return fmt.Errorf("failed to list hooks: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"count": len(hooks), "hooks": hooks})
		return nil
	}

	if len(hooks) == 0 {
		fmt.Println("No hooks registered")
		return nil
	}

	for _, h := range hooks {
		enabled := "enabled"
		if !h.Enabled {
			enabled = "disabled"
		}
		fmt.Printf("[%s] %s -> %s (%s)\n", h.ID, h.Event, h.Command, enabled)
	}
	return nil
}

func runHookRemove(cmd *cobra.Command, args []string) error {
	id := args[0]

	result := db.GetDB().Where("id = ?", id).Delete(&models.Hook{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete hook: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("hook '%s' not found", id)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"deleted": id})
		return nil
	}
	fmt.Printf("Removed hook: %s\n", id)
	return nil
}

func runHookTest(cmd *cobra.Command, args []string) error {
	event := args[0]

	if !models.IsValidHookEvent(event) {
		validEvents := []string{
			models.HookEventOnCreate,
			models.HookEventOnUpdate,
			models.HookEventOnClose,
			models.HookEventOnReopen,
		}
		return fmt.Errorf("invalid event '%s': must be one of: %s", event, strings.Join(validEvents, ", "))
	}

	var hooks []models.Hook
	db.GetDB().Where("event = ? AND enabled = ?", event, true).Find(&hooks)

	testTask := &models.Task{
		ID:     "gur-test0000",
		Title:  "Test Task",
		Status: models.StatusOpen,
	}

	if IsJSONOutput() {
		type hookInfo struct {
			ID      string `json:"id"`
			Command string `json:"command"`
		}
		infos := make([]hookInfo, len(hooks))
		for i, h := range hooks {
			infos[i] = hookInfo{ID: h.ID, Command: h.Command}
		}
		OutputJSON(map[string]interface{}{
			"event":   event,
			"task":    testTask,
			"hooks":   infos,
			"dry_run": true,
			"would_set": map[string]string{
				"GUR_TASK_ID":     testTask.ID,
				"GUR_TASK_TITLE":  testTask.Title,
				"GUR_TASK_STATUS": testTask.Status,
				"GUR_EVENT":       event,
			},
		})
		return nil
	}

	if len(hooks) == 0 {
		fmt.Printf("No enabled hooks for event: %s\n", event)
		return nil
	}

	fmt.Printf("Dry run for event: %s (test task: %s)\n", event, testTask.ID)
	fmt.Printf("Environment:\n")
	fmt.Printf("  GUR_TASK_ID=%s\n", testTask.ID)
	fmt.Printf("  GUR_TASK_TITLE=%s\n", testTask.Title)
	fmt.Printf("  GUR_TASK_STATUS=%s\n", testTask.Status)
	fmt.Printf("  GUR_EVENT=%s\n", event)
	fmt.Printf("\nWould execute:\n")
	for _, h := range hooks {
		fmt.Printf("  [%s] %s\n", h.ID, h.Command)
	}
	return nil
}
