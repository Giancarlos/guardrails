package models

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"gorm.io/gorm"
)

// HookTimeout bounds how long a single hook may run before it is killed
const HookTimeout = 30 * time.Second

// RunHooks finds and executes all enabled hooks for the given event
func RunHooks(database *gorm.DB, event string, task *Task) {
	var hooks []Hook
	if err := database.Where("event = ? AND enabled = ?", event, true).Find(&hooks).Error; err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load %s hooks: %v\n", event, err)
		return
	}
	for _, h := range hooks {
		// Run synchronously: the CLI exits right after the operation, which would
		// kill a hook started in a background goroutine before it ran.
		ctx, cancel := context.WithTimeout(context.Background(), HookTimeout)
		cmd := exec.CommandContext(ctx, "sh", "-c", h.Command)
		// Task data is passed only via environment variables, never interpolated into the command
		cmd.Env = append(os.Environ(),
			"GUR_TASK_ID="+task.ID,
			"GUR_TASK_TITLE="+task.Title,
			"GUR_TASK_STATUS="+task.Status,
			"GUR_EVENT="+event,
		)
		// Don't hang if the hook leaves a child process holding the output pipe open
		cmd.WaitDelay = 2 * time.Second
		output, err := cmd.CombinedOutput()
		cancel()
		// Log errors to stderr but don't fail the main operation
		if err != nil {
			fmt.Fprintf(os.Stderr, "Hook %s failed: %v\nOutput: %s\n", h.ID, err, output)
		}
	}
}
