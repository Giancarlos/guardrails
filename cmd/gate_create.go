package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var gateCreateCmd = &cobra.Command{
	Use:   "create \"title\"",
	Short: "Create a new gate",
	Long: `Create a new quality gate.

SUGGESTED TYPES (or use any custom type):
  test       - Automated or manual test
  review     - Code review
  approval   - Sign-off from someone
  manual     - Manual verification
  deploy     - Deployment check
  qa         - QA verification
  security   - Security scan/review
  doc        - Documentation check

Examples:
  gur gate create "Unit tests pass" --type test --category backend
  gur gate create "Code review approved" --type review
  gur gate create "PM sign-off" --type approval
  gur gate create "Security scan" --type security --cmd "npm audit"`,
	Args: cobra.ExactArgs(1),
	RunE: runGateCreate,
}

func init() {
	gateCmd.AddCommand(gateCreateCmd)

	gateCreateCmd.Flags().StringVar(&gateCategory, "category", "", "Category (e.g., auth, api, ui)")
	gateCreateCmd.Flags().StringVar(&gateType, "type", "manual", "Type (e.g., test, review, approval, manual)")
	gateCreateCmd.Flags().IntVar(&gatePriority, "priority", 2, "Priority (0-4)")
	gateCreateCmd.Flags().StringArrayVar(&gateLabels, "label", nil, "Labels")
	gateCreateCmd.Flags().StringVar(&gatePrecond, "pre", "", "Preconditions")
	gateCreateCmd.Flags().StringVar(&gateSteps, "steps", "", "Steps to verify")
	gateCreateCmd.Flags().StringVar(&gateExpected, "expected", "", "Expected result")
	gateCreateCmd.Flags().StringVar(&gateCommand, "cmd", "", "Command to run (for automated gates)")
	gateCreateCmd.Flags().StringVar(&gateDescription, "description", "", "Description")
}

func runGateCreate(cmd *cobra.Command, args []string) error {
	gate := &models.Gate{
		Title:          args[0],
		Description:    gateDescription,
		Category:       gateCategory,
		Type:           gateType,
		Priority:       gatePriority,
		Preconditions:  gatePrecond,
		Steps:          gateSteps,
		ExpectedResult: gateExpected,
		Command:        gateCommand,
		Labels:         gateLabels,
		LastResult:     models.GatePending,
	}

	if err := db.GetDB().Create(gate).Error; err != nil {
		return err
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "gate": gate})
	} else if IsCompactOutput() {
		fmt.Printf("ok: %s %s\n", gate.ID, gate.Title)
	} else {
		fmt.Printf("Created: %s - %s\n", gate.ID, gate.Title)
		if gate.Category != "" {
			fmt.Printf("  Category: %s\n", gate.Category)
		}
		fmt.Printf("  Type: %s\n", gate.TypeString())
	}
	return nil
}
