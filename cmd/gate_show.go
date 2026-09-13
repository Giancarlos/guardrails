package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var gateShowCmd = &cobra.Command{
	Use:   "show <gate-id>",
	Short: "Show gate details",
	Args:  cobra.ExactArgs(1),
	RunE:  runGateShow,
}

func init() {
	gateCmd.AddCommand(gateShowCmd)
}

func runGateShow(cmd *cobra.Command, args []string) error {
	gate, err := db.GetGateByID(args[0])
	if err != nil {
		return fmt.Errorf("gate '%s' not found (use 'gur gate list' to see available gates)", args[0])
	}

	// Get linked tasks
	var links []models.GateTaskLink
	db.GetDB().Where("gate_id = ?", gate.ID).Find(&links)

	// Get recent runs
	var runs []models.GateRun
	db.GetDB().Where("gate_id = ?", gate.ID).Order("created_at DESC").Limit(5).Find(&runs)

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{
			"gate":         gate,
			"linked_tasks": links,
			"recent_runs":  runs,
		})
		return nil
	}

	if IsCompactOutput() {
		cat := ""
		if gate.Category != "" {
			cat = " [" + gate.Category + "]"
		}
		fmt.Printf("%s | P%d %s | %s (%s)%s | %d/%d passed\n",
			gate.ID, gate.Priority, gate.ResultString(), gate.Title, gate.TypeString(), cat,
			gate.PassCount, gate.RunCount)
		if gate.Description != "" {
			fmt.Printf("desc:%s\n", gate.Description)
		}
		if gate.Command != "" {
			fmt.Printf("cmd:%s\n", gate.Command)
		}
		if len(links) > 0 {
			parts := make([]string, len(links))
			for i, l := range links {
				status := l.Status
				if status == "" {
					status = "pending"
				}
				parts[i] = l.TaskID + ":" + status
			}
			fmt.Printf("tasks:%s\n", strings.Join(parts, " "))
		}
		return nil
	}

	fmt.Printf("ID:       %s\n", gate.ID)
	fmt.Printf("Title:    %s\n", gate.Title)
	fmt.Printf("Type:     %s\n", gate.TypeString())
	fmt.Printf("Priority: P%d\n", gate.Priority)
	fmt.Printf("Result:   %s\n", gate.ResultString())
	if gate.Category != "" {
		fmt.Printf("Category: %s\n", gate.Category)
	}
	if gate.Description != "" {
		fmt.Printf("Desc:     %s\n", gate.Description)
	}
	if gate.Preconditions != "" {
		fmt.Printf("\nPreconditions:\n%s\n", gate.Preconditions)
	}
	if gate.Steps != "" {
		fmt.Printf("\nSteps:\n%s\n", gate.Steps)
	}
	if gate.ExpectedResult != "" {
		fmt.Printf("\nExpected:\n%s\n", gate.ExpectedResult)
	}
	if gate.Command != "" {
		fmt.Printf("\nCommand: %s\n", gate.Command)
	}
	if len(gate.Labels) > 0 {
		fmt.Printf("Labels:   %v\n", gate.Labels)
	}

	fmt.Printf("\nStats: %d runs, %d passed, %d failed (%.0f%% pass rate)\n",
		gate.RunCount, gate.PassCount, gate.FailCount, gate.PassRate())

	if len(links) > 0 {
		fmt.Println("\nLinked tasks:")
		for _, l := range links {
			status := l.Status
			if status == "" {
				status = "pending"
			}
			fmt.Printf("  %s (%s)\n", l.TaskID, status)
		}
	}

	if len(runs) > 0 {
		fmt.Println("\nRecent runs:")
		for _, r := range runs {
			fmt.Printf("  %s - %s by %s\n", r.CreatedAt.Format(models.DateTimeShortFormat), r.Result, r.RunBy)
			if r.Notes != "" {
				fmt.Printf("    Notes: %s\n", r.Notes)
			}
		}
	}

	return nil
}
