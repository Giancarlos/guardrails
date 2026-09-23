package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Generate a session summary of recent task activity",
	RunE:  runSummary,
}

func init() {
	rootCmd.AddCommand(summaryCmd)
}

func runSummary(cmd *cobra.Command, args []string) error {
	database := db.GetDB()

	// Get counts by status using single GROUP BY query
	type statusCount struct {
		Status string
		Count  int64
	}
	var statusCounts []statusCount
	database.Model(&models.Task{}).
		Select("status, COUNT(*) as count").
		Group("status").
		Find(&statusCounts)

	// Map results
	var openCount, inProgressCount, closedCount, archivedCount int64
	for _, sc := range statusCounts {
		switch sc.Status {
		case models.StatusOpen:
			openCount = sc.Count
		case models.StatusInProgress:
			inProgressCount = sc.Count
		case models.StatusClosed:
			closedCount = sc.Count
		case models.StatusArchived:
			archivedCount = sc.Count
		}
	}

	// Get recent activity (last 24 hours) - combined query
	yesterday := time.Now().Add(-24 * time.Hour)
	var recentlyCreated, recentlyClosed int64
	database.Model(&models.Task{}).
		Select("SUM(CASE WHEN created_at > ? THEN 1 ELSE 0 END) as created, SUM(CASE WHEN closed_at > ? THEN 1 ELSE 0 END) as closed", yesterday, yesterday).
		Row().Scan(&recentlyCreated, &recentlyClosed)

	// Get high priority open tasks
	var highPriorityTasks []models.Task
	database.Where("status IN ? AND priority <= 1", []string{models.StatusOpen, models.StatusInProgress}).
		Order("priority ASC, created_at ASC").
		Limit(5).
		Find(&highPriorityTasks)

	// Get compacted vs uncompacted - combined query
	var compactedCount, uncompactedCount int64
	database.Model(&models.Task{}).
		Select("SUM(CASE WHEN compacted = true THEN 1 ELSE 0 END) as compacted, SUM(CASE WHEN compacted = false AND status IN (?, ?) THEN 1 ELSE 0 END) as uncompacted", models.StatusClosed, models.StatusArchived).
		Row().Scan(&compactedCount, &uncompactedCount)

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{
			"status_counts": map[string]int64{
				"open":        openCount,
				"in_progress": inProgressCount,
				"closed":      closedCount,
				"archived":    archivedCount,
			},
			"recent_24h": map[string]int64{
				"created": recentlyCreated,
				"closed":  recentlyClosed,
			},
			"high_priority_tasks": highPriorityTasks,
			"compaction": map[string]int64{
				"compacted":   compactedCount,
				"uncompacted": uncompactedCount,
			},
		})
		return nil
	}

	if IsCompactOutput() {
		fmt.Printf("%d open, %d in_progress, %d closed, %d archived | 24h: +%d -%d | compact: %d/%d\n",
			openCount, inProgressCount, closedCount, archivedCount,
			recentlyCreated, recentlyClosed, compactedCount, compactedCount+uncompactedCount)
		if len(highPriorityTasks) > 0 {
			for _, t := range highPriorityTasks {
				fmt.Printf("P%d %s %s %s\n", t.Priority, t.ID, t.Status, t.Title)
			}
		}
		return nil
	}

	fmt.Println("=== Session Summary ===")
	fmt.Printf("Task Status:\n")
	fmt.Printf("  Open:        %d\n", openCount)
	fmt.Printf("  In Progress: %d\n", inProgressCount)
	fmt.Printf("  Closed:      %d\n", closedCount)
	fmt.Printf("  Archived:    %d\n", archivedCount)

	fmt.Printf("\nLast 24 Hours:\n")
	fmt.Printf("  Created: %d\n", recentlyCreated)
	fmt.Printf("  Closed:  %d\n", recentlyClosed)

	if len(highPriorityTasks) > 0 {
		fmt.Printf("\nHigh Priority Tasks:\n")
		for _, t := range highPriorityTasks {
			fmt.Printf("  [%s] P%d %s - %s\n", t.ID, t.Priority, t.Status, t.Title)
		}
	}

	fmt.Printf("\nMemory:\n")
	fmt.Printf("  Compacted:   %d tasks\n", compactedCount)
	fmt.Printf("  Uncompacted: %d tasks (run 'gur compact --all' to free space)\n", uncompactedCount)

	return nil
}
