package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/ioformat"
	"github.com/Giancarlos/guardrails/internal/models"
)

// Importing gur's own export is a restore, not a translation: IDs are already
// valid gur IDs, so records match on the primary key rather than source_id,
// and anything the file doesn't carry is left alone.

// runGurImport handles --format gur-jsonl and gur-json.
func runGurImport(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer f.Close()

	var records []ioformat.GurRecord
	if importFormat == "gur-json" {
		records, err = ioformat.DecodeGurJSON(f)
	} else {
		records, err = ioformat.DecodeGurJSONL(f)
	}
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	if err := validateGurRecords(records); err != nil {
		return err
	}

	summary := map[string]any{
		"dry_run": importDryRun,
		"file":    path,
		"format":  importFormat,
		"tasks":   len(records),
	}
	if importDryRun {
		printGurImportSummary(summary, 0, 0, 0, nil)
		return nil
	}

	inserted, updated, skipped, skippedDeps, err := applyGurImport(records, importOnConflict)
	if err != nil {
		return err
	}
	summary["inserted"] = inserted
	summary["updated"] = updated
	summary["skipped"] = skipped
	summary["deps_skipped"] = len(skippedDeps)
	printGurImportSummary(summary, inserted, updated, skipped, skippedDeps)
	return nil
}

// validateGurRecords rejects the whole file if any record is malformed, so a
// bad export can never half-apply.
func validateGurRecords(records []ioformat.GurRecord) error {
	seen := make(map[string]bool, len(records))
	inFile := make(map[string]bool, len(records))

	for _, r := range records {
		if !models.ValidateTaskID(r.Task.ID) {
			return fmt.Errorf("record %d: invalid task id %q", r.Line, r.Task.ID)
		}
		if seen[r.Task.ID] {
			return fmt.Errorf("record %d: duplicate task id %q", r.Line, r.Task.ID)
		}
		seen[r.Task.ID] = true
		inFile[r.Task.ID] = true
	}

	database := db.GetDB()

	// Two chunked lookups instead of two queries per record: one for the rows
	// these records would update, one for parents the file doesn't contain.
	ids := make([]string, 0, len(records))
	for _, r := range records {
		ids = append(ids, r.Task.ID)
	}
	existingByID, err := loadTasksByID(database, ids)
	if err != nil {
		return err
	}

	var parentIDs []string
	for _, r := range records {
		if r.Task.ParentID != "" && !inFile[r.Task.ParentID] {
			parentIDs = append(parentIDs, r.Task.ParentID)
		}
	}
	knownParents, err := loadTasksByID(database, parentIDs)
	if err != nil {
		return err
	}

	// source_id carries a unique index, so a collision would otherwise surface
	// as a raw SQLite constraint error halfway through the transaction.
	var sourceIDs []string
	inFileBySource := make(map[string]string, len(records))
	for _, r := range records {
		sid := derefString(r.Task.SourceID)
		if sid == "" {
			continue
		}
		if other, dup := inFileBySource[sid]; dup {
			return fmt.Errorf("record %d (%s): source_id %q is also used by %s in the same file", r.Line, r.Task.ID, sid, other)
		}
		inFileBySource[sid] = r.Task.ID
		sourceIDs = append(sourceIDs, sid)
	}
	claimedSources, err := loadTasksBySourceID(database, sourceIDs)
	if err != nil {
		return err
	}
	for _, r := range records {
		sid := derefString(r.Task.SourceID)
		if sid == "" {
			continue
		}
		if owner, taken := claimedSources[sid]; taken && owner != r.Task.ID {
			return fmt.Errorf("record %d (%s): source_id %q already belongs to task %s", r.Line, r.Task.ID, sid, owner)
		}
	}

	for _, r := range records {
		if r.Has("title") && r.Task.Title == "" {
			return fmt.Errorf("record %d (%s): title must not be empty", r.Line, r.Task.ID)
		}
		if r.Has("status") && !models.IsValidStatus(r.Task.Status) {
			return fmt.Errorf("record %d (%s): invalid status %q", r.Line, r.Task.ID, r.Task.Status)
		}
		if r.Has("type") && !models.IsValidType(r.Task.Type) {
			return fmt.Errorf("record %d (%s): invalid type %q", r.Line, r.Task.ID, r.Task.Type)
		}
		if r.Has("priority") && (r.Task.Priority < 0 || r.Task.Priority > 4) {
			return fmt.Errorf("record %d (%s): invalid priority %d (want 0-4)", r.Line, r.Task.ID, r.Task.Priority)
		}
		if r.Task.TokensUsed < 0 || r.Task.TokensBudget < 0 {
			return fmt.Errorf("record %d (%s): token counts must not be negative", r.Line, r.Task.ID)
		}

		// A parent may appear anywhere in the same file, or already exist.
		if r.Task.ParentID != "" && !inFile[r.Task.ParentID] {
			if _, ok := knownParents[r.Task.ParentID]; !ok {
				return fmt.Errorf("record %d (%s): parent %q does not exist", r.Line, r.Task.ID, r.Task.ParentID)
			}
		}

		existing, found := existingByID[r.Task.ID]
		if !found {
			if !r.Has("title") || r.Task.Title == "" {
				return fmt.Errorf("record %d (%s): title is required to create a task", r.Line, r.Task.ID)
			}
			continue
		}

		// Closing and reopening run gate checks and hooks, so an import must
		// not do it as a side effect.
		if r.Has("status") && r.Task.Status != existing.Status {
			wasClosed := existing.Status == models.StatusClosed
			isClosed := r.Task.Status == models.StatusClosed
			if wasClosed != isClosed {
				verb := "close"
				if wasClosed {
					verb = "reopen"
				}
				return fmt.Errorf("record %d (%s): import cannot %s a task; use 'gur %s %s'", r.Line, r.Task.ID, verb, verb, r.Task.ID)
			}
		}
	}
	return nil
}

// loadTasksBySourceID maps each existing source_id to the task that owns it.
func loadTasksBySourceID(database *gorm.DB, sourceIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(sourceIDs))
	for start := 0; start < len(sourceIDs); start += depQueryChunk {
		end := min(start+depQueryChunk, len(sourceIDs))
		var found []models.Task
		if err := database.Where("source_id IN ?", sourceIDs[start:end]).Find(&found).Error; err != nil {
			return nil, fmt.Errorf("look up source ids: %w", err)
		}
		for _, t := range found {
			out[derefString(t.SourceID)] = t.ID
		}
	}
	return out, nil
}

// loadTasksByID fetches the given ids in chunks, mirroring the export side's
// limit on SQLite bind variables.
func loadTasksByID(database *gorm.DB, ids []string) (map[string]models.Task, error) {
	out := make(map[string]models.Task, len(ids))
	for start := 0; start < len(ids); start += depQueryChunk {
		end := min(start+depQueryChunk, len(ids))
		var found []models.Task
		if err := database.Where("id IN ?", ids[start:end]).Find(&found).Error; err != nil {
			return nil, fmt.Errorf("look up tasks: %w", err)
		}
		for _, t := range found {
			out[t.ID] = t
		}
	}
	return out, nil
}

// gurImportColumns maps a record's JSON field to its database column. Only
// these are restorable; the rest are derived or local-only.
var gurImportColumns = map[string]string{
	"title": "title", "description": "description", "status": "status",
	"priority": "priority", "type": "type", "labels": "labels",
	"assignee": "assignee", "notes": "notes", "close_reason": "close_reason",
	"summary": "summary", "source": "source", "source_id": "source_id",
	"parent_id": "parent_id", "tokens_used": "tokens_used",
	"tokens_budget": "tokens_budget", "context_summary": "context_summary",
	"closed_at": "closed_at",
}

func applyGurImport(records []ioformat.GurRecord, onConflict string) (int, int, int, []string, error) {
	database := db.GetDB()
	var inserted, updated, skipped int
	var skippedDeps []string

	err := database.Transaction(func(tx *gorm.DB) error {
		for _, r := range records {
			var existing models.Task
			err := tx.Where("id = ?", r.Task.ID).First(&existing).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				task := r.Task
				// Match 'gur create' defaults for anything the file omits.
				if !r.Has("status") || task.Status == "" {
					task.Status = models.StatusOpen
				}
				if !r.Has("type") || task.Type == "" {
					task.Type = models.TypeTask
				}
				if !r.Has("priority") {
					task.Priority = models.PriorityMedium
				}
				if !r.Has("source") || task.Source == "" {
					task.Source = models.SourceLocal
				}
				if err := tx.Create(&task).Error; err != nil {
					return fmt.Errorf("insert %s: %w", r.Task.ID, err)
				}
				inserted++
			case err != nil:
				return fmt.Errorf("look up %s: %w", r.Task.ID, err)
			default:
				if onConflict == "error" {
					return fmt.Errorf("task %s already exists; use --on-conflict=update or skip", r.Task.ID)
				}
				if onConflict == "skip" {
					skipped++
					continue
				}
				changed, err := updateGurTask(tx, &existing, r)
				if err != nil {
					return err
				}
				if changed {
					updated++
				} else {
					skipped++
				}
			}
		}

		return importGurDeps(tx, records, &skippedDeps)
	})
	if err != nil {
		return 0, 0, 0, nil, err
	}
	return inserted, updated, skipped, skippedDeps, nil
}

// updateGurTask writes only the fields the record carried, recording history
// for each one that actually changed.
func updateGurTask(tx *gorm.DB, existing *models.Task, r ioformat.GurRecord) (bool, error) {
	updates := map[string]any{}
	incoming := r.Task

	for field, column := range gurImportColumns {
		if !r.Has(field) {
			continue
		}
		var oldVal, newVal string
		var value any
		switch field {
		case "title":
			oldVal, newVal, value = existing.Title, incoming.Title, incoming.Title
		case "description":
			oldVal, newVal, value = existing.Description, incoming.Description, incoming.Description
		case "status":
			oldVal, newVal, value = existing.Status, incoming.Status, incoming.Status
		case "priority":
			oldVal, newVal, value = fmt.Sprint(existing.Priority), fmt.Sprint(incoming.Priority), incoming.Priority
		case "type":
			oldVal, newVal, value = existing.Type, incoming.Type, incoming.Type
		case "labels":
			oldVal, newVal, value = fmt.Sprint(existing.Labels), fmt.Sprint(incoming.Labels), incoming.Labels
		case "assignee":
			oldVal, newVal, value = existing.Assignee, incoming.Assignee, incoming.Assignee
		case "notes":
			oldVal, newVal, value = existing.Notes, incoming.Notes, incoming.Notes
		case "close_reason":
			oldVal, newVal, value = existing.CloseReason, incoming.CloseReason, incoming.CloseReason
		case "summary":
			oldVal, newVal, value = existing.Summary, incoming.Summary, incoming.Summary
		case "source":
			oldVal, newVal, value = existing.Source, incoming.Source, incoming.Source
		case "source_id":
			oldVal, newVal, value = derefString(existing.SourceID), derefString(incoming.SourceID), incoming.SourceID
		case "parent_id":
			oldVal, newVal, value = existing.ParentID, incoming.ParentID, incoming.ParentID
		case "tokens_used":
			oldVal, newVal, value = fmt.Sprint(existing.TokensUsed), fmt.Sprint(incoming.TokensUsed), incoming.TokensUsed
		case "tokens_budget":
			oldVal, newVal, value = fmt.Sprint(existing.TokensBudget), fmt.Sprint(incoming.TokensBudget), incoming.TokensBudget
		case "context_summary":
			oldVal, newVal, value = existing.ContextSummary, incoming.ContextSummary, incoming.ContextSummary
		case "closed_at":
			oldVal, newVal, value = formatTimePtr(existing.ClosedAt), formatTimePtr(incoming.ClosedAt), incoming.ClosedAt
		}
		if oldVal == newVal {
			continue
		}
		updates[column] = value
		if err := models.RecordChange(tx, existing.ID, field, oldVal, newVal, "import"); err != nil {
			return false, fmt.Errorf("record history for %s: %w", existing.ID, err)
		}
	}

	if len(updates) == 0 {
		return false, nil
	}
	// UpdateColumns skips autoUpdateTime, so updated_at is set explicitly:
	// the file's value when it carries one, otherwise now (the row did change).
	if r.Has("updated_at") {
		updates["updated_at"] = incoming.UpdatedAt
	} else {
		updates["updated_at"] = time.Now()
	}
	if err := tx.Model(&models.Task{}).Where("id = ?", existing.ID).UpdateColumns(updates).Error; err != nil {
		return false, fmt.Errorf("update %s: %w", existing.ID, err)
	}
	return true, nil
}

// importGurDeps restores dependency edges, holding the same invariants as
// 'gur dep add': no self-references, no duplicates, no cycles.
func importGurDeps(tx *gorm.DB, records []ioformat.GurRecord, skippedDeps *[]string) error {
	var existingDeps []models.Dependency
	if err := tx.Find(&existingDeps).Error; err != nil {
		return fmt.Errorf("load deps: %w", err)
	}
	type edge struct{ parent, child, typ string }
	seen := make(map[edge]bool, len(existingDeps))
	adj := make(map[string][]string) // blocker -> blocked
	for _, d := range existingDeps {
		seen[edge{d.ParentID, d.ChildID, d.Type}] = true
		adj[d.ParentID] = append(adj[d.ParentID], d.ChildID)
	}

	for _, r := range records {
		for _, d := range r.Dependencies {
			label := fmt.Sprintf("%s->%s (%s)", d.ParentID, d.ChildID, d.Type)
			if d.ParentID == d.ChildID {
				*skippedDeps = append(*skippedDeps, label+": self-reference")
				continue
			}
			if seen[edge{d.ParentID, d.ChildID, d.Type}] {
				continue
			}
			var count int64
			if err := tx.Model(&models.Task{}).Where("id IN ?", []string{d.ParentID, d.ChildID}).Count(&count).Error; err != nil {
				return fmt.Errorf("check dep %s: %w", label, err)
			}
			if count != 2 {
				*skippedDeps = append(*skippedDeps, label+": unknown task")
				continue
			}
			if reaches(adj, d.ChildID, d.ParentID) {
				*skippedDeps = append(*skippedDeps, label+": would create a cycle")
				continue
			}
			dep := models.Dependency{ParentID: d.ParentID, ChildID: d.ChildID, Type: d.Type}
			if err := tx.Create(&dep).Error; err != nil {
				return fmt.Errorf("dep %s: %w", label, err)
			}
			seen[edge{d.ParentID, d.ChildID, d.Type}] = true
			adj[d.ParentID] = append(adj[d.ParentID], d.ChildID)
		}
	}
	return nil
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func printGurImportSummary(summary map[string]any, inserted, updated, skipped int, skippedDeps []string) {
	if IsJSONOutput() {
		if skippedDeps != nil {
			summary["skipped_deps"] = skippedDeps
		}
		OutputJSON(summary)
		return
	}

	fmt.Printf("imported from %s: %d task(s)\n", summary["file"], summary["tasks"])
	if importDryRun {
		fmt.Println("  (dry run — no writes)")
		return
	}
	fmt.Printf("  inserted: %d, updated: %d, skipped: %d\n", inserted, updated, skipped)
	for _, d := range skippedDeps {
		fmt.Fprintf(os.Stderr, "WARNING: skipped dependency %s\n", d)
	}
}
