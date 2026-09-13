package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var budgetWarn int64

var budgetCmd = &cobra.Command{
	Use:   "budget",
	Short: "Show token usage summary across open tasks",
	Args:  cobra.NoArgs,
	RunE:  runBudget,
}

func init() {
	rootCmd.AddCommand(budgetCmd)
	budgetCmd.Flags().Int64Var(&budgetWarn, "warn", 0, "Exit code 1 if total tokens exceed threshold")
}

func runBudget(cmd *cobra.Command, args []string) error {
	if budgetWarn < 0 {
		return fmt.Errorf("invalid --warn %d: must be zero or greater", budgetWarn)
	}

	var tasks []models.Task
	err := db.GetDB().
		Where("status IN ? AND (tokens_used > 0 OR tokens_budget > 0)", []string{models.StatusOpen, models.StatusInProgress}).
		Order("tokens_used DESC").
		Find(&tasks).Error
	if err != nil {
		return err
	}

	if IsJSONOutput() {
		var totalUsed, totalBudget int64
		for _, t := range tasks {
			totalUsed += t.TokensUsed
			totalBudget += t.TokensBudget
		}
		OutputJSON(map[string]interface{}{
			"tasks":        tasks,
			"total_used":   totalUsed,
			"total_budget": totalBudget,
			"count":        len(tasks),
		})
		if budgetWarn > 0 && totalUsed > budgetWarn {
			os.Exit(1)
		}
		return nil
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks with token usage found")
		return nil
	}

	// Print header
	fmt.Printf("%-16s %-30s %10s %10s %6s\n", "ID", "TITLE", "USED", "BUDGET", "%")
	fmt.Println("--------------------------------------------------------------------------")

	var totalUsed, totalBudget int64
	for _, t := range tasks {
		// Truncate by characters, not bytes, so multi-byte titles aren't split mid-character
		title := t.Title
		if runes := []rune(title); len(runes) > 30 {
			title = string(runes[:27]) + "..."
		}

		pct := ""
		if t.TokensBudget > 0 {
			pct = fmt.Sprintf("%.0f%%", float64(t.TokensUsed)*100/float64(t.TokensBudget))
		}

		budget := "-"
		if t.TokensBudget > 0 {
			budget = fmt.Sprintf("%d", t.TokensBudget)
		}

		fmt.Printf("%-16s %-30s %10d %10s %6s\n", t.ID, title, t.TokensUsed, budget, pct)
		totalUsed += t.TokensUsed
		totalBudget += t.TokensBudget
	}

	// Totals row
	fmt.Println("--------------------------------------------------------------------------")
	totalPct := ""
	if totalBudget > 0 {
		totalPct = fmt.Sprintf("%.0f%%", float64(totalUsed)*100/float64(totalBudget))
	}
	totalBudgetStr := "-"
	if totalBudget > 0 {
		totalBudgetStr = fmt.Sprintf("%d", totalBudget)
	}
	fmt.Printf("%-16s %-30s %10d %10s %6s\n", "", "TOTAL", totalUsed, totalBudgetStr, totalPct)

	if budgetWarn > 0 && totalUsed > budgetWarn {
		fmt.Fprintf(os.Stderr, "\nWARNING: Total token usage (%d) exceeds threshold (%d)\n", totalUsed, budgetWarn)
		os.Exit(1)
	}

	return nil
}
