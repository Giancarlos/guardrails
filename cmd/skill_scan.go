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

var skillScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Auto-discover skills from known locations",
	RunE:  runSkillScan,
}

func init() {
	skillCmd.AddCommand(skillScanCmd)
}

func runSkillScan(cmd *cobra.Command, args []string) error {
	homeDir, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	discovered := 0
	skipped := 0

	// Scan Claude skills
	claudeSkillDirs := []string{
		filepath.Join(homeDir, ".claude", "skills"),
		filepath.Join(cwd, ".claude", "skills"),
	}

	for _, dir := range claudeSkillDirs {
		skills, err := scanSkillDirectory(dir, models.SourceClaude)
		if err != nil {
			continue
		}
		for _, s := range skills {
			added, err := registerSkillIfNew(s)
			if err != nil {
				if !IsJSONOutput() {
					fmt.Printf("  Error: %s - %v\n", s.Name, err)
				}
			} else if added {
				discovered++
				if !IsJSONOutput() {
					fmt.Printf("  Found: %s (%s)\n", s.Name, s.Source)
				}
			} else {
				skipped++
			}
		}
	}

	// Scan Cursor rules
	cursorRuleDirs := []string{
		filepath.Join(homeDir, ".cursor", "rules"),
		filepath.Join(cwd, ".cursor", "rules"),
	}

	for _, dir := range cursorRuleDirs {
		skills, err := scanCursorRules(dir)
		if err != nil {
			continue
		}
		for _, s := range skills {
			added, err := registerSkillIfNew(s)
			if err != nil {
				if !IsJSONOutput() {
					fmt.Printf("  Error: %s - %v\n", s.Name, err)
				}
			} else if added {
				discovered++
				if !IsJSONOutput() {
					fmt.Printf("  Found: %s (%s)\n", s.Name, s.Source)
				}
			} else {
				skipped++
			}
		}
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"success": true, "discovered": discovered, "skipped": skipped})
	} else {
		fmt.Printf("\nDiscovered %d new skill(s), %d already registered\n", discovered, skipped)
	}
	return nil
}

func scanSkillDirectory(dir string, source string) ([]models.Skill, error) {
	var skills []models.Skill

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillPath); os.IsNotExist(err) {
			continue
		}

		skill := models.Skill{
			Name:        entry.Name(),
			Path:        skillPath,
			Source:      source,
			Description: extractSkillDescription(skillPath),
		}
		skills = append(skills, skill)
	}

	return skills, nil
}

func scanCursorRules(dir string) ([]models.Skill, error) {
	var skills []models.Skill

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".mdc") && !strings.HasSuffix(name, ".md") {
			continue
		}

		skillPath := filepath.Join(dir, name)
		skillName := strings.TrimSuffix(strings.TrimSuffix(name, ".mdc"), ".md")

		skill := models.Skill{
			Name:        skillName,
			Path:        skillPath,
			Source:      models.SourceCursor,
			Description: extractSkillDescription(skillPath),
		}
		skills = append(skills, skill)
	}

	return skills, nil
}

func extractSkillDescription(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inFrontmatter := false
	foundDescription := ""

	for scanner.Scan() {
		line := scanner.Text()

		if line == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			} else {
				break // End of frontmatter
			}
		}

		if inFrontmatter && strings.HasPrefix(line, "description:") {
			foundDescription = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			foundDescription = strings.Trim(foundDescription, "\"'")
			break
		}
	}

	return foundDescription
}

func registerSkillIfNew(skill models.Skill) (bool, error) {
	var existing models.Skill
	if err := db.GetDB().Where("name = ?", skill.Name).First(&existing).Error; err == nil {
		return false, nil // Already exists
	}

	if err := db.GetDB().Create(&skill).Error; err != nil {
		return false, err
	}
	return true, nil
}
