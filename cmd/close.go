package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var (
	closeReason string
	closeForce  bool
)

var closeCmd = &cobra.Command{
	Use:   "close <id>",
	Short: "Close a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runClose,
}

func init() {
	rootCmd.AddCommand(closeCmd)
	closeCmd.Flags().StringVar(&closeReason, "reason", "", "Reason for closing")
	closeCmd.Flags().BoolVar(&closeForce, "force", false, "Force close")
	closeCmd.MarkFlagRequired("reason")
}

func runClose(cmd *cobra.Command, args []string) error {
	database := db.GetDB()

	// First, find the task
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}

	if task.IsClosed() {
		return fmt.Errorf("cannot close task '%s': already closed on %s with reason: %s",
			task.ID, task.ClosedAt.Format(models.DateTimeShortFormat), task.CloseReason)
	}

	// Collect all gate check failures for force confirmation
	var gateCheckErr error

	if !closeForce {
		// Check for open blockers
		var blockerCount int64
		database.Model(&models.Dependency{}).
			Joins("JOIN tasks ON tasks.id = dependencies.parent_id").
			Where("dependencies.child_id = ? AND dependencies.type = ? AND tasks.status != ?",
				task.ID, models.DepTypeBlocks, models.StatusClosed).
			Count(&blockerCount)

		if blockerCount > 0 {
			return fmt.Errorf("cannot close task '%s': blocked by %d open task(s) (use 'gur show %s' to see blockers, or --force to override)",
				task.ID, blockerCount, task.ID)
		}

		// Check for open subtasks
		var openSubtasks int64
		database.Model(&models.Task{}).
			Where("parent_id = ? AND status != ?", task.ID, models.StatusClosed).
			Count(&openSubtasks)

		if openSubtasks > 0 {
			return fmt.Errorf("cannot close task '%s': has %d open subtask(s) (close subtasks first, or use --force to override)",
				task.ID, openSubtasks)
		}

		// Check for linked gates that haven't passed
		if err := CheckGatesBeforeClose(task.ID); err != nil {
			return err
		}
	} else {
		// --force was specified - require interactive confirmation
		// First check what we're bypassing
		gateCheckErr = CheckGatesBeforeClose(task.ID)

		if gateCheckErr != nil {
			// Require interactive terminal
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("--force requires interactive confirmation.\nCannot bypass gates from non-interactive terminal (e.g., scripts or AI agents).\n\n%s", gateCheckErr)
			}

			fmt.Println("WARNING: You are bypassing gate requirements!")
			fmt.Println()
			fmt.Println(gateCheckErr)
			fmt.Println()
			fmt.Printf("Task: %s - %s\n", task.ID, task.Title)
			fmt.Println()
			fmt.Print("Type 'yes' to force close this task: ")

			reader := bufio.NewReader(os.Stdin)
			confirmation, _ := reader.ReadString('\n')
			confirmation = strings.TrimSpace(strings.ToLower(confirmation))

			if confirmation != "yes" {
				return fmt.Errorf("force close cancelled")
			}

			fmt.Println()
			fmt.Println("Force closing task...")

			// Record that this was a force close
			closeReason = "[FORCE CLOSED] " + closeReason
		}
	}

	// Record history and close
	models.RecordChange(database, task.ID, "status", task.Status, models.StatusClosed, "user")
	models.RecordChange(database, task.ID, "close_reason", "", closeReason, "user")
	task.Close(closeReason)
	if err := database.Save(&task).Error; err != nil {
		return fmt.Errorf("failed to close task '%s': database error: %w", task.ID, err)
	}

	models.RunHooks(database, models.HookEventOnClose, task)

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "task": task, "forced": closeForce && gateCheckErr != nil})
	} else if IsCompactOutput() {
		fmt.Printf("ok: closed %s\n", task.ID)
	} else {
		fmt.Printf("Closed: %s\n", task.ID)
	}
	return nil
}
