package cmd

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var (
	updateTitle        string
	updateDescription  string
	updatePriority     int
	updateType         string
	updateStatus       string
	updateAssignee     string
	updateNotes        string
	updateAddLabel     []string
	updateRemoveLabel  []string
	updateAddSkill     []string
	updateRemoveSkill  []string
	updateAddAgent     []string
	updateRemoveAgent  []string
	updateTokensUsed   int64
	updateTokensAdd    int64
	updateTokensBudget int64
)

var updateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().StringVar(&updateTitle, "title", "", "New title")
	updateCmd.Flags().StringVar(&updateDescription, "description", "", "New description")
	updateCmd.Flags().IntVar(&updatePriority, "priority", -1, "New priority")
	updateCmd.Flags().StringVar(&updateType, "type", "", "New type")
	updateCmd.Flags().StringVar(&updateStatus, "status", "", "New status")
	updateCmd.Flags().StringVar(&updateAssignee, "assignee", "", "New assignee")
	updateCmd.Flags().StringVar(&updateNotes, "notes", "", "Append notes")
	updateCmd.Flags().StringArrayVar(&updateAddLabel, "label", nil, "Add label")
	updateCmd.Flags().StringArrayVar(&updateRemoveLabel, "remove-label", nil, "Remove label")
	updateCmd.Flags().StringArrayVar(&updateAddSkill, "skill", nil, "Link skill to task")
	updateCmd.Flags().StringArrayVar(&updateRemoveSkill, "remove-skill", nil, "Unlink skill from task")
	updateCmd.Flags().StringArrayVar(&updateAddAgent, "agent", nil, "Link agent to task")
	updateCmd.Flags().StringArrayVar(&updateRemoveAgent, "remove-agent", nil, "Unlink agent from task")
	updateCmd.Flags().Int64Var(&updateTokensUsed, "tokens-used", -1, "Set token usage count")
	updateCmd.Flags().Int64Var(&updateTokensAdd, "tokens-add", 0, "Add to token usage count")
	updateCmd.Flags().Int64Var(&updateTokensBudget, "tokens-budget", -1, "Set token budget")
	updateCmd.MarkFlagsMutuallyExclusive("tokens-used", "tokens-add")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	task, err := resolveTaskID(args[0])
	if err != nil {
		return err
	}

	// Prevent modifying closed tasks (except reopening via 'reopen' command)
	if task.IsClosed() && cmd.Flags().Changed("status") && updateStatus != models.StatusClosed {
		return fmt.Errorf("cannot change status of closed task '%s': use 'gur reopen %s' first", task.ID, task.ID)
	}

	// Validate token flags before any changes are recorded
	if cmd.Flags().Changed("tokens-used") && updateTokensUsed < 0 {
		return fmt.Errorf("invalid --tokens-used %d: must be zero or greater", updateTokensUsed)
	}
	if cmd.Flags().Changed("tokens-budget") && updateTokensBudget < 0 {
		return fmt.Errorf("invalid --tokens-budget %d: must be zero or greater", updateTokensBudget)
	}
	if cmd.Flags().Changed("tokens-add") {
		if updateTokensAdd > 0 && task.TokensUsed > math.MaxInt64-updateTokensAdd {
			return fmt.Errorf("invalid --tokens-add %d: token usage would overflow (currently %d)", updateTokensAdd, task.TokensUsed)
		}
		if task.TokensUsed+updateTokensAdd < 0 {
			return fmt.Errorf("invalid --tokens-add %d: token usage cannot go below zero (currently %d)", updateTokensAdd, task.TokensUsed)
		}
	}

	// Only flags defined on 'update' itself count as changes (not --json/--compact)
	hasChanges := false
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			hasChanges = true
		}
	})
	tokensChanged := cmd.Flags().Changed("tokens-used") || cmd.Flags().Changed("tokens-add") || cmd.Flags().Changed("tokens-budget")

	// Track changes for audit trail
	database := db.GetDB()
	changedBy := "user" // Could be enhanced to track actual user

	// Check if scope-changing fields are being modified and gates have passed
	scopeChanging := cmd.Flags().Changed("title") || cmd.Flags().Changed("description") || cmd.Flags().Changed("type")
	if scopeChanging {
		// Check for passed gates on this task
		var passedLinks []models.GateTaskLink
		database.Where("task_id = ? AND status = ?", task.ID, models.GateLinkPassed).Find(&passedLinks)

		if len(passedLinks) > 0 {
			fmt.Fprintf(os.Stderr, "WARNING: This task has %d gate(s) that have already passed.\n", len(passedLinks))
			fmt.Fprintf(os.Stderr, "Changing title, description, or type may affect the scope of verified work.\n\n")

			// Show which gates passed
			for _, link := range passedLinks {
				gate, _ := db.GetGateByID(link.GateID)
				if gate != nil {
					fmt.Fprintf(os.Stderr, "  - %s: %s (passed)\n", gate.ID, gate.Title)
				}
			}
			fmt.Fprintln(os.Stderr)

			// Require interactive confirmation
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("scope-changing update requires interactive confirmation when gates have passed.\nRe-run in an interactive terminal to confirm.")
			}

			fmt.Print("Do you want to proceed with this update? (yes/no): ")
			reader := bufio.NewReader(os.Stdin)
			confirmation, _ := reader.ReadString('\n')
			confirmation = strings.TrimSpace(strings.ToLower(confirmation))

			if confirmation != "yes" {
				return fmt.Errorf("update cancelled")
			}
			fmt.Println()
		}
	}

	if cmd.Flags().Changed("title") {
		models.RecordChange(database, task.ID, "title", task.Title, updateTitle, changedBy)
		task.Title = updateTitle
	}
	if cmd.Flags().Changed("description") {
		models.RecordChange(database, task.ID, "description", task.Description, updateDescription, changedBy)
		task.Description = updateDescription
	}
	if cmd.Flags().Changed("priority") {
		// Validate priority range
		if updatePriority < 0 || updatePriority > 4 {
			return fmt.Errorf("invalid priority %d for task '%s': must be 0 (critical) to 4 (lowest)", updatePriority, task.ID)
		}
		models.RecordChange(database, task.ID, "priority", fmt.Sprintf("%d", task.Priority), fmt.Sprintf("%d", updatePriority), changedBy)
		task.Priority = updatePriority
	}
	if cmd.Flags().Changed("type") {
		models.RecordChange(database, task.ID, "type", task.Type, updateType, changedBy)
		task.Type = updateType
	}
	if cmd.Flags().Changed("status") {
		// Validate status values
		validStatuses := map[string]bool{
			models.StatusOpen:       true,
			models.StatusInProgress: true,
			models.StatusClosed:     true,
		}
		if !validStatuses[updateStatus] {
			return fmt.Errorf("invalid status '%s' for task '%s': must be one of: open, in_progress, closed", updateStatus, task.ID)
		}
		models.RecordChange(database, task.ID, "status", task.Status, updateStatus, changedBy)
		task.Status = updateStatus
	}
	if cmd.Flags().Changed("assignee") {
		models.RecordChange(database, task.ID, "assignee", task.Assignee, updateAssignee, changedBy)
		task.Assignee = updateAssignee
	}
	if cmd.Flags().Changed("notes") {
		models.RecordChange(database, task.ID, "notes", "", updateNotes, changedBy)
		task.AppendNotes(updateNotes)
	}
	for _, l := range updateAddLabel {
		models.RecordChange(database, task.ID, "label_added", "", l, changedBy)
		task.AddLabel(l)
	}
	for _, l := range updateRemoveLabel {
		models.RecordChange(database, task.ID, "label_removed", l, "", changedBy)
		task.RemoveLabel(l)
	}

	// Link skills
	for _, skillName := range updateAddSkill {
		var skill models.Skill
		if err := database.Where("name = ?", skillName).First(&skill).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skill not found: %s\n", skillName)
			continue
		}
		// Check if already linked
		var existing models.TaskSkillLink
		if database.Where("task_id = ? AND skill_id = ?", task.ID, skill.ID).First(&existing).Error == nil {
			continue // Already linked
		}
		link := models.TaskSkillLink{TaskID: task.ID, SkillID: skill.ID}
		if err := database.Create(&link).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to link skill %s: %v\n", skillName, err)
			continue
		}
		models.RecordChange(database, task.ID, "skill_added", "", skillName, changedBy)
	}

	// Unlink skills
	for _, skillName := range updateRemoveSkill {
		var skill models.Skill
		if err := database.Where("name = ?", skillName).First(&skill).Error; err != nil {
			continue
		}
		if err := database.Where("task_id = ? AND skill_id = ?", task.ID, skill.ID).Delete(&models.TaskSkillLink{}).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to unlink skill %s: %v\n", skillName, err)
			continue
		}
		models.RecordChange(database, task.ID, "skill_removed", skillName, "", changedBy)
	}

	// Link agents
	for _, agentName := range updateAddAgent {
		var agent models.Agent
		if err := database.Where("name = ?", agentName).First(&agent).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Warning: agent not found: %s\n", agentName)
			continue
		}
		// Check if already linked
		var existing models.TaskAgentLink
		if database.Where("task_id = ? AND agent_id = ?", task.ID, agent.ID).First(&existing).Error == nil {
			continue // Already linked
		}
		link := models.TaskAgentLink{TaskID: task.ID, AgentID: agent.ID}
		if err := database.Create(&link).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to link agent %s: %v\n", agentName, err)
			continue
		}
		models.RecordChange(database, task.ID, "agent_added", "", agentName, changedBy)
	}

	// Unlink agents
	for _, agentName := range updateRemoveAgent {
		var agent models.Agent
		if err := database.Where("name = ?", agentName).First(&agent).Error; err != nil {
			continue
		}
		if err := database.Where("task_id = ? AND agent_id = ?", task.ID, agent.ID).Delete(&models.TaskAgentLink{}).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to unlink agent %s: %v\n", agentName, err)
			continue
		}
		models.RecordChange(database, task.ID, "agent_removed", agentName, "", changedBy)
	}

	if cmd.Flags().Changed("tokens-used") {
		models.RecordChange(database, task.ID, "tokens_used", fmt.Sprintf("%d", task.TokensUsed), fmt.Sprintf("%d", updateTokensUsed), changedBy)
		task.TokensUsed = updateTokensUsed
	}
	if cmd.Flags().Changed("tokens-add") {
		old := task.TokensUsed
		task.TokensUsed += updateTokensAdd
		models.RecordChange(database, task.ID, "tokens_used", fmt.Sprintf("%d", old), fmt.Sprintf("%d", task.TokensUsed), changedBy)
	}
	if cmd.Flags().Changed("tokens-budget") {
		models.RecordChange(database, task.ID, "tokens_budget", fmt.Sprintf("%d", task.TokensBudget), fmt.Sprintf("%d", updateTokensBudget), changedBy)
		task.TokensBudget = updateTokensBudget
	}

	// Warn if a token change leaves usage over budget
	if tokensChanged && task.TokensBudget > 0 && task.TokensUsed > task.TokensBudget {
		fmt.Fprintf(os.Stderr, "WARNING: Token usage (%d) exceeds budget (%d) for task %s\n", task.TokensUsed, task.TokensBudget, task.ID)
	}

	if err := database.Save(&task).Error; err != nil {
		return fmt.Errorf("failed to update task '%s': database error: %w", task.ID, err)
	}

	if hasChanges {
		models.RunHooks(database, models.HookEventOnUpdate, task)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "task": task})
	} else if IsCompactOutput() {
		fmt.Printf("ok: %s\n", task.ID)
	} else {
		fmt.Printf("Updated: %s\n", task.ID)
	}
	return nil
}
