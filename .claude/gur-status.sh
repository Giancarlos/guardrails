#!/bin/bash
# Status line script for Claude Code — sleek format with progress bar + emoji
# Requires: gur on PATH (go install ./cmd/gur) and jq. Missing tools fall back to defaults.
input=$(cat)

GUR="${GUR:-gur}"

# Parse Claude usage from stdin JSON
CONTEXT=$(echo "$input" | jq -r '.context_window.used_percentage // 0' 2>/dev/null | cut -d. -f1)
COST=$(echo "$input" | jq -r '.cost.total_cost_usd // 0' 2>/dev/null)
MODEL=$(echo "$input" | jq -r '.model.display_name // "?"' 2>/dev/null)

# Fall back to safe defaults if jq failed or fields were missing
[[ "$CONTEXT" =~ ^[0-9]+$ ]] || CONTEXT=0
[ "$CONTEXT" -gt 100 ] && CONTEXT=100
[ -n "$MODEL" ] || MODEL="?"

# Format cost
[[ "$COST" =~ ^[0-9]+(\.[0-9]+)?$ ]] || COST=0
COST_FMT=$(printf '$%.2f' "$COST")

# Build mini progress bar (8 chars wide)
FILLED=$(( CONTEXT * 8 / 100 ))
BAR=""
for ((i=0; i<8; i++)); do
    if [ $i -lt $FILLED ]; then
        BAR="${BAR}█"
    else
        BAR="${BAR}░"
    fi
done

# Count tasks with a given status using JSON output (independent of text/compact formatting)
task_json() {
    $GUR list --status "$1" --json 2>/dev/null
}

TASKS=$(task_json in_progress)
COUNT=$(echo "$TASKS" | jq -r '.count // 0' 2>/dev/null)
[[ "$COUNT" =~ ^[0-9]+$ ]] || COUNT=0

if [ "$COUNT" -gt 0 ]; then
    FIRST=$(echo "$TASKS" | jq -r '.tasks[0].title // ""' 2>/dev/null | cut -c1-35)
    if [ "$COUNT" -eq 1 ]; then
        TASK_PART="🔨 $FIRST"
    else
        TASK_PART="🔨 $FIRST (+$((COUNT - 1)))"
    fi
else
    OPEN_COUNT=$(task_json open | jq -r '.count // 0' 2>/dev/null)
    [[ "$OPEN_COUNT" =~ ^[0-9]+$ ]] || OPEN_COUNT=0
    TASK_PART="📋 ${OPEN_COUNT} open"
fi

echo "🧠 $MODEL · ${BAR} ${CONTEXT}% · 💰 ${COST_FMT} · $TASK_PART"
