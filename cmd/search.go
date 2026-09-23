package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search tasks",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
}

// escapeLikePattern escapes SQL LIKE wildcards in user input
func escapeLikePattern(s string) string {
	// Escape the escape character first, then special LIKE characters: % and _
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

func runSearch(cmd *cobra.Command, args []string) error {
	// Escape wildcards in user input to prevent pattern injection
	escaped := escapeLikePattern(strings.ToLower(args[0]))
	query := "%" + escaped + "%"

	// Use database-side filtering with LIKE for better performance
	// ESCAPE clause tells SQLite to use backslash as escape character
	var matches []models.Task
	if err := db.GetDB().
		Where("LOWER(title) LIKE ? ESCAPE '\\' OR LOWER(description) LIKE ? ESCAPE '\\'", query, query).
		Order("priority ASC, created_at DESC").
		Find(&matches).Error; err != nil {
		return err
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"count": len(matches), "tasks": matches})
		return nil
	}

	if len(matches) == 0 {
		fmt.Println("No matches found")
		return nil
	}

	for _, t := range matches {
		fmt.Printf("[%s] P%d %s - %s\n", t.ID, t.Priority, t.Status, t.Title)
	}
	return nil
}
