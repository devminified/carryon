#!/usr/bin/env bash
# CO-503: one matrix leg of the nightly Claude Code compatibility
# workflow. Runs entirely against an EPHEMERAL local Supabase instance
# (supabase start, same technique ci.yml's own `supabase`/`web-e2e`
# jobs already use) — that's this run's own "test backend," isolated
# per job, torn down with the runner. It never touches
# carryon-staging/carryon-prod except for the one final call to the
# real, deployed record-compat-run Edge Function, which is what the
# web app's own pinned-version gate actually reads.
#
# First real dispatch of this workflow is this script's actual test —
# every individual piece (claude CLI flags, the npm package name, the
# claudepath encoding, the ~/.carryon/credentials file-fallback
# mechanism) was verified directly against the real tools during this
# ticket's own development; the full sequence end to end has not been
# run for real yet, since that needs a real ANTHROPIC_API_KEY this
# environment doesn't have. Treat a failure on the first dispatch as
# this script needing a fix, not necessarily a real compatibility
# break.
set -euo pipefail

: "${CLAUDE_CODE_VERSION:?}" "${VERSION_LABEL:?}" "${OS_LABEL:?}" "${CARRYON_VERSION:?}"
: "${ANTHROPIC_API_KEY:?}"
: "${RECORD_RUN_URL:?}" "${COMPAT_CI_SECRET:?}"

WORKDIR="$(mktemp -d)"
FIXTURE_REPO="$WORKDIR/fixture-repo"
PULLED_REPO="$WORKDIR/pulled-repo"
MAGIC_FACT="the retry budget for payments-webhook is 7 attempts"
FAILURE_REASON=""
UNKNOWN_LINE_TYPES="[]"
PASSED=true

# Every session-file line "type" this codebase's own reader/viewer
# code already knows about (transcript-schema.ts / parse-transcript.ts)
# — a type outside this set is reported, never a failure on its own
# (the ticket's own explicit wording).
KNOWN_LINE_TYPES=("user" "assistant" "summary" "system")

record_run() {
  local run_url="${GITHUB_SERVER_URL:-}/${GITHUB_REPOSITORY:-}/actions/runs/${GITHUB_RUN_ID:-}"
  curl -sf -X POST "$RECORD_RUN_URL" \
    -H "x-ci-secret: $COMPAT_CI_SECRET" \
    -H "content-type: application/json" \
    -d "$(jq -n \
      --arg carryonVersion "$CARRYON_VERSION" \
      --arg claudeCodeVersion "$RESOLVED_CC_VERSION" \
      --arg versionLabel "$VERSION_LABEL" \
      --arg os "$OS_LABEL" \
      --argjson passed "$PASSED" \
      --arg failureReason "$FAILURE_REASON" \
      --argjson unknownLineTypes "$UNKNOWN_LINE_TYPES" \
      --arg runUrl "$run_url" \
      '{carryonVersion: $carryonVersion, claudeCodeVersion: $claudeCodeVersion, versionLabel: $versionLabel, os: $os, passed: $passed, failureReason: (if $failureReason == "" then null else $failureReason end), unknownLineTypes: $unknownLineTypes, runUrl: $runUrl}')" \
    >/dev/null
}

fail() {
  PASSED=false
  FAILURE_REASON="$1"
  echo "FAIL: $1" >&2
  record_run
  exit 1
}

echo "== installing Claude Code ($VERSION_LABEL: $CLAUDE_CODE_VERSION) =="
npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}"
RESOLVED_CC_VERSION="$(claude --version | awk '{print $1}')"
echo "resolved to $RESOLVED_CC_VERSION"

echo "== starting the ephemeral local test backend =="
supabase start
LOCAL_API_URL="$(supabase status -o env | grep '^API_URL=' | cut -d= -f2- | tr -d '"')"
LOCAL_ANON_KEY="$(supabase status -o env | grep '^ANON_KEY=' | cut -d= -f2- | tr -d '"')"
LOCAL_SERVICE_ROLE_KEY="$(supabase status -o env | grep '^SERVICE_ROLE_KEY=' | cut -d= -f2- | tr -d '"')"

echo "== building carryon =="
go build -o "$WORKDIR/bin/carryon" ./cmd/carryon
export PATH="$WORKDIR/bin:$PATH"

echo "== seeding a project and a first user via the service role =="
DEVELOPER_EMAIL="compat-ci-$(date +%s)@test.local"
DEVELOPER_ID="$(curl -sf -X POST "$LOCAL_API_URL/auth/v1/admin/users" \
  -H "apikey: $LOCAL_SERVICE_ROLE_KEY" -H "authorization: Bearer $LOCAL_SERVICE_ROLE_KEY" \
  -H "content-type: application/json" \
  -d "{\"email\": \"$DEVELOPER_EMAIL\", \"password\": \"compat-ci-password-123\", \"email_confirm\": true}" \
  | jq -r '.id')"

PROJECT_ID="$(uuidgen | tr 'A-Z' 'a-z')"
curl -sf -X POST "$LOCAL_API_URL/rest/v1/projects" \
  -H "apikey: $LOCAL_SERVICE_ROLE_KEY" -H "authorization: Bearer $LOCAL_SERVICE_ROLE_KEY" \
  -H "content-type: application/json" -H "Prefer: return=minimal" \
  -d "{\"id\": \"$PROJECT_ID\", \"name\": \"compat-ci\", \"git_remote\": \"example.com/compat-ci/$PROJECT_ID\"}"
curl -sf -X POST "$LOCAL_API_URL/rest/v1/project_members" \
  -H "apikey: $LOCAL_SERVICE_ROLE_KEY" -H "authorization: Bearer $LOCAL_SERVICE_ROLE_KEY" \
  -H "content-type: application/json" -H "Prefer: return=minimal" \
  -d "{\"project_id\": \"$PROJECT_ID\", \"user_id\": \"$DEVELOPER_ID\", \"role\": \"admin\"}"

cat > "$WORKDIR/config.toml" <<EOF
api_url = "$LOCAL_API_URL"
anon_key = "$LOCAL_ANON_KEY"
EOF
export HOME="$WORKDIR/home1"
mkdir -p "$HOME/.carryon"
cp "$WORKDIR/config.toml" "$HOME/.carryon/config.toml"

DEV_TOKENS="$(curl -sf -X POST "$LOCAL_API_URL/auth/v1/token?grant_type=password" \
  -H "apikey: $LOCAL_ANON_KEY" -H "content-type: application/json" \
  -d "{\"email\": \"$DEVELOPER_EMAIL\", \"password\": \"compat-ci-password-123\"}")"
echo "$DEV_TOKENS" | jq -r '.refresh_token' > "$HOME/.carryon/credentials"
chmod 600 "$HOME/.carryon/credentials"

echo "== running the scripted headless session =="
mkdir -p "$FIXTURE_REPO"
git -C "$FIXTURE_REPO" init -q
git -C "$FIXTURE_REPO" config user.email "ci@carryon.example"
git -C "$FIXTURE_REPO" config user.name "Carryon Compat CI"
echo "# compat-ci fixture" > "$FIXTURE_REPO/README.md"
git -C "$FIXTURE_REPO" add README.md
git -C "$FIXTURE_REPO" commit -q -m "initial commit"

carryon install
carryon agent &
AGENT_PID=$!
trap 'kill "$AGENT_PID" 2>/dev/null || true' EXIT

SESSION_ID="$(uuidgen | tr 'A-Z' 'a-z')"
(cd "$FIXTURE_REPO" && claude -p \
  --session-id "$SESSION_ID" \
  --model claude-haiku-4-5 \
  --output-format text \
  "Please remember this fact for later and just acknowledge it in one sentence: $MAGIC_FACT")

echo "== waiting for the session to upload =="
for _ in $(seq 1 30); do
  COUNT="$(curl -sf "$LOCAL_API_URL/rest/v1/sessions?id=eq.$SESSION_ID&select=id" \
    -H "apikey: $LOCAL_SERVICE_ROLE_KEY" -H "authorization: Bearer $LOCAL_SERVICE_ROLE_KEY" \
    | jq 'length')"
  [ "$COUNT" = "1" ] && break
  sleep 2
done
[ "$COUNT" = "1" ] || fail "session $SESSION_ID never reached the test backend"

echo "== checking the encoder folder name (CO-209) =="
EXPECTED_ENCODED="$(carryon debug encode-path "$FIXTURE_REPO")"
ACTUAL_FOLDER="$(ls "$HOME/.claude/projects" | grep -F "$EXPECTED_ENCODED" || true)"
[ -n "$ACTUAL_FOLDER" ] || fail "no ~/.claude/projects folder matched the expected encoding ($EXPECTED_ENCODED) for Claude Code $RESOLVED_CC_VERSION"

echo "== checking session-file line types =="
TRANSCRIPT_FILE="$HOME/.claude/projects/$ACTUAL_FOLDER/$SESSION_ID.jsonl"
SEEN_TYPES="$(jq -r '.type' "$TRANSCRIPT_FILE" 2>/dev/null | sort -u)"
NEW_TYPES=()
while IFS= read -r t; do
  [ -z "$t" ] && continue
  known=false
  for k in "${KNOWN_LINE_TYPES[@]}"; do
    [ "$t" = "$k" ] && known=true && break
  done
  $known || NEW_TYPES+=("$t")
done <<<"$SEEN_TYPES"
if [ "${#NEW_TYPES[@]}" -gt 0 ]; then
  echo "note: unrecognized session-file line type(s): ${NEW_TYPES[*]} (reported, not a failure)"
  UNKNOWN_LINE_TYPES="$(printf '%s\n' "${NEW_TYPES[@]}" | jq -R . | jq -s .)"
fi

echo "== pulling as a second user, resuming headless, asking a question =="
PULLER_EMAIL="compat-ci-puller-$(date +%s)@test.local"
PULLER_ID="$(curl -sf -X POST "$LOCAL_API_URL/auth/v1/admin/users" \
  -H "apikey: $LOCAL_SERVICE_ROLE_KEY" -H "authorization: Bearer $LOCAL_SERVICE_ROLE_KEY" \
  -H "content-type: application/json" \
  -d "{\"email\": \"$PULLER_EMAIL\", \"password\": \"compat-ci-password-123\", \"email_confirm\": true}" \
  | jq -r '.id')"
curl -sf -X POST "$LOCAL_API_URL/rest/v1/project_members" \
  -H "apikey: $LOCAL_SERVICE_ROLE_KEY" -H "authorization: Bearer $LOCAL_SERVICE_ROLE_KEY" \
  -H "content-type: application/json" -H "Prefer: return=minimal" \
  -d "{\"project_id\": \"$PROJECT_ID\", \"user_id\": \"$PULLER_ID\", \"role\": \"resumer\"}"

export HOME="$WORKDIR/home2"
mkdir -p "$HOME/.carryon" "$PULLED_REPO"
cp "$WORKDIR/config.toml" "$HOME/.carryon/config.toml"
PULLER_TOKENS="$(curl -sf -X POST "$LOCAL_API_URL/auth/v1/token?grant_type=password" \
  -H "apikey: $LOCAL_ANON_KEY" -H "content-type: application/json" \
  -d "{\"email\": \"$PULLER_EMAIL\", \"password\": \"compat-ci-password-123\"}")"
echo "$PULLER_TOKENS" | jq -r '.refresh_token' > "$HOME/.carryon/credentials"
chmod 600 "$HOME/.carryon/credentials"

(cd "$PULLED_REPO" && carryon pull "$SESSION_ID")

ANSWER="$(cd "$PULLED_REPO" && claude -p -r "$SESSION_ID" \
  --model claude-haiku-4-5 \
  --output-format text \
  "What fact did I ask you to remember earlier in this conversation? Answer in one short sentence.")"
echo "resumed answer: $ANSWER"

kill "$AGENT_PID" 2>/dev/null || true
trap - EXIT

case "$ANSWER" in
  *"7 attempts"*|*"payments-webhook"*) ;;
  *) fail "resumed session's answer didn't reference the remembered fact: $ANSWER" ;;
esac

echo "== recording a green run =="
record_run
echo "PASS"
