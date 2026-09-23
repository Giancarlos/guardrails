package cmd

import (
	"testing"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

func TestTemplateVariables(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	database := db.GetDB()

	// Create template with variables
	template := &models.Template{
		Name:      "component-task",
		Title:     "Fix {{component}} issue",
		Variables: models.StringSlice{"component", "severity"},
	}

	if err := database.Create(template).Error; err != nil {
		t.Fatalf("failed to create template: %v", err)
	}

	// Retrieve and verify Variables field persists
	var retrieved models.Template
	if err := database.Where("name = ?", "component-task").First(&retrieved).Error; err != nil {
		t.Fatalf("failed to retrieve template: %v", err)
	}

	if len(retrieved.Variables) != 2 {
		t.Fatalf("variables count = %d, want 2", len(retrieved.Variables))
	}
	if retrieved.Variables[0] != "component" {
		t.Errorf("variables[0] = %q, want %q", retrieved.Variables[0], "component")
	}
	if retrieved.Variables[1] != "severity" {
		t.Errorf("variables[1] = %q, want %q", retrieved.Variables[1], "severity")
	}
}

func TestTemplateVariableSubstitution(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	template := &models.Template{
		Name:        "deploy-task",
		Title:       "Deploy {{component}} to {{env}}",
		Description: "Deploy the {{component}} service to the {{env}} environment",
		Variables:   models.StringSlice{"component", "env"},
	}

	vars := map[string]string{
		"component": "auth-service",
		"env":       "production",
	}

	task := template.ToTask(vars)

	expectedTitle := "Deploy auth-service to production"
	if task.Title != expectedTitle {
		t.Errorf("title = %q, want %q", task.Title, expectedTitle)
	}

	expectedDesc := "Deploy the auth-service service to the production environment"
	if task.Description != expectedDesc {
		t.Errorf("description = %q, want %q", task.Description, expectedDesc)
	}
}

func TestTemplateVariableMissing(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	template := &models.Template{
		Name:        "missing-var",
		Title:       "Fix {{component}} in {{module}}",
		Description: "The {{component}} has issues in {{module}}",
		Variables:   models.StringSlice{"component", "module"},
	}

	// Only provide one of the two variables
	vars := map[string]string{
		"component": "parser",
	}

	task := template.ToTask(vars)

	// component should be substituted, module should remain as placeholder
	expectedTitle := "Fix parser in {{module}}"
	if task.Title != expectedTitle {
		t.Errorf("title = %q, want %q", task.Title, expectedTitle)
	}

	expectedDesc := "The parser has issues in {{module}}"
	if task.Description != expectedDesc {
		t.Errorf("description = %q, want %q", task.Description, expectedDesc)
	}
}
