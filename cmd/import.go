package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gorm.io/gorm"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/ioformat"
	"github.com/Giancarlos/guardrails/internal/models"
)

var (
	importFormat      string
	importDryRun      bool
	importOnConflict  string
	importNoComments  bool
	importTypeMaps    []string
	importLabelPrefix string
)

var importCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import tasks from an external format",
	Long: `Import tasks into guardrails from an external format.

Currently supports beads 1.0.x JSONL (the output of 'bd export').

Idempotency: tasks are matched on their original bd id (stored in the
source_id column). Default --on-conflict=update upserts existing rows.

Non-issue records (memories, agents, rigs, roles, messages) are skipped
with a summary warning at the end.`,
	Args: cobra.ExactArgs(1),
	RunE: runImport,
}

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.Flags().StringVar(&importFormat, "format", "beads-jsonl", "Input format (beads-jsonl)")
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "Parse + validate, print summary, no writes")
	importCmd.Flags().StringVar(&importOnConflict, "on-conflict", "update", "Behavior when source_id already exists: update, skip, error")
	importCmd.Flags().BoolVar(&importNoComments, "no-comments", false, "Do not fold comments into notes")
	importCmd.Flags().StringArrayVar(&importTypeMaps, "map-type", nil, "Override type mapping, e.g. --map-type spike=bug (repeatable)")
	importCmd.Flags().StringVar(&importLabelPrefix, "label-prefix", "bd:", "Prefix for synthesized labels")
}

func runImport(cmd *cobra.Command, args []string) error {
	if importFormat != "beads-jsonl" {
		return fmt.Errorf("unsupported --format %q (only beads-jsonl is supported)", importFormat)
	}
	switch importOnConflict {
	case "update", "skip", "error":
	default:
		return fmt.Errorf("invalid --on-conflict %q (want update, skip, or error)", importOnConflict)
	}

	typeOverrides, err := parseTypeMaps(importTypeMaps)
	if err != nil {
		return err
	}

	f, err := os.Open(args[0])
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer f.Close()

	res, err := ioformat.DecodeBeadsJSONL(f)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	opts := ioformat.DefaultConvertOptions()
	opts.IncludeComments = !importNoComments
	opts.LabelPrefix = importLabelPrefix
	opts.TypeOverrides = typeOverrides

	converted := make([]ioformat.ConvertedTask, 0, len(res.Issues))
	for _, iss := range res.Issues {
		converted = append(converted, ioformat.ConvertIssue(iss, opts))
	}

	summary := map[string]any{
		"dry_run":          importDryRun,
		"file":             args[0],
		"issues":           len(converted),
		"memories_skipped": res.MemoriesSkipped,
		"infra_skipped":    res.InfraSkipped,
		"invalid_lines":    res.InvalidLines,
	}
	if len(res.LineErrors) > 0 {
		summary["line_errors"] = res.LineErrors
	}

	if importDryRun {
		printImportSummary(summary, 0, 0, nil)
		return nil
	}

	inserted, updated, skippedDeps, err := applyImport(converted, importOnConflict)
	if err != nil {
		return err
	}

	summary["inserted"] = inserted
	summary["updated"] = updated
	summary["deps_skipped"] = len(skippedDeps)
	printImportSummary(summary, inserted, updated, skippedDeps)
	return nil
}

func parseTypeMaps(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	validTargets := map[string]bool{
		models.TypeTask: true, models.TypeBug: true,
		models.TypeFeature: true, models.TypeEpic: true,
	}
	m := make(map[string]string, len(raw))
	for _, s := range raw {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" || v == "" {
			return nil, fmt.Errorf("invalid --map-type %q: want bd_type=gur_type", s)
		}
		if !validTargets[v] {
			return nil, fmt.Errorf("invalid --map-type %q: target must be one of task, bug, feature, epic", s)
		}
		m[k] = v
	}
	return m, nil
}

// reaches reports whether target is reachable from start by following the
// blocker -> blocked edges in adj (BFS, mirrors wouldCreateCycle).
func reaches(adj map[string][]string, start, target string) bool {
	visited := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adj[current] {
			if next == target {
				return true
			}
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}

// applyImport runs the whole insert/upsert in a single transaction.
// Returns (inserted, updated, skippedDeps, error). skippedDeps lists
// dependency edges dropped for unknown targets, self-references, or cycles.
func applyImport(converted []ioformat.ConvertedTask, onConflict string) (int, int, []string, error) {
	database := db.GetDB()
	var inserted, updated int
	var skippedDeps []string

	err := database.Transaction(func(tx *gorm.DB) error {
		bdToGur := make(map[string]string, len(converted))

		for i := range converted {
			ct := &converted[i]
			existing, found, err := findBySourceID(tx, ct.SourceID)
			if err != nil {
				return err
			}
			if found {
				if onConflict == "error" {
					return fmt.Errorf("task with source_id %q already exists (id=%s); use --on-conflict=update or skip", ct.SourceID, existing.ID)
				}
				bdToGur[ct.SourceID] = existing.ID
				if onConflict == "skip" {
					continue
				}
				// Update only the columns beads owns, via UpdateColumns so
				// GORM's autoUpdateTime doesn't clobber the imported
				// updated_at. Local-only columns (parent_id, synced, summary,
				// compacted) are left untouched.
				updates := map[string]any{
					"title":        ct.Task.Title,
					"description":  ct.Task.Description,
					"status":       ct.Task.Status,
					"priority":     ct.Task.Priority,
					"type":         ct.Task.Type,
					"labels":       ct.Task.Labels,
					"assignee":     ct.Task.Assignee,
					"notes":        ct.Task.Notes,
					"close_reason": ct.Task.CloseReason,
					"source":       ct.Task.Source,
					"updated_at":   ct.Task.UpdatedAt,
					"closed_at":    ct.Task.ClosedAt,
				}
				if err := tx.Model(&models.Task{}).Where("id = ?", existing.ID).
					UpdateColumns(updates).Error; err != nil {
					return fmt.Errorf("update %s: %w", ct.SourceID, err)
				}
				updated++
				continue
			}

			ct.Task.ID = models.GenerateID()
			if err := tx.Create(&ct.Task).Error; err != nil {
				return fmt.Errorf("insert %s: %w", ct.SourceID, err)
			}
			bdToGur[ct.SourceID] = ct.Task.ID
			inserted++
		}

		// Pass 2: dependencies, once every source id has a gur id. Mirror
		// `gur dep add` invariants: no self-references, no cycles.
		var existingDeps []models.Dependency
		if err := tx.Find(&existingDeps).Error; err != nil {
			return fmt.Errorf("load deps: %w", err)
		}
		type edge struct{ parent, child, typ string }
		seen := make(map[edge]bool, len(existingDeps))
		adj := make(map[string][]string) // blocker -> blocked, all dep types
		for _, d := range existingDeps {
			seen[edge{d.ParentID, d.ChildID, d.Type}] = true
			adj[d.ParentID] = append(adj[d.ParentID], d.ChildID)
		}

		for _, ct := range converted {
			for _, d := range ct.DepTargets {
				from, okF := bdToGur[d.FromSourceID]
				to, okT := bdToGur[d.ToSourceID]
				if !okF || !okT {
					skippedDeps = append(skippedDeps, fmt.Sprintf("%s->%s (%s): unknown target", d.FromSourceID, d.ToSourceID, d.Type))
					continue
				}
				if from == to {
					skippedDeps = append(skippedDeps, fmt.Sprintf("%s->%s (%s): self-reference", d.FromSourceID, d.ToSourceID, d.Type))
					continue
				}
				if seen[edge{to, from, d.Type}] {
					continue
				}
				if reaches(adj, from, to) {
					skippedDeps = append(skippedDeps, fmt.Sprintf("%s->%s (%s): would create a cycle", d.FromSourceID, d.ToSourceID, d.Type))
					continue
				}
				dep := models.Dependency{ParentID: to, ChildID: from, Type: d.Type}
				if err := tx.Create(&dep).Error; err != nil {
					return fmt.Errorf("dep %s->%s: %w", from, to, err)
				}
				seen[edge{to, from, d.Type}] = true
				adj[to] = append(adj[to], from)
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, nil, err
	}
	return inserted, updated, skippedDeps, nil
}

func findBySourceID(tx *gorm.DB, sid string) (*models.Task, bool, error) {
	var t models.Task
	err := tx.Where("source_id = ?", sid).First(&t).Error
	if err == nil {
		return &t, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func printImportSummary(summary map[string]any, inserted, updated int, skippedDeps []string) {
	if IsJSONOutput() {
		if skippedDeps != nil {
			summary["skipped_deps"] = skippedDeps
		}
		OutputJSON(summary)
		return
	}

	fmt.Printf("imported from %s: %d issue(s)\n", summary["file"], summary["issues"])
	if !importDryRun {
		fmt.Printf("  inserted: %d, updated: %d\n", inserted, updated)
	} else {
		fmt.Println("  (dry run — no writes)")
	}

	if n, _ := summary["memories_skipped"].(int); n > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: dropped %d beads memories (guardrails has no memory store)\n", n)
	}
	if n, _ := summary["infra_skipped"].(int); n > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: skipped %d infrastructure/unknown records (agents, rigs, roles, messages, custom types)\n", n)
	}
	if n, _ := summary["invalid_lines"].(int); n > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d line(s) failed to parse and were skipped:\n", n)
		lineErrs, _ := summary["line_errors"].([]string)
		const maxShown = 5
		for i, le := range lineErrs {
			if i == maxShown {
				break
			}
			fmt.Fprintf(os.Stderr, "  %s\n", le)
		}
		// n is the true count; lineErrs detail may be capped below it.
		if n > maxShown {
			fmt.Fprintf(os.Stderr, "  ... and %d more\n", n-maxShown)
		}
	}
	if len(skippedDeps) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d dependencies were skipped (unknown target, self-reference, or cycle):\n", len(skippedDeps))
		for _, d := range skippedDeps {
			fmt.Fprintf(os.Stderr, "  %s\n", d)
		}
	}
}
