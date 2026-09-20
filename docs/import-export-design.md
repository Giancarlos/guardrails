# `gur import` / `gur export` — Design Notes

Branch: `feat/import-export`. Status: implemented (see `cmd/import.go`,
`cmd/export.go`, `internal/ioformat/`); this doc records the design decisions
and field mappings. Deviations from the original plan are noted inline.

## 1. Goal

- `gur import`: ingest a beads 1.0.x JSONL export into the local guardrails DB.
- `gur export`: dump the local guardrails DB to JSONL so it round-trips (and can also feed a beads `bd import`-style flow if one exists).

Out of scope for v1: memories, gates, skills/agents, history, GitHub links, templates. Tasks + dependencies + comments-as-notes only. Everything else can be phased in — see §6.

## 2. Beads 1.0.x export shape (verified with `bd 1.0.2 export --all`)

One JSON object per line. Issue records and memory records are mixed; memory lines carry `"_type":"memory"` and should be skipped in v1.

Issue fields observed:

| Field | Type | Notes |
|---|---|---|
| `id` | string | e.g. `bd-probe-keb`. Prefix varies per project. |
| `title` | string | |
| `description` | string | May embed `## Context` section when `--context` was used on create. |
| `design` | string | Separate field. |
| `acceptance_criteria` | string | Separate field. |
| `notes` | string | |
| `status` | string | `open`, `in_progress`, `blocked`, `deferred`, `closed`, `pinned`, `hooked` |
| `priority` | int | 0–4, 0 = highest |
| `issue_type` | string | `task`, `bug`, `feature`, `chore`, `epic`, `decision`, `spike`, `story`, `milestone` (+ custom) |
| `assignee` | string | |
| `owner` | string | Often email; separate from `assignee`. |
| `estimated_minutes` | int | |
| `created_at`, `updated_at`, `closed_at`, `due_at`, `defer_until` | RFC3339 | |
| `created_by` | string | Author name. |
| `close_reason` | string | Present only when closed. |
| `external_ref` | string | e.g. `gh-42`, `jira-ABC`. |
| `labels` | []string | |
| `comments` | []{id,issue_id,author,text,created_at} | |
| `dependencies` | []{issue_id,depends_on_id,type,created_at,created_by,metadata} | Only appears on the *dependent* side. |
| `dependency_count`, `dependent_count`, `comment_count` | int | Derivable; ignore. |

Dependency `type` vocabulary: `blocks`, `tracks`, `related`, `parent-child`, `discovered-from`, `until`, `caused-by`, `validates`, `relates-to`, `supersedes`.

## 3. Guardrails target shape (from `internal/models`)

`Task` fields that matter here: `ID`, `ParentID`, `Title`, `Description`, `Status` (`open|in_progress|closed|archived`), `Priority` (0–4), `Type` (`task|bug|feature|epic`), `Labels`, `Assignee`, `Notes`, `CloseReason`, `Source` (`local|github`), `CreatedAt`, `UpdatedAt`, `ClosedAt`.

`Dependency`: `ParentID` (blocker) → `ChildID` (blocked), `Type` (`blocks|related|parent-child`).

Task IDs must match `^gur-[a-f0-9]{8}(\.\d+)*$` (`internal/models/task.go:59`).

## 4. Field mapping — `bd → gur` (import)

### Task

| bd | gur | Notes |
|---|---|---|
| `id` (`bd-probe-keb`) | `tasks.id` (`gur-<8 hex>`) | **IDs don't match.** We can't reuse bd's `bd-<prefix>-<slug>` form — `ValidateTaskID` would reject it. Solution: generate a fresh `gur-` ID and keep the bd ID in a `source_id` column (see §5). Maintain an in-memory map `bd_id → gur_id` for dependency rewrites. |
| `title` | `title` | Truncate to 255 with warning. |
| `description` | `description` | Concat `design` and `acceptance_criteria` as `## Design` / `## Acceptance Criteria` sections so nothing is lost (gur has no separate columns). |
| `notes` | `notes` | |
| `status` | `status` | `open`→`open`; `in_progress`→`in_progress`; `closed`→`closed`; `blocked`→`open` (gur has no blocked; rely on deps); `deferred`→`open` + label `deferred`; `pinned`→`open` + label `pinned`; `hooked`→`in_progress` + label `hooked`. |
| `priority` | `priority` | 1:1. |
| `issue_type` | `type` | `task/bug/feature/epic` pass through. `chore/decision/spike/story/milestone/<custom>` → `task` + label with the original type (loss-preserving). |
| `assignee` | `assignee` | Prefer `assignee`, fall back to `owner`. |
| `owner` | *(label `owner:<value>` when it differs from assignee)* | Optional. |
| `estimated_minutes`, `due_at`, `defer_until`, `external_ref` | No native slot | Stash in notes as a `<!-- bd: {...} -->` trailer, *or* in new optional columns if we're willing to migrate. See §5. |
| `labels` | `labels` | Plus any synthesized ones above. |
| `close_reason` | `close_reason` | |
| `created_at`, `updated_at`, `closed_at` | same | Need to bypass GORM auto-timestamps on insert — use `Session{SkipHooks:true}` and explicit column writes, or raw insert. |
| `comments[]` | appended to `notes` | Format: `[<created_at>] <author>: <text>\n`. Matches existing `AppendNotes` style. |

Source: set to a new constant `SourceBeads = "beads"` (already have `local`, `github`). That lets `gur export` later decide whether to preserve the original `bd-` id.

### Dependency

| bd type | gur type |
|---|---|
| `blocks` | `blocks` |
| `parent-child` | `parent-child` (v1 creates the dependency row only; `tasks.parent_id` is *not* set on import, so bd hierarchies don't render as gur subtasks — revisit if needed) |
| `related`, `relates-to` | `related` |
| `tracks`, `discovered-from`, `until`, `caused-by`, `validates`, `supersedes` | `related` + keep original in a comment/note (v1). Proper mapping waits on dep-type expansion in `models/dependency.go`. |

Dependency rows use the bd-id → gur-id map built during pass 1. Anything pointing to an ID outside the file becomes a soft error (log + skip).

## 5. Schema change proposal

Minimal migration to preserve round-tripping:

```go
// models/task.go
SourceID  string `gorm:"size:64;uniqueIndex;default:null" json:"source_id,omitempty"` // e.g. "bd-probe-xtw"
// Already have Source — set to SourceBeads on import.
```

`source_id` is `uniqueIndex` with SQL `NULL` as the empty value so locally-created tasks (no `source_id`) don't collide with each other. Reimport of the same bd file upserts by `source_id`, giving idempotent import.

Optional (only if we want full fidelity, recommend deferring):
- `EstimatedMinutes *int`
- `DueAt *time.Time`
- `DeferUntil *time.Time`
- `ExternalRef string`

For v1, punt the optional four into a single `metadata` JSON column or the notes trailer to avoid schema churn. Recommendation: **one new column (`source_id`) + notes trailer for the rest**. Revisit once we see real usage.

## 6. CLI surface

### `gur import`

```
gur import <file> [flags]
  --format beads-jsonl        (default; only supported format in v1)
  --dry-run                   Parse + validate, print summary, no writes
  --on-conflict update|skip|error   (default update; match by source_id — idempotent reimport)
  --no-comments               Don't fold comments into notes
  --map-type <bd>=<gur>       Repeatable, override default type mapping (target validated)
  --label-prefix <str>        Prefix synthesized labels (default: "bd:")
```

Behavior:
1. Two passes. Pass 1: parse all lines, build `bd_id → gur_id` map, validate.
2. Report summary (`N tasks, M deps, K comments → notes, X skipped memories, Y unknown dep targets`).
3. Pass 2 (unless `--dry-run`): single GORM transaction, insert tasks then deps.
4. Exit nonzero on any hard error; soft errors (unknown deps, truncated titles) go to stderr but don't fail.

### `gur export`

```
gur export [flags]
  --format beads-jsonl|gur-jsonl   (default: gur-jsonl — native schema, lossless)
  -o, --output <path>              (default: stdout)
  --all                            Include archived
  --include-closed                 (default: true)
  --exclude-labels <l1,l2>
```

`gur-jsonl` is literally the JSON tags already on `models.Task` + a `dependencies` array per task, mirroring bd's shape so a future `gur import --format gur-jsonl` is trivial.

`beads-jsonl` emits records shaped like §2 so they can feed `bd import` / external tooling. Lossy on: gur `archived` status (emit as `closed` + label `archived`), hierarchical subtask IDs (flatten to parent-child dep + bd-compatible id).

## 7. Code layout

```
cmd/
  import.go               # cobra command + flag plumbing
  export.go
  import_export_test.go   # round-trip / idempotency tests against the DB
internal/
  ioformat/
    beads.go         # BeadsIssue struct, decoder
    beads_test.go
    gur.go           # native JSONL encoder + beads encoder
    gur_test.go
    mapping.go       # status/type/dep mapping tables (easy to unit test)
    mapping_test.go
testdata/
  beads/
    full_sample.jsonl   # verified bd 1.0.2 export (3 issues, 1 dep, 1 memory)
```

Adversarial cases (cycles, self-deps, hostile trailers, BOM, oversized lines,
empty ids) are covered by inline fixtures in the unit tests rather than
checked-in files.

Keep cmd/*.go thin — parse flags, open DB, call into `internal/ioformat`. Mirrors how `cmd/sync.go` delegates to helpers.

## 8. Decisions

1. **bd is treated as an external, unmodifiable upstream.** guardrails owns its own copy of the data; no write path back to bd in v1.
2. **Memories are dropped.** Log each one at import time and print a prominent end-of-run warning: `WARNING: dropped N beads memories (guardrails has no memory store). Lines: ...`.
3. **Infrastructure records are dropped with the same warning treatment.** Any line with `_type` set, or `issue_type` outside `{task,bug,feature,epic,chore,decision,spike,story,milestone}`, is skipped and counted. Printed at end of run: `WARNING: skipped N infrastructure/unknown records (agents, rigs, roles, messages, memories).`
4. **Import is idempotent by `source_id`.** Unique index on `source_id`; default `--on-conflict=update` upserts on reimport of the same bd file. Locally-created gur tasks (no `source_id`) are untouched.

## 9. Suggested implementation order

1. PR 1: add `SourceID`, `SourceBeads` constant, migration. ~30 LOC.
2. PR 2: `internal/ioformat/beads.go` decoder + mapping tables + tests. ~250 LOC.
3. PR 3: `cmd/import.go` wired to the package. ~150 LOC + fixtures.
4. PR 4: `cmd/export.go` (gur-jsonl first, beads-jsonl second). ~200 LOC.
5. PR 5: round-trip test (`bd export` → `gur import` → `gur export --format beads-jsonl` → diff).
