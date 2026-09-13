package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var (
	listStatus   string
	listPriority int
	listType     string
	listAssignee string
	listLabel    string
	listArchived bool
	listLimit    int
	listOffset   int
	listSort     string
	listFilter   string
)

var listCmd = &cobra.Command{
	Use:     "list",
	Short:   "List tasks",
	Aliases: []string{"ls"},
	RunE:    runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().StringVar(&listStatus, "status", "", "Filter by status")
	listCmd.Flags().IntVar(&listPriority, "priority", -1, "Filter by priority")
	listCmd.Flags().StringVar(&listType, "type", "", "Filter by type")
	listCmd.Flags().StringVar(&listAssignee, "assignee", "", "Filter by assignee")
	listCmd.Flags().StringVar(&listLabel, "label", "", "Filter by label")
	listCmd.Flags().BoolVar(&listArchived, "archived", false, "Include archived tasks")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "Limit number of results (0 = no limit)")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "Skip first N results")
	listCmd.Flags().StringVar(&listSort, "sort", "", "Sort order: 'tokens' to sort by token usage descending")
	listCmd.Flags().StringVar(&listFilter, "filter", "", "Apply a saved filter by name")
}

func runList(cmd *cobra.Command, args []string) error {
	switch listSort {
	case "", "tokens":
	default:
		return fmt.Errorf("invalid --sort '%s': must be 'tokens'", listSort)
	}

	// Apply saved filter if specified (filter values act as defaults; explicit flags override)
	if listFilter != "" {
		sf, err := LoadSavedFilter(listFilter)
		if err != nil {
			return err
		}
		if sf.Status != "" && !cmd.Flags().Changed("status") {
			listStatus = sf.Status
		}
		if sf.Priority >= 0 && !cmd.Flags().Changed("priority") {
			listPriority = sf.Priority
		}
		if sf.Type != "" && !cmd.Flags().Changed("type") {
			listType = sf.Type
		}
		if sf.Assignee != "" && !cmd.Flags().Changed("assignee") {
			listAssignee = sf.Assignee
		}
		if sf.Label != "" && !cmd.Flags().Changed("label") {
			listLabel = sf.Label
		}
	}

	var tasks []models.Task
	orderClause := "priority ASC, created_at DESC"
	if listSort == "tokens" {
		orderClause = "tokens_used DESC, priority ASC"
	}
	query := db.GetDB().Order(orderClause)

	// Exclude archived by default unless --archived flag or filtering by archived status
	if !listArchived && listStatus != models.StatusArchived {
		query = query.Where("status != ?", models.StatusArchived)
	}

	if listStatus != "" {
		query = query.Where("status = ?", listStatus)
	}
	if listPriority >= 0 {
		query = query.Where("priority = ?", listPriority)
	}
	if listType != "" {
		query = query.Where("type = ?", listType)
	}
	if listAssignee != "" {
		query = query.Where("assignee = ?", listAssignee)
	}
	if listLabel != "" {
		// Labels are stored as a JSON array; match the quoted element exactly
		needle, _ := json.Marshal(listLabel)
		query = query.Where(`labels LIKE ? ESCAPE '\'`, "%"+escapeLikePattern(string(needle))+"%")
	}

	if listOffset > 0 {
		query = query.Offset(listOffset)
	}
	if listLimit > 0 {
		query = query.Limit(listLimit)
	}

	if err := query.Find(&tasks).Error; err != nil {
		return err
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"count": len(tasks), "tasks": tasks})
		return nil
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found")
		return nil
	}

	for _, t := range tasks {
		indent := ""
		depth := models.GetDepth(t.ID)
		for i := 0; i < depth; i++ {
			indent += "  "
		}
		if IsCompactOutput() {
			typeStr := ""
			if t.Type != models.TypeTask {
				typeStr = " (" + t.Type + ")"
			}
			fmt.Printf("%s%s P%d %s %s%s\n", indent, t.ID, t.Priority, t.Status, t.Title, typeStr)
		} else {
			fmt.Printf("%s[%s] P%d %s - %s (%s)\n", indent, t.ID, t.Priority, t.Status, t.Title, t.Type)
		}
	}
	return nil
}
