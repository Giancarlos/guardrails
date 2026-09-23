package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var agentScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Auto-discover agents from known locations",
	RunE:  runAgentScan,
}

func init() {
	agentCmd.AddCommand(agentScanCmd)
}

func runAgentScan(cmd *cobra.Command, args []string) error {
	homeDir, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	discovered := 0
	skipped := 0

	// Scan Claude agents directory
	claudeAgentDirs := []string{
		filepath.Join(homeDir, ".claude", "agents"),
		filepath.Join(cwd, ".claude", "agents"),
	}

	for _, dir := range claudeAgentDirs {
		agents, err := scanAgentDirectory(dir, models.SourceClaude)
		if err != nil {
			continue
		}
		for _, a := range agents {
			added, err := registerAgentIfNew(a)
			if err != nil {
				if !IsJSONOutput() {
					fmt.Printf("  Error: %s - %v\n", a.Name, err)
				}
			} else if added {
				discovered++
				if !IsJSONOutput() {
					fmt.Printf("  Found: %s (%s)\n", a.Name, a.Source)
				}
			} else {
				skipped++
			}
		}
	}

	// Scan for standard agent files in project root
	standardAgentFiles := []struct {
		name   string
		source string
	}{
		{"AGENTS.md", models.SourceCustom},
		{"CLAUDE.md", models.SourceClaude},
		{"GEMINI.md", models.SourceCustom},
		{".cursorrules", models.SourceCursor},
		{".windsurfrules", models.SourceWindsurf},
	}

	for _, af := range standardAgentFiles {
		agentPath := filepath.Join(cwd, af.name)
		if _, err := os.Stat(agentPath); os.IsNotExist(err) {
			continue
		}

		agent := models.Agent{
			Name:        strings.TrimSuffix(strings.TrimPrefix(af.name, "."), ".md"),
			Path:        agentPath,
			Source:      af.source,
			Description: extractAgentDescription(agentPath),
		}

		added, err := registerAgentIfNew(agent)
		if err != nil {
			if !IsJSONOutput() {
				fmt.Printf("  Error: %s - %v\n", agent.Name, err)
			}
		} else if added {
			discovered++
			if !IsJSONOutput() {
				fmt.Printf("  Found: %s (%s)\n", agent.Name, agent.Source)
			}
		} else {
			skipped++
		}
	}

	// Register built-in Claude Code agents
	builtInAgents := []models.Agent{
		{Name: "Explore", Source: models.SourceClaude, Description: "Fast agent for exploring codebases", Capabilities: "Glob, Grep, Read, WebFetch, WebSearch"},
		{Name: "Plan", Source: models.SourceClaude, Description: "Software architect for designing implementation plans", Capabilities: "All read tools, no edit/write"},
		{Name: "Bash", Source: models.SourceClaude, Description: "Command execution specialist", Capabilities: "Bash commands, git operations"},
	}

	for _, a := range builtInAgents {
		added, err := registerAgentIfNew(a)
		if err != nil {
			if !IsJSONOutput() {
				fmt.Printf("  Error: %s - %v\n", a.Name, err)
			}
		} else if added {
			discovered++
			if !IsJSONOutput() {
				fmt.Printf("  Found: %s (built-in)\n", a.Name)
			}
		} else {
			skipped++
		}
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "discovered": discovered, "skipped": skipped})
	} else {
		fmt.Printf("\nDiscovered %d new agent(s), %d already registered\n", discovered, skipped)
	}
	return nil
}

func scanAgentDirectory(dir string, source string) ([]models.Agent, error) {
	var agents []models.Agent

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		agentPath := filepath.Join(dir, name)
		agentName := strings.TrimSuffix(name, ".md")

		agent := models.Agent{
			Name:        agentName,
			Path:        agentPath,
			Source:      source,
			Description: extractAgentDescription(agentPath),
		}
		agents = append(agents, agent)
	}

	return agents, nil
}

func extractAgentDescription(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Try to find a description in frontmatter or first paragraph
	inFrontmatter := false
	lineCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		lineCount++

		if line == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			} else {
				inFrontmatter = false
				continue
			}
		}

		if inFrontmatter && strings.HasPrefix(line, "description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			return strings.Trim(desc, "\"'")
		}

		// If no frontmatter, use first non-empty, non-heading line
		if !inFrontmatter && lineCount <= 10 {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
				if len(line) > 100 {
					line = line[:97] + "..."
				}
				return line
			}
		}
	}

	return ""
}

func registerAgentIfNew(agent models.Agent) (bool, error) {
	var existing models.Agent
	if err := db.GetDB().Where("name = ?", agent.Name).First(&existing).Error; err == nil {
		return false, nil // Already exists
	}

	if err := db.GetDB().Create(&agent).Error; err != nil {
		return false, err
	}
	return true, nil
}
