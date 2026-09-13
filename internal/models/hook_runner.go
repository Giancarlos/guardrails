package models

import (
	"fmt"
	"os"
	"os/exec"

	"gorm.io/gorm"
)

// RunHooks finds and executes all enabled hooks for the given event
func RunHooks(database *gorm.DB, event string, task *Task) {
	var hooks []Hook
	database.Where("event = ? AND enabled = ?", event, true).Find(&hooks)
	for _, h := range hooks {
		// Set environment variables for the hook
		// GUR_TASK_ID, GUR_TASK_TITLE, GUR_TASK_STATUS, GUR_EVENT
		cmd := exec.Command("sh", "-c", h.Command)
		cmd.Env = append(os.Environ(),
			"GUR_TASK_ID="+task.ID,
			"GUR_TASK_TITLE="+task.Title,
			"GUR_TASK_STATUS="+task.Status,
			"GUR_EVENT="+event,
		)
		// Run async - don't block the main operation
		// Log errors to stderr but don't fail
		go func(h Hook) {
			if output, err := cmd.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "Hook %s failed: %v\nOutput: %s\n", h.ID, err, output)
			}
		}(h)
	}
}
