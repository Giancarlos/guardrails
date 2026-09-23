package ioformat

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Giancarlos/guardrails/internal/models"
)

// This file holds the human- and spreadsheet-facing export shapes. Unlike the
// JSONL encoders they are one-way: CSV and Markdown are lossy by design, and
// only the JSON array round-trips through 'gur import --format gur-json'.

// EncodeJSON writes tasks as a single indented JSON array.
func EncodeJSON(w io.Writer, tasks []models.Task) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	_, err = fmt.Fprintln(w)
	return err
}

// EncodeCSV writes one header row plus one row per task.
func EncodeCSV(w io.Writer, tasks []models.Task) error {
	cw := csv.NewWriter(w)

	header := []string{"ID", "Title", "Status", "Priority", "Type", "Assignee", "Labels", "TokensUsed", "TokensBudget", "CreatedAt"}
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}

	for _, t := range tasks {
		row := []string{
			t.ID,
			t.Title,
			t.Status,
			fmt.Sprintf("%d", t.Priority),
			t.Type,
			t.Assignee,
			strings.Join(t.Labels, ", "),
			fmt.Sprintf("%d", t.TokensUsed),
			fmt.Sprintf("%d", t.TokensBudget),
			t.CreatedAt.Format(models.DateTimeFormat),
		}
		if err := cw.Write(row); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}
	// csv.Writer buffers; write errors only surface after Flush
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}
	return nil
}

// EncodeMarkdown writes tasks as a readable Markdown document.
func EncodeMarkdown(w io.Writer, tasks []models.Task) error {
	for i, t := range tasks {
		if _, err := fmt.Fprintf(w, "## [%s] %s\n\n", t.ID, t.Title); err != nil {
			return fmt.Errorf("write Markdown: %w", err)
		}
		fmt.Fprintf(w, "- **Status:** %s\n", t.Status)
		fmt.Fprintf(w, "- **Priority:** %s\n", t.PriorityString())
		fmt.Fprintf(w, "- **Type:** %s\n", t.Type)
		if t.Assignee != "" {
			fmt.Fprintf(w, "- **Assignee:** %s\n", t.Assignee)
		}
		if len(t.Labels) > 0 {
			fmt.Fprintf(w, "- **Labels:** %s\n", strings.Join(t.Labels, ", "))
		}
		if t.TokensUsed > 0 || t.TokensBudget > 0 {
			fmt.Fprintf(w, "- **Tokens:** %s\n", t.TokenUsageString())
		}
		fmt.Fprintf(w, "- **Created:** %s\n", t.CreatedAt.Format("2006-01-02"))

		if t.Description != "" {
			fmt.Fprintf(w, "\n%s\n", t.Description)
		}
		if i < len(tasks)-1 {
			fmt.Fprintln(w, "\n---")
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return fmt.Errorf("write Markdown: %w", err)
		}
	}
	return nil
}
