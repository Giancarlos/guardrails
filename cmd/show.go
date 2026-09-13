package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show task details",
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func init() {
	rootCmd.AddCommand(showCmd)
}

func runShow(cmd *cobra.Command, args []string) error {
	database := db.GetDB()
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}

	// Use eager loading to fetch dependencies in fewer queries
	var blockedBy, blocks []models.Dependency
	database.Where("child_id = ?", task.ID).Find(&blockedBy)
	database.Where("parent_id = ?", task.ID).Find(&blocks)

	// Fetch subtasks
	var subtasks []models.Task
	database.Where("parent_id = ?", task.ID).Order("id ASC").Find(&subtasks)

	// Fetch linked skills
	var skillLinks []models.TaskSkillLink
	database.Preload("Skill").Where("task_id = ?", task.ID).Find(&skillLinks)

	// Fetch linked agents
	var agentLinks []models.TaskAgentLink
	database.Preload("Agent").Where("task_id = ?", task.ID).Find(&agentLinks)

	// Fetch latest checkpoint
	var latestCheckpoint *models.Checkpoint
	var chk models.Checkpoint
	if err := database.Where("task_id = ?", task.ID).Order("created_at DESC").First(&chk).Error; err == nil {
		latestCheckpoint = &chk
	}

	// Fetch pending handoffs
	var pendingHandoffs []models.Handoff
	database.Where("task_id = ? AND status = ?", task.ID, models.HandoffPending).Find(&pendingHandoffs)

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{
			"task":              task,
			"blocked_by":        blockedBy,
			"blocks":            blocks,
			"subtasks":          subtasks,
			"skills":            skillLinks,
			"agents":            agentLinks,
			"latest_checkpoint": latestCheckpoint,
			"pending_handoffs":  pendingHandoffs,
		})
		return nil
	}

	if IsCompactOutput() {
		// Compact: single-line core info + minimal extras
		typeStr := ""
		if task.Type != models.TypeTask {
			typeStr = " (" + task.Type + ")"
		}
		parentStr := ""
		if task.ParentID != "" {
			parentStr = " <" + task.ParentID
		}
		fmt.Printf("%s | P%d %s | %s%s%s\n", task.ID, task.Priority, task.Status, task.Title, typeStr, parentStr)
		if task.Description != "" {
			fmt.Printf("desc:%s\n", task.Description)
		}
		if task.Assignee != "" {
			fmt.Printf("assignee:%s\n", task.Assignee)
		}
		if len(task.Labels) > 0 {
			fmt.Printf("labels:%s\n", strings.Join(task.Labels, ","))
		}
		if len(subtasks) > 0 {
			fmt.Printf("subtasks(%d):", len(subtasks))
			for _, s := range subtasks {
				fmt.Printf(" %s:%s", s.ID, s.Status)
			}
			fmt.Println()
		}
		if len(blockedBy) > 0 {
			ids := make([]string, len(blockedBy))
			for i, d := range blockedBy {
				ids[i] = d.ParentID
			}
			fmt.Printf("blocked_by:%s\n", strings.Join(ids, ","))
		}
		if len(blocks) > 0 {
			ids := make([]string, len(blocks))
			for i, d := range blocks {
				ids[i] = d.ChildID
			}
			fmt.Printf("blocks:%s\n", strings.Join(ids, ","))
		}
		if task.Notes != "" {
			fmt.Printf("notes:%s\n", task.Notes)
		}
		if task.TokensUsed > 0 || task.TokensBudget > 0 {
			fmt.Printf("tokens:%s\n", task.TokenUsageString())
		}
		if task.ContextSummary != "" {
			fmt.Printf("context:%s\n", task.ContextSummary)
		}
		if latestCheckpoint != nil {
			fmt.Printf("checkpoint:%s %s\n", latestCheckpoint.ID, latestCheckpoint.StateText)
		}
		if len(pendingHandoffs) > 0 {
			parts := make([]string, len(pendingHandoffs))
			for i, h := range pendingHandoffs {
				parts[i] = h.ID + ":" + h.FromAgent + "->" + h.ToAgent
			}
			fmt.Printf("handoffs:%s\n", strings.Join(parts, " "))
		}
		return nil
	}

	fmt.Printf("ID:       %s\n", task.ID)
	if task.ParentID != "" {
		fmt.Printf("Parent:   %s\n", task.ParentID)
	}
	fmt.Printf("Title:    %s\n", task.Title)
	fmt.Printf("Status:   %s\n", task.Status)
	fmt.Printf("Priority: %s\n", task.PriorityString())
	fmt.Printf("Type:     %s\n", task.Type)
	if task.Description != "" {
		fmt.Printf("Desc:     %s\n", task.Description)
	}
	if task.Assignee != "" {
		fmt.Printf("Assignee: %s\n", task.Assignee)
	}
	if len(task.Labels) > 0 {
		fmt.Printf("Labels:   %v\n", task.Labels)
	}
	if task.Summary != "" {
		fmt.Printf("Summary:  %s\n", task.Summary)
	}
	fmt.Printf("Created:  %s\n", task.CreatedAt.Format(models.DateTimeShortFormat))
	if task.TokensUsed > 0 || task.TokensBudget > 0 {
		fmt.Printf("Tokens:   %s\n", task.TokenUsageString())
	}
	if task.ContextSummary != "" {
		fmt.Printf("Context:  %s\n", task.ContextSummary)
	}
	if len(subtasks) > 0 {
		fmt.Println("\nSubtasks:")
		for _, s := range subtasks {
			fmt.Printf("  [%s] %s - %s\n", s.ID, s.Status, s.Title)
		}
	}
	if len(blockedBy) > 0 {
		fmt.Println("\nBlocked by:")
		for _, d := range blockedBy {
			fmt.Printf("  - %s\n", d.ParentID)
		}
	}
	if len(blocks) > 0 {
		fmt.Println("\nBlocks:")
		for _, d := range blocks {
			fmt.Printf("  - %s\n", d.ChildID)
		}
	}
	if task.Notes != "" {
		fmt.Printf("\nNotes:\n%s", task.Notes)
	}

	// Show recommended skills and agents
	if len(skillLinks) > 0 || len(agentLinks) > 0 {
		fmt.Println()
		fmt.Println("Recommended:")
		if len(skillLinks) > 0 {
			var skillNames []string
			for _, sl := range skillLinks {
				skillNames = append(skillNames, "/"+sl.Skill.Name)
			}
			fmt.Printf("  Skills: %s\n", strings.Join(skillNames, ", "))
		}
		if len(agentLinks) > 0 {
			var agentNames []string
			for _, al := range agentLinks {
				name := al.Agent.Name
				if al.IsPrimary {
					name += " (primary)"
				}
				agentNames = append(agentNames, name)
			}
			fmt.Printf("  Agents: %s\n", strings.Join(agentNames, ", "))
		}
	}

	// Show latest checkpoint
	if latestCheckpoint != nil {
		fmt.Println()
		fmt.Println("Latest Checkpoint:")
		fmt.Printf("  ID:      %s\n", latestCheckpoint.ID)
		if latestCheckpoint.AgentID != "" {
			fmt.Printf("  Agent:   %s\n", latestCheckpoint.AgentID)
		}
		fmt.Printf("  Created: %s\n", latestCheckpoint.CreatedAt.Format(models.DateTimeShortFormat))
		if latestCheckpoint.StateText != "" {
			fmt.Printf("  State:   %s\n", latestCheckpoint.StateText)
		}
	}

	// Show pending handoffs
	if len(pendingHandoffs) > 0 {
		fmt.Println()
		fmt.Println("Pending Handoffs:")
		for _, h := range pendingHandoffs {
			summary := h.Summary
			if summary == "" {
				summary = "(context data only)"
			}
			fmt.Printf("  [%s] %s -> %s: %s\n", h.ID, h.FromAgent, h.ToAgent, summary)
		}
	}

	return nil
}
