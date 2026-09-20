package ioformat

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/Giancarlos/guardrails/internal/models"
)

// ExportTask is the on-wire shape of one task for gur's native JSONL export.
// It's structurally similar to BeadsIssue but uses gur's vocabulary.
type ExportTask struct {
	models.Task
	Dependencies []ExportDep `json:"dependencies,omitempty"`
}

// ExportDep is the on-wire shape of a dependency edge.
type ExportDep struct {
	ParentID string `json:"parent_id"` // blocker
	ChildID  string `json:"child_id"`  // blocked (owner of the record)
	Type     string `json:"type"`
}

// EncodeGurJSONL writes tasks as one-JSON-per-line. Each task carries its
// outgoing dependencies (child perspective) so the line is self-contained.
func EncodeGurJSONL(w io.Writer, tasks []models.Task, depsByChild map[string][]models.Dependency) error {
	enc := json.NewEncoder(w)
	for _, t := range tasks {
		var eds []ExportDep
		for _, d := range depsByChild[t.ID] {
			eds = append(eds, ExportDep{ParentID: d.ParentID, ChildID: d.ChildID, Type: d.Type})
		}
		if err := enc.Encode(ExportTask{Task: t, Dependencies: eds}); err != nil {
			return err
		}
	}
	return nil
}

// BuildBeadsIDMap maps gur task IDs to the IDs used in beads-jsonl export:
// the original bd source_id where available, otherwise the gur ID with
// hierarchical dots flattened ("gur-abc.1" → "gur-abc-1") so the result is
// bd-compatible. Build it from *all* tasks in the DB — not just the exported
// subset — so dep refs to excluded tasks (archived, label-filtered) still
// resolve to their bd IDs.
func BuildBeadsIDMap(tasks []models.Task) map[string]string {
	idMap := make(map[string]string, len(tasks))
	for _, t := range tasks {
		if t.SourceID != nil && *t.SourceID != "" {
			idMap[t.ID] = *t.SourceID
		} else {
			idMap[t.ID] = strings.ReplaceAll(t.ID, ".", "-")
		}
	}
	return idMap
}

// EncodeBeadsJSONL writes tasks in bd 1.0.x-compatible JSONL. Lossy on
// archived status (→ closed + label). Hierarchical subtask IDs are flattened
// ("gur-abc.1" → "gur-abc-1") and tasks.parent_id is emitted as a
// parent-child dependency so the hierarchy survives.
//
// idMap rewrites task refs to bd source IDs where available (see
// BuildBeadsIDMap); pass nil to build one from the exported tasks only.
func EncodeBeadsJSONL(w io.Writer, tasks []models.Task, depsByChild map[string][]models.Dependency, idMap map[string]string) error {
	if idMap == nil {
		idMap = BuildBeadsIDMap(tasks)
	}

	enc := json.NewEncoder(w)
	for _, t := range tasks {
		issue := taskToBeads(t, depsByChild[t.ID], idMap)
		if err := enc.Encode(issue); err != nil {
			return err
		}
	}
	return nil
}

const bdTrailerMarker = "<!-- bd: "

func taskToBeads(t models.Task, deps []models.Dependency, idMap map[string]string) BeadsIssue {
	status := t.Status
	labels := append([]string(nil), t.Labels...)
	if status == models.StatusArchived {
		status = "closed"
		labels = append(labels, "archived")
	}

	desc, design, acceptance := splitDescription(t.Description)
	notes, trailer := splitNotesTrailer(t.Notes)

	issue := BeadsIssue{
		// idMap resolves to the bd source_id when the task was imported from
		// bd, otherwise the (flattened) gur ID.
		ID:                 resolveExportID(t.ID, idMap),
		Title:              t.Title,
		Description:        desc,
		Design:             design,
		AcceptanceCriteria: acceptance,
		Notes:              notes,
		Status:             status,
		Priority:           t.Priority,
		IssueType:          mapTypeToBeads(t.Type),
		Assignee:           t.Assignee,
		CreatedAt:          t.CreatedAt,
		UpdatedAt:          t.UpdatedAt,
		ClosedAt:           t.ClosedAt,
		CloseReason:        t.CloseReason,
		Labels:             labels,
	}

	applyTrailer(&issue, trailer)

	hasParentDep := false
	for _, d := range deps {
		if d.Type == models.DepTypeParentChild && d.ParentID == t.ParentID {
			hasParentDep = true
		}
		issue.Dependencies = append(issue.Dependencies, BeadsDependency{
			IssueID:     resolveExportID(d.ChildID, idMap),
			DependsOnID: resolveExportID(d.ParentID, idMap),
			Type:        mapDepTypeToBeads(d.Type),
		})
	}

	// Subtask hierarchy lives in tasks.parent_id, not the dependencies table;
	// emit it as a parent-child dep so bd tooling sees it.
	if t.ParentID != "" && !hasParentDep {
		issue.Dependencies = append(issue.Dependencies, BeadsDependency{
			IssueID:     resolveExportID(t.ID, idMap),
			DependsOnID: resolveExportID(t.ParentID, idMap),
			Type:        "parent-child",
		})
	}

	return issue
}

func resolveExportID(gurID string, idMap map[string]string) string {
	if v, ok := idMap[gurID]; ok {
		return v
	}
	return gurID
}

func mapTypeToBeads(t string) string {
	switch t {
	case models.TypeBug:
		return "bug"
	case models.TypeFeature:
		return "feature"
	case models.TypeEpic:
		return "epic"
	default:
		return "task"
	}
}

func mapDepTypeToBeads(t string) string {
	switch t {
	case models.DepTypeParentChild:
		return "parent-child"
	case models.DepTypeRelated:
		return "related"
	default:
		return "blocks"
	}
}

// splitDescription reverses buildDescription: extracts optional ## Design and
// ## Acceptance Criteria sections when they sit at the end.
func splitDescription(s string) (desc, design, acceptance string) {
	desc = s
	if idx := strings.LastIndex(desc, "\n\n## Acceptance Criteria\n\n"); idx >= 0 {
		acceptance = desc[idx+len("\n\n## Acceptance Criteria\n\n"):]
		desc = desc[:idx]
	} else if strings.HasPrefix(desc, "## Acceptance Criteria\n\n") {
		acceptance = desc[len("## Acceptance Criteria\n\n"):]
		desc = ""
	}
	if idx := strings.LastIndex(desc, "\n\n## Design\n\n"); idx >= 0 {
		design = desc[idx+len("\n\n## Design\n\n"):]
		desc = desc[:idx]
	} else if strings.HasPrefix(desc, "## Design\n\n") {
		design = desc[len("## Design\n\n"):]
		desc = ""
	}
	return
}

// splitNotesTrailer returns (notes, trailer) where trailer is the JSON
// payload of a `<!-- bd: {...} -->` comment at the very end of notes. Only
// the LAST marker occurrence is considered (earlier ones are user content),
// and anything that isn't valid trailer JSON is treated as plain notes.
func splitNotesTrailer(notes string) (string, string) {
	idx := strings.LastIndex(notes, bdTrailerMarker)
	if idx < 0 {
		return notes, ""
	}
	rest := strings.TrimRight(notes[idx+len(bdTrailerMarker):], " \t\n")
	payload, ok := strings.CutSuffix(rest, " -->")
	if !ok {
		return notes, ""
	}
	if !json.Valid([]byte(payload)) || !strings.HasPrefix(payload, "{") {
		return notes, ""
	}
	return strings.TrimRight(notes[:idx], "\n"), payload
}

func applyTrailer(issue *BeadsIssue, trailer string) {
	if trailer == "" {
		return
	}
	var tr bdTrailer
	if err := json.Unmarshal([]byte(trailer), &tr); err != nil {
		return
	}
	issue.EstimatedMinutes = tr.EstimatedMinutes
	issue.DueAt = tr.DueAt
	issue.DeferUntil = tr.DeferUntil
	issue.ExternalRef = tr.ExternalRef
}
