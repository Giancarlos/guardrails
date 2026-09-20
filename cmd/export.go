package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/ioformat"
	"github.com/Giancarlos/guardrails/internal/models"
)

var (
	exportFormat        string
	exportOutput        string
	exportIncludeAll    bool
	exportIncludeClosed bool
	exportExcludeLabels []string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export tasks to JSONL",
	Long: `Export all tasks (and their dependencies) to JSONL.

Formats:
  gur-jsonl     Native guardrails shape (lossless; default).
  beads-jsonl   beads 1.0.x compatible shape for interop. Lossy on:
                - archived status (emitted as closed + "archived" label)
                - hierarchical subtask IDs (flattened)`,
	RunE: runExport,
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVar(&exportFormat, "format", "gur-jsonl", "Output format (gur-jsonl, beads-jsonl)")
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "Output file path (default: stdout)")
	exportCmd.Flags().BoolVar(&exportIncludeAll, "all", false, "Include archived tasks")
	exportCmd.Flags().BoolVar(&exportIncludeClosed, "include-closed", true, "Include closed tasks")
	exportCmd.Flags().StringSliceVar(&exportExcludeLabels, "exclude-labels", nil, "Skip tasks carrying any of these labels (comma-separated or repeated)")
}

func runExport(cmd *cobra.Command, args []string) error {
	if exportFormat != "gur-jsonl" && exportFormat != "beads-jsonl" {
		return fmt.Errorf("unsupported --format %q (want gur-jsonl or beads-jsonl)", exportFormat)
	}

	database := db.GetDB()
	q := database.Model(&models.Task{})
	if !exportIncludeAll {
		q = q.Where("status != ?", models.StatusArchived)
	}
	if !exportIncludeClosed {
		q = q.Where("status NOT IN ?", []string{models.StatusClosed, models.StatusArchived})
	}
	var tasks []models.Task
	if err := q.Find(&tasks).Error; err != nil {
		return fmt.Errorf("query tasks: %w", err)
	}

	tasks = filterByLabels(tasks, exportExcludeLabels)

	depsByChild, err := loadDepsByChild(tasks)
	if err != nil {
		return err
	}

	w, closeFn, err := openExportWriter(exportOutput)
	if err != nil {
		return err
	}
	// Safety net for early returns; the success path closes explicitly below
	// so flush errors (e.g. disk full) are reported.
	defer func() { _ = closeFn() }()

	switch exportFormat {
	case "gur-jsonl":
		err = ioformat.EncodeGurJSONL(w, tasks, depsByChild)
	case "beads-jsonl":
		// Build the id map from every task in the DB (not just the exported
		// subset) so dep refs to excluded tasks still resolve to bd IDs.
		var allTasks []models.Task
		if err := database.Find(&allTasks).Error; err != nil {
			return fmt.Errorf("query tasks for id map: %w", err)
		}
		err = ioformat.EncodeBeadsJSONL(w, tasks, depsByChild, ioformat.BuildBeadsIDMap(allTasks))
	}
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := closeFn(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}

	// When the JSONL stream goes to stdout, keep stdout pure: no summary at
	// all in --json mode (it would corrupt the stream), stderr otherwise.
	if exportOutput == "" {
		return nil
	}
	if IsJSONOutput() {
		OutputJSON(map[string]any{"exported": len(tasks), "format": exportFormat, "output": exportOutput})
	} else {
		fmt.Fprintf(os.Stderr, "exported %d task(s) to %s\n", len(tasks), exportOutput)
	}
	return nil
}

func filterByLabels(tasks []models.Task, exclude []string) []models.Task {
	if len(exclude) == 0 {
		return tasks
	}
	excludeSet := make(map[string]bool, len(exclude))
	for _, l := range exclude {
		excludeSet[l] = true
	}
	out := tasks[:0]
	for _, t := range tasks {
		skip := false
		for _, l := range t.Labels {
			if excludeSet[l] {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, t)
		}
	}
	return out
}

// depQueryChunk keeps the IN(...) bind-variable count well under SQLite's
// limit so exports of very large DBs don't fail.
const depQueryChunk = 500

func loadDepsByChild(tasks []models.Task) (map[string][]models.Dependency, error) {
	if len(tasks) == 0 {
		return nil, nil
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	out := make(map[string][]models.Dependency)
	for start := 0; start < len(ids); start += depQueryChunk {
		end := min(start+depQueryChunk, len(ids))
		var deps []models.Dependency
		if err := db.GetDB().Where("child_id IN ?", ids[start:end]).Find(&deps).Error; err != nil {
			return nil, fmt.Errorf("query deps: %w", err)
		}
		for _, d := range deps {
			out[d.ChildID] = append(out[d.ChildID], d)
		}
	}
	return out, nil
}

func openExportWriter(path string) (io.Writer, func() error, error) {
	if path == "" {
		return os.Stdout, func() error { return nil }, nil
	}
	// Reject suspicious paths just enough to avoid accidental surprises.
	if strings.HasPrefix(path, "-") {
		return nil, nil, fmt.Errorf("refusing to write to path %q", path)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("create output: %w", err)
	}
	return f, f.Close, nil
}
