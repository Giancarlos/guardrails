package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var (
	tmplPriority    int
	tmplType        string
	tmplDescription string
	tmplLabels      []string
	tmplVars        []string
)

var templateCmd = &cobra.Command{
	Use:     "template",
	Aliases: []string{"tmpl"},
	Short:   "Manage task templates",
}

var templateCreateCmd = &cobra.Command{
	Use:   "create <name> [title]",
	Short: "Create a new template",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runTemplateCreate,
}

var templateListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all templates",
	RunE:    runTemplateList,
}

var templateShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show template details",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateShow,
}

var templateDeleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete a template",
	Args:    cobra.ExactArgs(1),
	RunE:    runTemplateDelete,
}

func init() {
	rootCmd.AddCommand(templateCmd)
	templateCmd.AddCommand(templateCreateCmd)
	templateCmd.AddCommand(templateListCmd)
	templateCmd.AddCommand(templateShowCmd)
	templateCmd.AddCommand(templateDeleteCmd)

	templateCreateCmd.Flags().IntVar(&tmplPriority, "priority", models.PriorityMedium, "Default priority (0-4)")
	templateCreateCmd.Flags().StringVar(&tmplType, "type", models.TypeTask, "Default type (task, bug, feature, epic)")
	templateCreateCmd.Flags().StringVar(&tmplDescription, "description", "", "Default description")
	templateCreateCmd.Flags().StringSliceVar(&tmplLabels, "label", nil, "Default labels")
	templateCreateCmd.Flags().StringArrayVar(&tmplVars, "var", nil, "Declare template variable names")
}

func runTemplateCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	title := ""
	if len(args) > 1 {
		title = args[1]
	}

	// Check if template already exists
	var existing models.Template
	if err := db.GetDB().Where("name = ?", name).First(&existing).Error; err == nil {
		return fmt.Errorf("cannot create template: template '%s' already exists (use 'gur template show %s' to view it)", name, name)
	}

	template := &models.Template{
		Name:        name,
		Title:       title,
		Description: tmplDescription,
		Priority:    tmplPriority,
		Type:        tmplType,
		Labels:      tmplLabels,
		Variables:   tmplVars,
	}

	if err := db.GetDB().Create(template).Error; err != nil {
		return fmt.Errorf("failed to create template '%s': database error: %w", name, err)
	}

	if IsJSONOutput() {
		OutputJSON(template)
		return nil
	}
	fmt.Printf("Created template: %s\n", name)
	return nil
}

func runTemplateList(cmd *cobra.Command, args []string) error {
	var templates []models.Template
	if err := db.GetDB().Order("name ASC").Find(&templates).Error; err != nil {
		return err
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"count": len(templates), "templates": templates})
		return nil
	}

	if len(templates) == 0 {
		fmt.Println("No templates found")
		return nil
	}

	for _, t := range templates {
		titlePart := ""
		if t.Title != "" {
			titlePart = fmt.Sprintf(" - \"%s\"", t.Title)
		}
		fmt.Printf("[%s] %s%s (P%d %s)\n", t.ID, t.Name, titlePart, t.Priority, t.Type)
	}
	return nil
}

func runTemplateShow(cmd *cobra.Command, args []string) error {
	name := args[0]
	var template models.Template
	if err := db.GetDB().Where("name = ? OR id = ?", name, name).First(&template).Error; err != nil {
		return fmt.Errorf("template '%s' not found (use 'gur template list' to see available templates)", name)
	}

	if IsJSONOutput() {
		OutputJSON(template)
		return nil
	}

	fmt.Printf("ID:          %s\n", template.ID)
	fmt.Printf("Name:        %s\n", template.Name)
	if template.Title != "" {
		fmt.Printf("Title:       %s\n", template.Title)
	}
	fmt.Printf("Type:        %s\n", template.Type)
	fmt.Printf("Priority:    P%d\n", template.Priority)
	if template.Description != "" {
		fmt.Printf("Description: %s\n", template.Description)
	}
	if len(template.Labels) > 0 {
		fmt.Printf("Labels:      %v\n", template.Labels)
	}
	if len(template.Variables) > 0 {
		fmt.Printf("Variables:   %v\n", template.Variables)
	}
	return nil
}

func runTemplateDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	result := db.GetDB().Where("name = ? OR id = ?", name, name).Delete(&models.Template{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete template '%s': database error: %w", name, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("cannot delete template: template '%s' not found (use 'gur template list' to see available templates)", name)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"deleted": name})
		return nil
	}
	fmt.Printf("Deleted template: %s\n", name)
	return nil
}
