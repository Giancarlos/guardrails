package ioformat

import (
	"strings"
	"testing"
)

const sampleJSONL = `{"id":"bd-probe-xtw","title":"sample task","description":"example desc","status":"closed","priority":1,"issue_type":"feature","owner":"user@example.com","created_at":"2026-04-23T22:16:50Z","updated_at":"2026-04-23T22:17:36Z","closed_at":"2026-04-23T22:17:36Z","close_reason":"done","labels":["backend","urgent"],"comments":[{"id":"c1","issue_id":"bd-probe-xtw","author":"Alice","text":"a comment","created_at":"2026-04-23T22:17:02Z"}]}
{"id":"bd-probe-7jq","title":"second","description":"dep child","status":"open","priority":2,"issue_type":"task","owner":"user@example.com","created_at":"2026-04-23T22:16:51Z","updated_at":"2026-04-23T22:16:51Z","dependencies":[{"issue_id":"bd-probe-7jq","depends_on_id":"bd-probe-xtw","type":"blocks"}]}
{"_type":"memory","key":"some-persistent-memory","value":"some persistent memory"}
{"id":"bd-probe-agt","title":"agent bead","status":"open","priority":2,"issue_type":"agent"}
`

func TestDecodeBeadsJSONL(t *testing.T) {
	res, err := DecodeBeadsJSONL(strings.NewReader(sampleJSONL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(res.Issues))
	}
	if res.MemoriesSkipped != 1 {
		t.Errorf("MemoriesSkipped: want 1, got %d", res.MemoriesSkipped)
	}
	if res.InfraSkipped != 1 {
		t.Errorf("InfraSkipped: want 1, got %d", res.InfraSkipped)
	}
	if res.Issues[0].ID != "bd-probe-xtw" || res.Issues[0].Status != "closed" {
		t.Errorf("first issue mismatch: %+v", res.Issues[0])
	}
	if len(res.Issues[1].Dependencies) != 1 {
		t.Errorf("expected 1 dep on second issue, got %d", len(res.Issues[1].Dependencies))
	}
}

func TestDecodeBeadsJSONL_InvalidLine(t *testing.T) {
	in := sampleJSONL + "not json\n"
	res, err := DecodeBeadsJSONL(strings.NewReader(in))
	if err != nil {
		t.Fatalf("invalid lines are soft errors, got: %v", err)
	}
	if res.InvalidLines != 1 {
		t.Errorf("InvalidLines: want 1, got %d", res.InvalidLines)
	}
	if len(res.LineErrors) != 1 || !strings.HasPrefix(res.LineErrors[0], "line 5:") {
		t.Errorf("LineErrors: want one entry for line 5, got %v", res.LineErrors)
	}
	// Valid lines should still parse.
	if len(res.Issues) != 2 {
		t.Errorf("expected 2 issues despite bad line, got %d", len(res.Issues))
	}
}

func TestDecodeBeadsJSONL_EmptyIDSkipped(t *testing.T) {
	in := `{"id":"","title":"no id","status":"open","issue_type":"task"}` + "\n" +
		`{"id":"bd-ok","title":"ok","status":"open","issue_type":"task"}` + "\n"
	res, err := DecodeBeadsJSONL(strings.NewReader(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Issues) != 1 || res.Issues[0].ID != "bd-ok" {
		t.Errorf("want only bd-ok, got %+v", res.Issues)
	}
	if res.InvalidLines != 1 || len(res.LineErrors) != 1 || !strings.Contains(res.LineErrors[0], "missing issue id") {
		t.Errorf("empty id should be a counted line error, got invalid=%d errs=%v", res.InvalidLines, res.LineErrors)
	}
}

func TestDecodeBeadsJSONL_LineErrorsCapped(t *testing.T) {
	in := strings.Repeat("garbage\n", maxLineErrors+50)
	res, err := DecodeBeadsJSONL(strings.NewReader(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.InvalidLines != maxLineErrors+50 {
		t.Errorf("InvalidLines should carry true count: want %d, got %d", maxLineErrors+50, res.InvalidLines)
	}
	if len(res.LineErrors) != maxLineErrors {
		t.Errorf("LineErrors should cap at %d, got %d", maxLineErrors, len(res.LineErrors))
	}
}

func TestDecodeBeadsJSONL_BOMStripped(t *testing.T) {
	in := "\xef\xbb\xbf" + `{"id":"bd-bom","title":"bom","status":"open","issue_type":"task"}` + "\n"
	res, err := DecodeBeadsJSONL(strings.NewReader(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Issues) != 1 || res.InvalidLines != 0 {
		t.Errorf("BOM line should parse: issues=%d invalid=%d errs=%v", len(res.Issues), res.InvalidLines, res.LineErrors)
	}
}
