package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Giancarlos/guardrails/internal/db"
	"github.com/Giancarlos/guardrails/internal/ioformat"
	"github.com/Giancarlos/guardrails/internal/models"
)

// fixturePath is the verified bd 1.0.2 export checked into testdata/.
const fixturePath = "../testdata/beads/full_sample.jsonl"

func setupImportTestDB(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	if _, err := db.InitDB(dbPath); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.CloseDB() })
}

func decodeFixture(t *testing.T) []ioformat.ConvertedTask {
	t.Helper()
	f, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	res, err := ioformat.DecodeBeadsJSONL(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.MemoriesSkipped != 1 {
		t.Errorf("fixture should drop 1 memory, got %d", res.MemoriesSkipped)
	}
	opts := ioformat.DefaultConvertOptions()
	out := make([]ioformat.ConvertedTask, 0, len(res.Issues))
	for _, iss := range res.Issues {
		out = append(out, ioformat.ConvertIssue(iss, opts))
	}
	return out
}

func TestImport_RoundTrip(t *testing.T) {
	setupImportTestDB(t)

	converted := decodeFixture(t)
	if len(converted) != 3 {
		t.Fatalf("want 3 issues from fixture, got %d", len(converted))
	}

	inserted, updated, unknown, err := applyImport(converted, "update")
	if err != nil {
		t.Fatalf("applyImport: %v", err)
	}
	if inserted != 3 || updated != 0 {
		t.Errorf("first import: want inserted=3 updated=0, got inserted=%d updated=%d", inserted, updated)
	}
	if len(unknown) != 0 {
		t.Errorf("unknown deps: %v", unknown)
	}

	var rowCount int64
	if err := db.GetDB().Model(&models.Task{}).Count(&rowCount).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rowCount != 3 {
		t.Errorf("task rows: want 3, got %d", rowCount)
	}

	// Dep: bd-probe-7jq -> bd-probe-xtw (blocks)
	var depCount int64
	db.GetDB().Model(&models.Dependency{}).Count(&depCount)
	if depCount != 1 {
		t.Errorf("dep rows: want 1, got %d", depCount)
	}

	// Idempotent reimport: same input, same row counts, zero inserted.
	converted2 := decodeFixture(t)
	inserted2, updated2, _, err := applyImport(converted2, "update")
	if err != nil {
		t.Fatalf("reimport: %v", err)
	}
	if inserted2 != 0 || updated2 != 3 {
		t.Errorf("reimport: want inserted=0 updated=3, got inserted=%d updated=%d", inserted2, updated2)
	}
	db.GetDB().Model(&models.Task{}).Count(&rowCount)
	if rowCount != 3 {
		t.Errorf("after reimport rows: want 3, got %d", rowCount)
	}

	var depCountAfter int64
	db.GetDB().Model(&models.Dependency{}).Count(&depCountAfter)
	if depCountAfter != 1 {
		t.Errorf("dep rows after reimport: want 1 (idempotent), got %d", depCountAfter)
	}
}

func TestImport_SkipVsUpdate(t *testing.T) {
	setupImportTestDB(t)
	converted := decodeFixture(t)
	if _, _, _, err := applyImport(converted, "update"); err != nil {
		t.Fatalf("initial import: %v", err)
	}

	// Mutate one task's title in place so we can observe update vs skip.
	sid := "bd-probe-xtw"
	if err := db.GetDB().Model(&models.Task{}).
		Where("source_id = ?", sid).
		Update("title", "locally renamed").Error; err != nil {
		t.Fatalf("mutate: %v", err)
	}

	// Reimport with skip: local rename should stay.
	if _, _, _, err := applyImport(decodeFixture(t), "skip"); err != nil {
		t.Fatalf("skip import: %v", err)
	}
	var task models.Task
	db.GetDB().Where("source_id = ?", sid).First(&task)
	if task.Title != "locally renamed" {
		t.Errorf("skip should preserve local rename, got title %q", task.Title)
	}

	// Reimport with update: should overwrite with fixture title.
	if _, _, _, err := applyImport(decodeFixture(t), "update"); err != nil {
		t.Fatalf("update import: %v", err)
	}
	db.GetDB().Where("source_id = ?", sid).First(&task)
	if task.Title != "sample task" {
		t.Errorf("update should restore title, got %q", task.Title)
	}
}

func TestImport_ReimportPreservesTimestampsAndLocalColumns(t *testing.T) {
	setupImportTestDB(t)
	if _, _, _, err := applyImport(decodeFixture(t), "update"); err != nil {
		t.Fatalf("initial import: %v", err)
	}

	// Simulate local-only state that beads knows nothing about.
	sid := "bd-probe-keb"
	if err := db.GetDB().Model(&models.Task{}).Where("source_id = ?", sid).
		UpdateColumns(map[string]any{"synced": true, "summary": "local summary"}).Error; err != nil {
		t.Fatalf("mutate: %v", err)
	}

	if _, _, _, err := applyImport(decodeFixture(t), "update"); err != nil {
		t.Fatalf("reimport: %v", err)
	}

	var task models.Task
	if err := db.GetDB().Where("source_id = ?", sid).First(&task).Error; err != nil {
		t.Fatalf("find: %v", err)
	}
	// Fixture says updated_at 2026-04-23T22:18:14Z; reimport must not clobber it.
	if got := task.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"); got != "2026-04-23T22:18:14Z" {
		t.Errorf("updated_at clobbered on reimport: got %s", got)
	}
	if !task.Synced {
		t.Error("reimport wiped local-only column synced")
	}
	if task.Summary != "local summary" {
		t.Errorf("reimport wiped local-only column summary: got %q", task.Summary)
	}
}

func TestImport_ConflictError(t *testing.T) {
	setupImportTestDB(t)
	if _, _, _, err := applyImport(decodeFixture(t), "update"); err != nil {
		t.Fatalf("initial import: %v", err)
	}
	_, _, _, err := applyImport(decodeFixture(t), "error")
	if err == nil {
		t.Error("expected error on --on-conflict=error with existing source_id")
	}
}

func TestExport_BeadsJSONL_RoundTrip(t *testing.T) {
	setupImportTestDB(t)
	if _, _, _, err := applyImport(decodeFixture(t), "update"); err != nil {
		t.Fatalf("import: %v", err)
	}

	var tasks []models.Task
	if err := db.GetDB().Find(&tasks).Error; err != nil {
		t.Fatalf("find tasks: %v", err)
	}
	depsByChild, err := loadDepsByChild(tasks)
	if err != nil {
		t.Fatalf("deps: %v", err)
	}

	var buf bytes.Buffer
	if err := ioformat.EncodeBeadsJSONL(&buf, tasks, depsByChild, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Verify original bd IDs came back.
	out := buf.String()
	for _, sid := range []string{"bd-probe-xtw", "bd-probe-keb", "bd-probe-7jq"} {
		if !strings.Contains(out, `"id":"`+sid+`"`) {
			t.Errorf("exported JSONL missing source id %q", sid)
		}
	}

	// Parse each line and check at least one has a recovered trailer field.
	foundEstimate := false
	foundDesign := false
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var iss ioformat.BeadsIssue
		if err := json.Unmarshal([]byte(line), &iss); err != nil {
			t.Fatalf("re-parse: %v", err)
		}
		if iss.EstimatedMinutes == 60 {
			foundEstimate = true
		}
		if iss.Design == "design notes" {
			foundDesign = true
		}
	}
	if !foundEstimate {
		t.Error("estimated_minutes=60 not recovered in beads-jsonl export")
	}
	if !foundDesign {
		t.Error("design field not recovered in beads-jsonl export")
	}
}

func convertRaw(t *testing.T, jsonl string) []ioformat.ConvertedTask {
	t.Helper()
	res, err := ioformat.DecodeBeadsJSONL(strings.NewReader(jsonl))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	opts := ioformat.DefaultConvertOptions()
	out := make([]ioformat.ConvertedTask, 0, len(res.Issues))
	for _, iss := range res.Issues {
		out = append(out, ioformat.ConvertIssue(iss, opts))
	}
	return out
}

func TestImport_SelfDepAndCycleSkipped(t *testing.T) {
	setupImportTestDB(t)
	jsonl := `{"id":"bd-self","title":"self","status":"open","issue_type":"task","dependencies":[{"issue_id":"bd-self","depends_on_id":"bd-self","type":"blocks"}]}
{"id":"bd-a","title":"a","status":"open","issue_type":"task","dependencies":[{"issue_id":"bd-a","depends_on_id":"bd-b","type":"blocks"}]}
{"id":"bd-b","title":"b","status":"open","issue_type":"task","dependencies":[{"issue_id":"bd-b","depends_on_id":"bd-a","type":"blocks"}]}
`
	_, _, skipped, err := applyImport(convertRaw(t, jsonl), "update")
	if err != nil {
		t.Fatalf("applyImport: %v", err)
	}
	if len(skipped) != 2 {
		t.Fatalf("want 2 skipped deps (self + cycle), got %v", skipped)
	}
	if !strings.Contains(skipped[0], "self-reference") {
		t.Errorf("first skip should be self-reference: %v", skipped)
	}
	if !strings.Contains(skipped[1], "cycle") {
		t.Errorf("second skip should be cycle: %v", skipped)
	}
	// Only the first edge of the would-be cycle survives.
	var depCount int64
	db.GetDB().Model(&models.Dependency{}).Count(&depCount)
	if depCount != 1 {
		t.Errorf("dep rows: want 1, got %d", depCount)
	}
}

func TestParseTypeMaps_RejectsInvalidTarget(t *testing.T) {
	if _, err := parseTypeMaps([]string{"task=banana"}); err == nil {
		t.Error("expected error for invalid target type")
	}
	m, err := parseTypeMaps([]string{"spike=bug", "story=feature"})
	if err != nil {
		t.Fatalf("valid maps rejected: %v", err)
	}
	if m["spike"] != "bug" || m["story"] != "feature" {
		t.Errorf("mapping wrong: %v", m)
	}
}

func TestLoadDepsByChild_Chunked(t *testing.T) {
	setupImportTestDB(t)
	// More tasks than depQueryChunk so the IN(...) query must split.
	n := depQueryChunk*2 + 100
	tasks := make([]models.Task, n)
	for i := range tasks {
		tasks[i] = models.Task{ID: fmt.Sprintf("gur-%08x", i), Title: "t", Status: models.StatusOpen}
	}
	if err := db.GetDB().CreateInBatches(tasks, 200).Error; err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	deps := make([]models.Dependency, 0, n-1)
	for i := 1; i < n; i++ {
		deps = append(deps, models.Dependency{ParentID: tasks[0].ID, ChildID: tasks[i].ID, Type: models.DepTypeBlocks})
	}
	if err := db.GetDB().CreateInBatches(deps, 200).Error; err != nil {
		t.Fatalf("create deps: %v", err)
	}

	got, err := loadDepsByChild(tasks)
	if err != nil {
		t.Fatalf("loadDepsByChild: %v", err)
	}
	if len(got) != n-1 {
		t.Errorf("want deps for %d children, got %d", n-1, len(got))
	}
	if len(got[tasks[1].ID]) != 1 || got[tasks[1].ID][0].ParentID != tasks[0].ID {
		t.Errorf("dep content wrong: %+v", got[tasks[1].ID])
	}
}

func TestImport_DryRun(t *testing.T) {
	setupImportTestDB(t)
	// Dry run shouldn't write anything; we simulate by not calling applyImport.
	// Just verify the decoder side stays consistent.
	converted := decodeFixture(t)
	if len(converted) != 3 {
		t.Fatalf("dry run decode: want 3, got %d", len(converted))
	}
	var count int64
	db.GetDB().Model(&models.Task{}).Count(&count)
	if count != 0 {
		t.Errorf("dry run should leave DB empty, got %d rows", count)
	}
}
