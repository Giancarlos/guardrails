package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

const filterKeyPrefix = "filter:"

// SavedFilter represents a saved filter configuration
type SavedFilter struct {
	Status   string `json:"status,omitempty"`
	Priority int    `json:"priority"` // -1 means unset
	Type     string `json:"type,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	Label    string `json:"label,omitempty"`
}

var filterCmd = &cobra.Command{
	Use:   "filter",
	Short: "Manage saved filters",
}

// filter save flags
var (
	filterSaveStatus   string
	filterSavePriority int
	filterSaveType     string
	filterSaveAssignee string
	filterSaveLabel    string
)

var filterSaveCmd = &cobra.Command{
	Use:   "save <name>",
	Short: "Save a filter",
	Args:  cobra.ExactArgs(1),
	RunE:  runFilterSave,
}

var filterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved filters",
	RunE:  runFilterList,
}

var filterDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a saved filter",
	Args:  cobra.ExactArgs(1),
	RunE:  runFilterDelete,
}

func init() {
	rootCmd.AddCommand(filterCmd)
	filterCmd.AddCommand(filterSaveCmd)
	filterCmd.AddCommand(filterListCmd)
	filterCmd.AddCommand(filterDeleteCmd)

	filterSaveCmd.Flags().StringVar(&filterSaveStatus, "status", "", "Filter by status")
	filterSaveCmd.Flags().IntVar(&filterSavePriority, "priority", -1, "Filter by priority (-1 means unset)")
	filterSaveCmd.Flags().StringVar(&filterSaveType, "type", "", "Filter by type")
	filterSaveCmd.Flags().StringVar(&filterSaveAssignee, "assignee", "", "Filter by assignee")
	filterSaveCmd.Flags().StringVar(&filterSaveLabel, "label", "", "Filter by label")
}

func runFilterSave(cmd *cobra.Command, args []string) error {
	name := args[0]

	sf := SavedFilter{
		Status:   filterSaveStatus,
		Priority: filterSavePriority,
		Type:     filterSaveType,
		Assignee: filterSaveAssignee,
		Label:    filterSaveLabel,
	}

	data, err := json.Marshal(sf)
	if err != nil {
		return fmt.Errorf("failed to serialize filter: %w", err)
	}

	key := filterKeyPrefix + name
	if err := db.SetConfig(key, string(data)); err != nil {
		return fmt.Errorf("failed to save filter: %w", err)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"saved": true, "name": name, "filter": sf})
		return nil
	}

	fmt.Printf("Filter '%s' saved\n", name)
	return nil
}

func runFilterList(cmd *cobra.Command, args []string) error {
	var configs []models.Config
	if err := db.GetDB().Where("key LIKE ?", filterKeyPrefix+"%").Find(&configs).Error; err != nil {
		return fmt.Errorf("failed to list filters: %w", err)
	}

	type filterEntry struct {
		Name   string      `json:"name"`
		Filter SavedFilter `json:"filter"`
	}

	entries := []filterEntry{}
	for _, c := range configs {
		name := strings.TrimPrefix(c.Key, filterKeyPrefix)
		var sf SavedFilter
		if err := json.Unmarshal([]byte(c.Value), &sf); err != nil {
			continue
		}
		entries = append(entries, filterEntry{Name: name, Filter: sf})
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"count": len(entries), "filters": entries})
		return nil
	}

	if len(entries) == 0 {
		fmt.Println("No saved filters")
		return nil
	}

	for _, e := range entries {
		parts := []string{}
		if e.Filter.Status != "" {
			parts = append(parts, "status="+e.Filter.Status)
		}
		if e.Filter.Priority >= 0 {
			parts = append(parts, fmt.Sprintf("priority=%d", e.Filter.Priority))
		}
		if e.Filter.Type != "" {
			parts = append(parts, "type="+e.Filter.Type)
		}
		if e.Filter.Assignee != "" {
			parts = append(parts, "assignee="+e.Filter.Assignee)
		}
		if e.Filter.Label != "" {
			parts = append(parts, "label="+e.Filter.Label)
		}
		desc := strings.Join(parts, ", ")
		if desc == "" {
			desc = "(no filters)"
		}
		fmt.Printf("  %s: %s\n", e.Name, desc)
	}
	return nil
}

func runFilterDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	key := filterKeyPrefix + name

	result := db.GetDB().Where("key = ?", key).Delete(&models.Config{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete filter: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("filter '%s' not found", name)
	}

	if IsJSONOutput() {
		OutputJSON(map[string]interface{}{"deleted": true, "name": name})
		return nil
	}

	fmt.Printf("Filter '%s' deleted\n", name)
	return nil
}

// LoadSavedFilter loads a saved filter by name from the config store
func LoadSavedFilter(name string) (*SavedFilter, error) {
	key := filterKeyPrefix + name
	value, err := db.GetConfig(key)
	if err != nil {
		return nil, fmt.Errorf("filter '%s' not found", name)
	}
	var sf SavedFilter
	if err := json.Unmarshal([]byte(value), &sf); err != nil {
		return nil, fmt.Errorf("failed to parse filter '%s': %w", name, err)
	}
	return &sf, nil
}
