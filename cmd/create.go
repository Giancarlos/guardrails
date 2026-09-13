package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var (
	createPriority    int
	createType        string
	createLabels      []string
	createAssignee    string
	createDescription string
	createTemplate    string
	createParent      string
	createSkills      []string
	createAgents      []string
	createVars        []string
)

var createCmd = &cobra.Command{
	Use:   "create \"title\"",
	Short: "Create a new task",
	Args:  cobra.RangeArgs(0, 1),
	RunE:  runCreate,
}

func init() {
	rootCmd.AddCommand(createCmd)
	createCmd.Flags().IntVar(&createPriority, "priority", -1, "Priority (0-4)")
	createCmd.Flags().StringVar(&createType, "type", "", "Type (task/bug/feature/epic)")
	createCmd.Flags().StringArrayVar(&createLabels, "label", nil, "Labels")
	createCmd.Flags().StringVar(&createAssignee, "assignee", "", "Assignee")
	createCmd.Flags().StringVar(&createDescription, "description", "", "Description")
	createCmd.Flags().StringVar(&createTemplate, "template", "", "Create from template")
	createCmd.Flags().StringVar(&createParent, "parent", "", "Parent task ID (creates subtask)")
	createCmd.Flags().StringArrayVar(&createSkills, "skill", nil, "Link skill to task")
	createCmd.Flags().StringArrayVar(&createAgents, "agent", nil, "Link agent to task")
	createCmd.Flags().StringArrayVar(&createVars, "var", nil, "Template variable (key=value)")

}

func runCreate(cmd *cobra.Command, args []string) error {
	var task *models.Task

	// Parse --var flags into a map
	varsMap := make(map[string]string)
	for _, v := range createVars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid --var format '%s': must be key=value", v)
		}
		varsMap[parts[0]] = parts[1]
	}

	// If using a template, start with template values
	if createTemplate != "" {
		var template models.Template
		if err := db.GetDB().Where("name = ? OR id = ?", createTemplate, createTemplate).First(&template).Error; err != nil {
			return fmt.Errorf("cannot create task: template '%s' not found (use 'gur template list' to see available templates)", createTemplate)
		}
		task = template.ToTask(varsMap)
	} else {
		task = &models.Task{
			Status:   models.StatusOpen,
			Priority: models.PriorityMedium,
			Type:     models.TypeTask,
		}
	}

	// Title from args (required unless template provides it)
	if len(args) > 0 {
		task.Title = args[0]
	}
	if task.Title == "" {
		return fmt.Errorf("title is required (provide as argument or use template with title)")
	}

	// Override with flags if provided
	if createPriority >= 0 {
		task.Priority = createPriority
	}
	if createType != "" {
		task.Type = createType
	}
	if createDescription != "" {
		task.Description = createDescription
	}
	if createAssignee != "" {
		task.Assignee = createAssignee
	}
	if len(createLabels) > 0 {
		task.Labels = createLabels
	}

	// Validate priority range
	if task.Priority < 0 || task.Priority > 4 {
		return fmt.Errorf("invalid priority %d: must be 0 (critical), 1 (high), 2 (medium), 3 (low), or 4 (lowest)", task.Priority)
	}

	// Validate type
	validTypes := map[string]bool{
		models.TypeTask:    true,
		models.TypeBug:     true,
		models.TypeFeature: true,
		models.TypeEpic:    true,
	}
	if !validTypes[task.Type] {
		return fmt.Errorf("invalid type '%s': must be one of: task, bug, feature, epic", task.Type)
	}

	database := db.GetDB()

	// Handle subtask creation
	if createParent != "" {
		var parent models.Task
		if err := database.First(&parent, "id = ?", createParent).Error; err != nil {
			return fmt.Errorf("cannot create subtask: parent task '%s' not found (use 'gur list' to see available tasks)", createParent)
		}
		if parent.IsClosed() {
			return fmt.Errorf("cannot create subtask: parent task '%s' is closed (reopen it first with 'gur reopen %s')", createParent, createParent)
		}

		// Count existing subtasks to generate next number
		var count int64
		database.Model(&models.Task{}).Where("parent_id = ?", createParent).Count(&count)
		task.ID = models.GenerateSubtaskID(createParent, int(count)+1)
		task.ParentID = createParent
	}

	if err := database.Create(task).Error; err != nil {
		return fmt.Errorf("failed to create task '%s': database error: %w", task.Title, err)
	}

	// Link skills
	for _, skillName := range createSkills {
		var skill models.Skill
		if err := database.Where("name = ?", skillName).First(&skill).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skill not found: %s\n", skillName)
			continue
		}
		link := models.TaskSkillLink{TaskID: task.ID, SkillID: skill.ID}
		if err := database.Create(&link).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to link skill %s: %v\n", skillName, err)
		}
	}

	// Link agents (first one is primary)
	for i, agentName := range createAgents {
		var agent models.Agent
		if err := database.Where("name = ?", agentName).First(&agent).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Warning: agent not found: %s\n", agentName)
			continue
		}
		link := models.TaskAgentLink{TaskID: task.ID, AgentID: agent.ID, IsPrimary: i == 0}
		if err := database.Create(&link).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to link agent %s: %v\n", agentName, err)
		}
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "task": task})
	} else if IsCompactOutput() {
		fmt.Printf("ok: %s\n", task.ID)
	} else {
		fmt.Printf("Created: %s - %s\n", task.ID, task.Title)
	}
	return nil
}
