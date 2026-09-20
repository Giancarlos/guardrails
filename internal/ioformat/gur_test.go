package ioformat

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Giancarlos/guardrails/internal/models"
)

func decodeBeadsLines(t *testing.T, buf *bytes.Buffer) []BeadsIssue {
	t.Helper()
	var out []BeadsIssue
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var iss BeadsIssue
		if err := json.Unmarshal([]byte(line), &iss); err != nil {
			t.Fatalf("re-parse %q: %v", line, err)
		}
		out = append(out, iss)
	}
	return out
}

func TestEncodeBeadsJSONL_SubtaskFlattening(t *testing.T) {
	parent := models.Task{ID: "gur-aaaa1111", Title: "parent", Status: models.StatusOpen, Type: models.TypeEpic}
	child := models.Task{ID: "gur-aaaa1111.1", ParentID: "gur-aaaa1111", Title: "child", Status: models.StatusOpen, Type: models.TypeTask}

	var buf bytes.Buffer
	if err := EncodeBeadsJSONL(&buf, []models.Task{parent, child}, nil, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}

	issues := decodeBeadsLines(t, &buf)
	if len(issues) != 2 {
		t.Fatalf("want 2 issues, got %d", len(issues))
	}
	if issues[1].ID != "gur-aaaa1111-1" {
		t.Errorf("hierarchical id not flattened: got %q", issues[1].ID)
	}
	if len(issues[1].Dependencies) != 1 {
		t.Fatalf("want synthesized parent-child dep, got %v", issues[1].Dependencies)
	}
	d := issues[1].Dependencies[0]
	if d.Type != "parent-child" || d.DependsOnID != "gur-aaaa1111" || d.IssueID != "gur-aaaa1111-1" {
		t.Errorf("synthesized dep mismatch: %+v", d)
	}
}

func TestEncodeBeadsJSONL_NoDuplicateParentDep(t *testing.T) {
	child := models.Task{ID: "gur-aaaa1111.1", ParentID: "gur-aaaa1111", Title: "child", Status: models.StatusOpen}
	deps := map[string][]models.Dependency{
		child.ID: {{ParentID: "gur-aaaa1111", ChildID: child.ID, Type: models.DepTypeParentChild}},
	}

	var buf bytes.Buffer
	if err := EncodeBeadsJSONL(&buf, []models.Task{child}, deps, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	issues := decodeBeadsLines(t, &buf)
	if len(issues[0].Dependencies) != 1 {
		t.Errorf("parent-child dep duplicated: %v", issues[0].Dependencies)
	}
}

func TestTrailer_HostileExternalRef(t *testing.T) {
	ref := "https://x.test/q?a=b;due_at=2030-01-01T00:00:00Z;c=d"
	iss := BeadsIssue{ID: "bd-1", Title: "t", IssueType: "task", Status: "open",
		ExternalRef: ref, EstimatedMinutes: 45}
	ct := ConvertIssue(iss, DefaultConvertOptions())

	notes, trailer := splitNotesTrailer(ct.Task.Notes)
	if notes != "" {
		t.Errorf("notes should be empty, got %q", notes)
	}
	var out BeadsIssue
	applyTrailer(&out, trailer)
	if out.ExternalRef != ref {
		t.Errorf("external_ref corrupted: want %q, got %q", ref, out.ExternalRef)
	}
	if out.DueAt != nil {
		t.Errorf("phantom due_at injected: %v", out.DueAt)
	}
	if out.EstimatedMinutes != 45 {
		t.Errorf("estimated_minutes: want 45, got %d", out.EstimatedMinutes)
	}
}

func TestTrailer_MidNotesFakeTrailerPreserved(t *testing.T) {
	iss := BeadsIssue{ID: "bd-1", Title: "t", IssueType: "task", Status: "open",
		Notes:            "before\n<!-- bd: estimated_minutes=111 -->\nafter",
		EstimatedMinutes: 30}
	ct := ConvertIssue(iss, DefaultConvertOptions())

	notes, trailer := splitNotesTrailer(ct.Task.Notes)
	if !strings.Contains(notes, "before") || !strings.Contains(notes, "after") ||
		!strings.Contains(notes, "<!-- bd: estimated_minutes=111 -->") {
		t.Errorf("mid-notes content lost: %q", notes)
	}
	var out BeadsIssue
	applyTrailer(&out, trailer)
	if out.EstimatedMinutes != 30 {
		t.Errorf("real trailer lost: want est 30, got %d", out.EstimatedMinutes)
	}
}

func TestTrailer_FakeKVTrailerAtEndPreserved(t *testing.T) {
	// User notes ending with old k=v-shaped trailer text, no real trailer:
	// must be kept as plain notes, not parsed.
	notes := "real notes\n<!-- bd: estimated_minutes=999 -->"
	gotNotes, trailer := splitNotesTrailer(notes)
	if trailer != "" {
		t.Errorf("k=v text wrongly treated as trailer: %q", trailer)
	}
	if gotNotes != notes {
		t.Errorf("notes mutated: %q", gotNotes)
	}
}

func TestEncodeBeadsJSONL_DepToExcludedTaskUsesSourceID(t *testing.T) {
	sid := "bd-probe-xtw"
	blocker := models.Task{ID: "gur-bbbb2222", SourceID: &sid, Title: "blocker", Status: models.StatusArchived}
	blocked := models.Task{ID: "gur-cccc3333", Title: "blocked", Status: models.StatusOpen}
	deps := map[string][]models.Dependency{
		blocked.ID: {{ParentID: blocker.ID, ChildID: blocked.ID, Type: models.DepTypeBlocks}},
	}

	// The id map covers all tasks; the export itself excludes the blocker.
	idMap := BuildBeadsIDMap([]models.Task{blocker, blocked})
	var buf bytes.Buffer
	if err := EncodeBeadsJSONL(&buf, []models.Task{blocked}, deps, idMap); err != nil {
		t.Fatalf("encode: %v", err)
	}
	issues := decodeBeadsLines(t, &buf)
	if got := issues[0].Dependencies[0].DependsOnID; got != sid {
		t.Errorf("dep to excluded task: want %q, got %q", sid, got)
	}
}
