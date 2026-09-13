# Project Instructions

This is the **guardrails** project, which contains the `gur` CLI tool for task management.

## Task Tracking

**Always use `gur` for task tracking. NEVER use the built-in TaskCreate, TaskUpdate, TaskList, or TaskGet tools.**

The `gur` CLI is specifically designed for this project and provides:
- Persistent task storage in a SQLite database
- Hierarchical subtasks, dependencies, and quality gates
- GitHub Issues sync
- Skills and agents linking
- Checkpoint/resume for long-running tasks
- Agent handoff protocol

### Quick Reference

```bash
# List open tasks
./gur list --status open

# List in-progress tasks
./gur list --status in_progress

# Show task details
./gur show <task-id>

# Create a new task
./gur create "Task title" --type task --priority 2

# Update task status
./gur update <task-id> --status in_progress

# Close a task (requires all linked gates to pass first)
./gur close <task-id> --reason "Reason for closing"

# Find tasks ready to work on (no blockers)
./gur ready
```

### Quality Gates

**Every task must have at least one gate linked and passed before it can be closed.** Gates enforce quality requirements like tests passing, code review, or manual verification.

```bash
# Create a gate
./gur gate create "Unit tests pass" --type test
./gur gate create "Code review" --type review --category backend

# Link a gate to a task (must be done before closing)
./gur gate link <gate-id> <task-id>

# Mark a gate as passed for a task
./gur gate pass <gate-id> <task-id> --notes "All tests green"
./gur gate pass <gate-id> <task-id> --by agent

# Mark as failed or skipped
./gur gate fail <gate-id> <task-id>
./gur gate skip <gate-id> <task-id>

# List all gates
./gur gate list

# Show gate details
./gur gate show <gate-id>
```

Gate types are free-form but common ones: `test`, `review`, `approval`, `manual`, `deploy`, `qa`, `security`, `doc`.

Each gate must be verified **per-task** — a global pass doesn't count. The `--force` flag on close requires interactive confirmation and cannot be used from scripts/agents.

### Workflow

1. Before starting work, run `./gur list --status open` or `./gur ready` to see available tasks
2. When starting a task, update its status: `./gur update <id> --status in_progress`
3. Add notes as you work: `./gur update <id> --notes "Found the issue in..."`
4. Before closing, link and pass at least one gate
5. Close with a reason: `./gur close <id> --reason "Fixed by implementing..."`

### Full Documentation

See `skills/gur-workflow/SKILL.md` for complete documentation on:
- Task lifecycle and priorities
- Subtasks and dependencies
- Quality gates
- GitHub sync
- Best practices
