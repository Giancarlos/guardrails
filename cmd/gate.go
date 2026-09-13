package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var gateCmd = &cobra.Command{
	Use:   "gate",
	Short: "Quality gate management",
	Long: `Manage quality gates for your project.

Gates are requirements that must pass before a task can be closed.
They can be tests, reviews, approvals, or any custom verification.

COMMON TYPES: test, review, approval, manual, deploy, qa, doc (or any custom type)
RESULTS: pending, passed, failed, skipped`,
}

// Shared flag variables for gate subcommands
var (
	gateCategory    string
	gateType        string
	gatePriority    int
	gateLabels      []string
	gatePrecond     string
	gateSteps       string
	gateExpected    string
	gateCommand     string
	gateDescription string
	gateNotes       string
	gateRunBy       string
)

func init() {
	rootCmd.AddCommand(gateCmd)
}

// GateLinkInfo contains gate info with its per-task link status
type GateLinkInfo struct {
	Gate   models.Gate
	Link   models.GateTaskLink
	Status string
}

// GetGateLinksForTask returns all gate links for a task with their per-task status
func GetGateLinksForTask(taskID string) ([]GateLinkInfo, error) {
	database := db.GetDB()

	var links []models.GateTaskLink
	if err := database.Where("task_id = ? AND deleted_at IS NULL", taskID).Find(&links).Error; err != nil {
		return nil, err
	}

	var result []GateLinkInfo
	for _, link := range links {
		gate, err := db.GetGateByID(link.GateID)
		if err != nil {
			continue
		}
		result = append(result, GateLinkInfo{
			Gate:   *gate,
			Link:   link,
			Status: link.Status,
		})
	}

	return result, nil
}

// GetFailingGateLinksForTask returns gates linked to a task where the per-task status is not "passed"
func GetFailingGateLinksForTask(taskID string) ([]GateLinkInfo, error) {
	links, err := GetGateLinksForTask(taskID)
	if err != nil {
		return nil, err
	}

	var failing []GateLinkInfo
	for _, info := range links {
		if info.Status != models.GateLinkPassed {
			failing = append(failing, info)
		}
	}

	return failing, nil
}

// GetLinkedGatesForTask returns all gates linked to a task
func GetLinkedGatesForTask(taskID string) ([]models.Gate, error) {
	database := db.GetDB()

	var gates []models.Gate
	err := database.
		Joins("JOIN gate_task_links ON gate_task_links.gate_id = gates.id").
		Where("gate_task_links.task_id = ? AND gate_task_links.deleted_at IS NULL", taskID).
		Find(&gates).Error

	if err != nil {
		return nil, err
	}

	return gates, nil
}

// CheckGatesBeforeClose checks if all linked gates have been verified as passed for this specific task.
// Tasks MUST have at least one gate linked to be closed.
// Each gate must be verified per-task - global gate status is not sufficient.
func CheckGatesBeforeClose(taskID string) error {
	gateLinks, err := GetGateLinksForTask(taskID)
	if err != nil {
		return err
	}

	// Require at least one gate to be linked
	if len(gateLinks) == 0 {
		return fmt.Errorf("Cannot close task: no gates linked.\n\nEvery task must have at least one gate before closing.\nLink a gate: gur gate link <gate-id> %s\nOr use --force to close anyway (requires interactive confirmation).", taskID)
	}

	failingLinks, err := GetFailingGateLinksForTask(taskID)
	if err != nil {
		return err
	}

	if len(failingLinks) > 0 {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Cannot close task: %d gate(s) not verified for this task:\n", len(failingLinks)))
		for _, info := range failingLinks {
			status := info.Status
			if status == "" {
				status = "pending"
			}
			sb.WriteString(fmt.Sprintf("  - %s: %s (status: %s)\n", info.Gate.ID, info.Gate.Title, status))
		}
		sb.WriteString(fmt.Sprintf("\nVerify gates for this task:\n"))
		for _, info := range failingLinks {
			sb.WriteString(fmt.Sprintf("  gur gate pass %s %s\n", info.Gate.ID, taskID))
		}
		sb.WriteString("\nOr use --force to close anyway (requires interactive confirmation).")
		return fmt.Errorf("%s", sb.String())
	}

	return nil
}
