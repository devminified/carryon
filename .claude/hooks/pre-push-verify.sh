#!/usr/bin/env bash
# PreToolUse hook (matcher: Bash). Fires on every Bash call; no-ops immediately
# unless the command actually contains a `git push`. When it does, it runs the
# mechanical subset of .claude/rules/ that CAN be checked by a script before
# letting the push through: secret scanning (redaction.md / I5), commit
# hygiene (git-workflow.md), and whichever quality gates (go/web/supabase)
# are already scaffolded in this repo. Everything else in .claude/rules/ is
# architectural/judgment-based and stays a human-and-Claude review job.
set -euo pipefail

INPUT="$(cat)"
COMMAND="$(printf '%s' "$INPUT" | jq -r '.tool_input.command // ""')"

allow() {
  local note="${1:-}"
  jq -n --arg note "$note" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "allow",
      permissionDecisionReason: $note
    }
  }'
  exit 0
}

deny() {
  local reason="$1"
  jq -n --arg reason "$reason" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: $reason
    }
  }'
  exit 0
}

# Only act on commands that actually invoke `git push` (covers chained
# commands like `git add . && git push`, not just a bare `git push`).
if ! printf '%s' "$COMMAND" | grep -Eq '(^|[;&|]|\s)git[[:space:]]+push([[:space:]]|$)'; then
  exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-.}"

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  allow "Not a git repo yet — nothing to verify."
fi

FAILURES=()
SKIPPED=()

# Range of commits about to be pushed: prefer the configured push target,
# fall back to the upstream, fall back to just the latest commit.
RANGE=""
if git rev-parse '@{push}' >/dev/null 2>&1; then
  RANGE='@{push}..HEAD'
elif git rev-parse '@{u}' >/dev/null 2>&1; then
  RANGE='@{u}..HEAD'
fi

if [ -n "$RANGE" ] && [ -n "$(git rev-list "$RANGE" 2>/dev/null)" ]; then
  COMMIT_RANGE="$RANGE"
else
  COMMIT_RANGE="HEAD~1..HEAD"
  SKIPPED+=("no push target configured — only checked the latest commit, not the full range")
fi

# 1. Commit hygiene: no AI/tool attribution (git-workflow.md hard rule)
while IFS= read -r sha; do
  [ -z "$sha" ] && continue
  body="$(git log -1 --format=%B "$sha")"
  if printf '%s' "$body" | grep -Eiq 'co-authored-by:[[:space:]]*claude|generated with \[claude|claude code|claude\.ai|cursor|copilot'; then
    FAILURES+=("commit ${sha:0:9} contains AI/tool attribution (git-workflow.md: \"No AI/tool attribution in commits\")")
  fi
done < <(git rev-list "$COMMIT_RANGE" 2>/dev/null || true)

# 2. Conventional Commits via commitlint, if configured
if [ -f .commitlintrc.json ] && command -v pnpm >/dev/null 2>&1 && [ -f package.json ]; then
  while IFS= read -r sha; do
    [ -z "$sha" ] && continue
    if ! git log -1 --format=%B "$sha" | pnpm commitlint >/dev/null 2>&1; then
      FAILURES+=("commit ${sha:0:9} fails commitlint (tooling.md Conventional Commits)")
    fi
  done < <(git rev-list "$COMMIT_RANGE" 2>/dev/null || true)
else
  SKIPPED+=("commitlint not set up yet")
fi

# 3. Secret scan over the diff about to be pushed (redaction.md / I5, NFR-07)
if command -v gitleaks >/dev/null 2>&1; then
  if ! git diff "$COMMIT_RANGE" 2>/dev/null | gitleaks detect --no-git --pipe -v >/tmp/carryon-gitleaks.$$ 2>&1; then
    FAILURES+=("gitleaks found a likely secret in the diff being pushed — see redaction.md (I5)")
  fi
  rm -f /tmp/carryon-gitleaks.$$
else
  # Best-effort fallback when gitleaks isn't installed: a handful of
  # high-confidence patterns. Not a substitute for real redaction tests.
  PATTERN='(AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|xox[baprs]-[0-9A-Za-z-]{10,}|ghp_[0-9A-Za-z]{30,}|sk-[A-Za-z0-9]{20,})'
  if git diff "$COMMIT_RANGE" 2>/dev/null | grep -Eqo "$PATTERN"; then
    FAILURES+=("basic secret pattern matched in the diff being pushed — install gitleaks for full coverage (redaction.md)")
  fi
  SKIPPED+=("gitleaks not installed — only ran a basic pattern fallback, not the real rule set")
fi

# 4. Go quality gates, if the module exists (go-conventions.md / testing.md)
if [ -f go.mod ]; then
  if command -v golangci-lint >/dev/null 2>&1; then
    golangci-lint run || FAILURES+=("golangci-lint failed")
  else
    SKIPPED+=("golangci-lint not installed")
  fi
  go vet ./... || FAILURES+=("go vet failed")
  go test -race ./... || FAILURES+=("go test -race ./... failed")
else
  SKIPPED+=("Go module not scaffolded yet")
fi

# 5. Web quality gates, if web/ is scaffolded (tooling.md)
if [ -f web/package.json ]; then
  (cd web && pnpm lint && pnpm typecheck && pnpm test) \
    || FAILURES+=("web/ lint, typecheck, or test failed")
else
  SKIPPED+=("web/ not scaffolded yet")
fi

# 6. Supabase migration lint, if migrations + CLI exist (database-rls.md)
if [ -d supabase/migrations ] && command -v supabase >/dev/null 2>&1; then
  supabase db lint || FAILURES+=("supabase db lint failed")
else
  SKIPPED+=("supabase CLI or migrations not set up yet")
fi

if [ "${#FAILURES[@]}" -gt 0 ]; then
  REASON="Blocked by pre-push verification against .claude/rules/:
$(printf '  - %s\n' "${FAILURES[@]}")"
  deny "$REASON"
fi

NOTE=""
if [ "${#SKIPPED[@]}" -gt 0 ]; then
  NOTE="Pre-push checks passed. Skipped (not yet applicable): $(printf '%s; ' "${SKIPPED[@]}")"
fi
allow "$NOTE"
