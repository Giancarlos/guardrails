package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/models"
)

var (
	exportFormat string
	exportStatus string
	exportFile   string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export tasks to JSON, CSV, or Markdown",
	RunE:  runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVar(&exportFormat, "format", "json", "Output format: json, csv, markdown")
	exportCmd.Flags().StringVar(&exportStatus, "status", "", "Filter by status")
	exportCmd.Flags().StringVar(&exportFile, "output", "", "Output file (default stdout)")
}

func runExport(cmd *cobra.Command, args []string) (err error) {
	query := db.GetDB().Order("priority ASC, created_at DESC")

	if exportStatus != "" {
		query = query.Where("status = ?", exportStatus)
	}

	var tasks []models.Task
	if err := query.Find(&tasks).Error; err != nil {
		return fmt.Errorf("failed to query tasks: %w", err)
	}

	var w io.Writer = os.Stdout
	if exportFile != "" {
		f, createErr := os.Create(exportFile)
		if createErr != nil {
			return fmt.Errorf("failed to create output file: %w", createErr)
		}
		// A failed close can mean buffered data never reached disk
		defer func() {
			if closeErr := f.Close(); closeErr != nil && err == nil {
				err = fmt.Errorf("failed to write output file: %w", closeErr)
			}
		}()
		w = f
	}

	switch exportFormat {
	case "json":
		return ExportJSON(w, tasks)
	case "csv":
		return ExportCSV(w, tasks)
	case "markdown", "md":
		return ExportMarkdown(w, tasks)
	default:
		return fmt.Errorf("unsupported format '%s': use json, csv, or markdown", exportFormat)
	}
}

// ExportJSON writes tasks as a JSON array to the given writer.
func ExportJSON(w io.Writer, tasks []models.Task) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}
	_, err = fmt.Fprintln(w)
	return err
}

// ExportCSV writes tasks as CSV rows to the given writer.
func ExportCSV(w io.Writer, tasks []models.Task) error {
	cw := csv.NewWriter(w)

	header := []string{"ID", "Title", "Status", "Priority", "Type", "Assignee", "Labels", "CreatedAt"}
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	for _, t := range tasks {
		labels := strings.Join(t.Labels, ", ")
		row := []string{
			t.ID,
			t.Title,
			t.Status,
			fmt.Sprintf("%d", t.Priority),
			t.Type,
			t.Assignee,
			labels,
			t.CreatedAt.Format(models.DateTimeFormat),
		}
		if err := cw.Write(row); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
	}
	// csv.Writer buffers; write errors only surface after Flush
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("failed to write CSV: %w", err)
	}
	return nil
}

// ExportMarkdown writes tasks in a human-readable Markdown format to the given writer.
func ExportMarkdown(w io.Writer, tasks []models.Task) error {
	for i, t := range tasks {
		fmt.Fprintf(w, "## [%s] %s\n\n", t.ID, t.Title)
		fmt.Fprintf(w, "- **Status:** %s\n", t.Status)
		fmt.Fprintf(w, "- **Priority:** %s\n", t.PriorityString())
		fmt.Fprintf(w, "- **Type:** %s\n", t.Type)
		if t.Assignee != "" {
			fmt.Fprintf(w, "- **Assignee:** %s\n", t.Assignee)
		}
		if len(t.Labels) > 0 {
			fmt.Fprintf(w, "- **Labels:** %s\n", strings.Join(t.Labels, ", "))
		}
		fmt.Fprintf(w, "- **Created:** %s\n", t.CreatedAt.Format("2006-01-02"))

		if t.Description != "" {
			fmt.Fprintf(w, "\n%s\n", t.Description)
		}

		if i < len(tasks)-1 {
			fmt.Fprintln(w, "\n---")
		}
		fmt.Fprintln(w)
	}
	return nil
}
