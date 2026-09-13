package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var gateListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List gates",
	Aliases: []string{"ls"},
	RunE:    runGateList,
}

func init() {
	gateCmd.AddCommand(gateListCmd)

	gateListCmd.Flags().StringVar(&gateCategory, "category", "", "Filter by category")
	gateListCmd.Flags().StringVar(&gateType, "type", "", "Filter by type")
	gateListCmd.Flags().StringVar(&listStatus, "result", "", "Filter by last result")
}

func runGateList(cmd *cobra.Command, args []string) error {
	var gates []models.Gate
	query := db.GetDB().Order("priority ASC, category ASC, created_at DESC")

	if gateCategory != "" {
		query = query.Where("category = ?", gateCategory)
	}
	if gateType != "" {
		query = query.Where("type = ?", gateType)
	}
	if listStatus != "" {
		query = query.Where("last_result = ?", listStatus)
	}

	if err := query.Find(&gates).Error; err != nil {
		return err
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"count": len(gates), "gates": gates})
		return nil
	}

	if len(gates) == 0 {
		fmt.Println("No gates found")
		return nil
	}

	for _, g := range gates {
		cat := ""
		if g.Category != "" {
			cat = "[" + g.Category + "] "
		}
		fmt.Printf("[%s] %s%s - %s (%s)\n", g.ID, cat, g.ResultString(), g.Title, g.TypeString())
	}
	return nil
}
