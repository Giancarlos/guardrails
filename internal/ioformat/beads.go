// Package ioformat handles import/export between guardrails and external formats
// (currently beads 1.0.x JSONL).
package ioformat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// BeadsIssue mirrors one line of a `bd export` JSONL file.
// Fields with no guardrails analogue are kept so the raw payload can be
// preserved in the notes trailer.
type BeadsIssue struct {
	ID                 string            `json:"id"`
	Title              string            `json:"title"`
	Description        string            `json:"description,omitempty"`
	Design             string            `json:"design,omitempty"`
	AcceptanceCriteria string            `json:"acceptance_criteria,omitempty"`
	Notes              string            `json:"notes,omitempty"`
	Status             string            `json:"status"`
	Priority           int               `json:"priority"`
	IssueType          string            `json:"issue_type"`
	Assignee           string            `json:"assignee,omitempty"`
	Owner              string            `json:"owner,omitempty"`
	EstimatedMinutes   int               `json:"estimated_minutes,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	CreatedBy          string            `json:"created_by,omitempty"`
	UpdatedAt          time.Time         `json:"updated_at"`
	ClosedAt           *time.Time        `json:"closed_at,omitempty"`
	DueAt              *time.Time        `json:"due_at,omitempty"`
	DeferUntil         *time.Time        `json:"defer_until,omitempty"`
	CloseReason        string            `json:"close_reason,omitempty"`
	ExternalRef        string            `json:"external_ref,omitempty"`
	Labels             []string          `json:"labels,omitempty"`
	Comments           []BeadsComment    `json:"comments,omitempty"`
	Dependencies       []BeadsDependency `json:"dependencies,omitempty"`

	// Discriminator for non-issue records. If non-empty, this line is not a
	// regular issue (memory, agent, rig, role, message, ...) and should be
	// skipped.
	RecordType string `json:"_type,omitempty"`
}

// BeadsComment is one entry in an issue's comments array.
type BeadsComment struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// BeadsDependency is one entry in an issue's dependencies array. The owning
// issue is the dependent (child); `DependsOnID` is the blocker (parent).
type BeadsDependency struct {
	IssueID     string `json:"issue_id"`
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"`
	CreatedBy   string `json:"created_by,omitempty"`
}

// maxLineErrors caps how many per-line error details are retained;
// InvalidLines always carries the true count.
const maxLineErrors = 100

// DecodeResult summarizes what was parsed.
type DecodeResult struct {
	Issues          []BeadsIssue
	MemoriesSkipped int
	InfraSkipped    int
	InvalidLines    int
	// LineErrors describes lines that failed to parse ("line N: <err>"),
	// capped at maxLineErrors entries.
	LineErrors []string
}

func (r *DecodeResult) addLineError(lineNo int, msg string) {
	r.InvalidLines++
	if len(r.LineErrors) < maxLineErrors {
		r.LineErrors = append(r.LineErrors, fmt.Sprintf("line %d: %s", lineNo, msg))
	}
}

// coreIssueTypes is the set of issue_type values we accept as tasks.
// Anything else (agents/rigs/etc. have different issue_types, memories have
// _type="memory") is treated as infrastructure and skipped.
var coreIssueTypes = map[string]bool{
	"task":      true,
	"bug":       true,
	"feature":   true,
	"epic":      true,
	"chore":     true,
	"decision":  true,
	"spike":     true,
	"story":     true,
	"milestone": true,
}

// DecodeBeadsJSONL reads a bd JSONL stream and returns the parsed issues
// along with skip counts. Parse errors on individual lines are soft: they are
// counted in DecodeResult.InvalidLines (with details in LineErrors) and
// decoding continues. Only stream-level read failures return an error.
func DecodeBeadsJSONL(r io.Reader) (*DecodeResult, error) {
	res := &DecodeResult{}
	sc := bufio.NewScanner(r)
	// bd descriptions / notes can be large; raise the line limit to 4 MiB.
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)

	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Bytes()
		if lineNo == 1 {
			raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")) // UTF-8 BOM
		}
		if len(raw) == 0 {
			continue
		}

		var issue BeadsIssue
		if err := json.Unmarshal(raw, &issue); err != nil {
			res.addLineError(lineNo, err.Error())
			continue
		}

		if issue.RecordType != "" {
			if issue.RecordType == "memory" {
				res.MemoriesSkipped++
			} else {
				res.InfraSkipped++
			}
			continue
		}

		if !coreIssueTypes[issue.IssueType] {
			res.InfraSkipped++
			continue
		}

		// An empty id would make every such record upsert onto the same
		// source_id="" row, silently merging distinct records.
		if issue.ID == "" {
			res.addLineError(lineNo, "missing issue id")
			continue
		}

		res.Issues = append(res.Issues, issue)
	}
	if err := sc.Err(); err != nil {
		return res, fmt.Errorf("scan: %w", err)
	}
	return res, nil
}
