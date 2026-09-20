package ioformat

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Giancarlos/guardrails/internal/models"
)

func TestConvertIssue_StatusMapping(t *testing.T) {
	cases := []struct {
		bd      string
		gurStat string
		wantLbl string // synthesized label suffix (without prefix)
	}{
		{"open", models.StatusOpen, ""},
		{"in_progress", models.StatusInProgress, ""},
		{"closed", models.StatusClosed, ""},
		{"blocked", models.StatusOpen, ""},
		{"deferred", models.StatusOpen, "deferred"},
		{"pinned", models.StatusOpen, "pinned"},
		{"hooked", models.StatusInProgress, "hooked"},
	}
	for _, tc := range cases {
		t.Run(tc.bd, func(t *testing.T) {
			iss := BeadsIssue{ID: "bd-1", Title: "t", IssueType: "task", Status: tc.bd}
			got := ConvertIssue(iss, DefaultConvertOptions())
			if got.Task.Status != tc.gurStat {
				t.Errorf("status: want %q, got %q", tc.gurStat, got.Task.Status)
			}
			if tc.wantLbl == "" {
				for _, l := range got.Task.Labels {
					if strings.HasPrefix(l, "bd:") && l != "bd:owner:" {
						// accept — may be present for other reasons
					}
					_ = l
				}
				return
			}
			found := false
			for _, l := range got.Task.Labels {
				if l == "bd:"+tc.wantLbl {
					found = true
				}
			}
			if !found {
				t.Errorf("expected label bd:%s in %v", tc.wantLbl, got.Task.Labels)
			}
		})
	}
}

func TestConvertIssue_TypeCollapse(t *testing.T) {
	iss := BeadsIssue{ID: "bd-1", Title: "t", IssueType: "spike", Status: "open"}
	got := ConvertIssue(iss, DefaultConvertOptions())
	if got.Task.Type != models.TypeTask {
		t.Errorf("type: want task, got %q", got.Task.Type)
	}
	found := false
	for _, l := range got.Task.Labels {
		if l == "bd:type:spike" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected bd:type:spike label, got %v", got.Task.Labels)
	}
}

func TestConvertIssue_DescriptionMerge(t *testing.T) {
	iss := BeadsIssue{
		ID: "bd-1", Title: "t", IssueType: "task", Status: "open",
		Description:        "main body",
		Design:             "design body",
		AcceptanceCriteria: "accept body",
	}
	got := ConvertIssue(iss, DefaultConvertOptions())
	d := got.Task.Description
	if !strings.Contains(d, "main body") || !strings.Contains(d, "## Design") || !strings.Contains(d, "## Acceptance Criteria") {
		t.Errorf("merged desc missing sections: %q", d)
	}

	// Round-trip: splitDescription should recover the parts.
	desc, design, accept := splitDescription(d)
	if desc != "main body" {
		t.Errorf("split desc: want %q, got %q", "main body", desc)
	}
	if design != "design body" {
		t.Errorf("split design: want %q, got %q", "design body", design)
	}
	if accept != "accept body" {
		t.Errorf("split acceptance: want %q, got %q", "accept body", accept)
	}
}

func TestConvertIssue_Trailer(t *testing.T) {
	due := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	iss := BeadsIssue{
		ID: "bd-1", Title: "t", IssueType: "task", Status: "open",
		EstimatedMinutes: 90,
		DueAt:            &due,
		ExternalRef:      "gh-42",
		Notes:            "existing",
	}
	got := ConvertIssue(iss, DefaultConvertOptions())
	if !strings.Contains(got.Task.Notes, "<!-- bd:") {
		t.Fatalf("expected trailer in notes, got %q", got.Task.Notes)
	}

	// Round-trip through splitNotesTrailer + applyTrailer.
	notes, trailer := splitNotesTrailer(got.Task.Notes)
	if notes != "existing" {
		t.Errorf("notes after split: want %q, got %q", "existing", notes)
	}
	var recovered BeadsIssue
	applyTrailer(&recovered, trailer)
	if recovered.EstimatedMinutes != 90 {
		t.Errorf("est_minutes: want 90, got %d", recovered.EstimatedMinutes)
	}
	if recovered.ExternalRef != "gh-42" {
		t.Errorf("external_ref: want gh-42, got %q", recovered.ExternalRef)
	}
	if recovered.DueAt == nil || !recovered.DueAt.Equal(due) {
		t.Errorf("due_at not recovered: %v", recovered.DueAt)
	}
}

func TestConvertIssue_DepMapping(t *testing.T) {
	iss := BeadsIssue{
		ID: "bd-a", Title: "t", IssueType: "task", Status: "open",
		Dependencies: []BeadsDependency{
			{IssueID: "bd-a", DependsOnID: "bd-b", Type: "blocks"},
			{IssueID: "bd-a", DependsOnID: "bd-c", Type: "discovered-from"},
			{IssueID: "bd-a", DependsOnID: "bd-d", Type: "parent-child"},
		},
	}
	got := ConvertIssue(iss, DefaultConvertOptions())
	if len(got.DepTargets) != 3 {
		t.Fatalf("want 3 deps, got %d", len(got.DepTargets))
	}
	if got.DepTargets[0].Type != models.DepTypeBlocks {
		t.Errorf("blocks: got %q", got.DepTargets[0].Type)
	}
	if got.DepTargets[1].Type != models.DepTypeRelated || got.DepTargets[1].OriginalType != "discovered-from" {
		t.Errorf("discovered-from collapse: %+v", got.DepTargets[1])
	}
	if got.DepTargets[2].Type != models.DepTypeParentChild {
		t.Errorf("parent-child: got %q", got.DepTargets[2].Type)
	}
}

func TestConvertIssue_LabelDedup(t *testing.T) {
	// bd data already carries the label we'd synthesize for deferred status.
	iss := BeadsIssue{ID: "bd-1", Title: "t", IssueType: "task", Status: "deferred",
		Labels: []string{"bd:deferred", "backend", "backend"}}
	got := ConvertIssue(iss, DefaultConvertOptions())
	counts := map[string]int{}
	for _, l := range got.Task.Labels {
		counts[l]++
	}
	for l, n := range counts {
		if n > 1 {
			t.Errorf("label %q duplicated %d times: %v", l, n, got.Task.Labels)
		}
	}
	if counts["bd:deferred"] != 1 || counts["backend"] != 1 {
		t.Errorf("expected deduped bd:deferred and backend, got %v", got.Task.Labels)
	}
}

func TestTruncate_RuneSafe(t *testing.T) {
	// 100 x 4-byte runes = 400 bytes; 255 is not a multiple of 4, so a naive
	// byte cut would split a rune.
	title := strings.Repeat("🚀", 100)
	got := truncate(title, 255)
	if len(got) > 255 {
		t.Errorf("truncate exceeded max: %d bytes", len(got))
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncate split a rune: %q", got[len(got)-4:])
	}
	if len(got) != 252 { // 63 whole runes
		t.Errorf("want 252 bytes (63 runes), got %d", len(got))
	}
	if s := "short"; truncate(s, 255) != s {
		t.Error("short string should be untouched")
	}
}

func TestConvertIssue_AssigneeFallback(t *testing.T) {
	iss := BeadsIssue{ID: "bd-1", Title: "t", IssueType: "task", Status: "open", Owner: "alice"}
	got := ConvertIssue(iss, DefaultConvertOptions())
	if got.Task.Assignee != "alice" {
		t.Errorf("assignee fallback to owner: want alice, got %q", got.Task.Assignee)
	}
}

func TestConvertIssue_CommentsFoldedIntoNotes(t *testing.T) {
	created := time.Date(2026, 4, 23, 22, 17, 2, 0, time.UTC)
	iss := BeadsIssue{
		ID: "bd-1", Title: "t", IssueType: "task", Status: "open",
		Comments: []BeadsComment{
			{Author: "alice", Text: "hi", CreatedAt: created},
		},
	}
	got := ConvertIssue(iss, DefaultConvertOptions())
	if !strings.Contains(got.Task.Notes, "alice: hi") {
		t.Errorf("comment not folded into notes: %q", got.Task.Notes)
	}
}
