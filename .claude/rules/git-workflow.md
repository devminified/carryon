# Git Workflow & Branching

This codebase captures developers' full Claude Code sessions — code, prompts, and whatever secrets slipped past redaction — so an accidental merge to `main` is harder to simply "revert and forget" than in a typical app. Treat merges with the same discipline the directory default asks for, applied through tickets.

## Branch model

- **`main`** — production. Auto-deploys the web app to Vercel; Edge Functions deploy from tags (see `commands.md`). Receives only reviewed PRs.
- **`co-<id>-<slug>`** — one branch per ticket, off `main`. The ID is the branch-name prefix per Doc's ticket template: `co-107-capture-daemon`, `co-302-roles-and-rls`. Squash-merged.
- **`fix/{slug}`** — hotfix branches off `main` for anything that can't wait for the next ticket.
- **`chore/{slug}`** — tooling, deps, docs without behavior change.

## Workflow

1. Branch off `main`: `git checkout -b co-107-capture-daemon main`.
2. Work the ticket. Commit often, locally, Conventional Commits format (see `tooling.md`).
3. When every testing point and validation point on the ticket's phase tab is checked (Doc §"Definition of done"): push and open a PR.
4. PR title matches the squash-merge commit: `feat(agent): add capture daemon with debounce and scheduler`.
5. PR body: ticket reference (`Closes CO-107`), summary, migration list if any, manual test plan.
6. After merge, delete the branch.

## Enforcement

`carryon` is public, so (unlike `hr-system`'s private-repo Team-plan gap) GitHub branch protection on `main` is live, not just discipline: PR required, required status checks (`lint`, `test (ubuntu-latest)`, `test (macos-latest)`, `test (windows-latest)`, `cross-compile`), enforced for admins too, linear history, no force-push, no deletion. A direct push to `main` is rejected by GitHub itself, not just by convention.

**Bootstrap exception:** `CO-101`'s scaffold (the first five commits) was pushed directly to `main` before this protection existed, while bootstrapping the repo itself. From `CO-102` onward, every ticket goes through the branch-per-ticket flow below — no more exceptions.

## Hard rules

- **Never push directly to `main`.**
- **Never `git push --force`** on `main`; fine on a personal ticket branch before review begins.
- **Never `--no-verify`** to skip Husky hooks — fix the cause.
- **Migrations are append-only.** Never edit a migration file after it has merged to `main`. Add a new migration that corrects course.
- **Never edit a merged rule-set change in `internal/redact` without a new corpus test** — same append-only spirit applied to security rules.
- **One PR per ticket.** Don't bundle CO-107 and CO-109 in the same branch even though they're related.
- **No AI/tool attribution in commits or PRs.** No `Co-Authored-By: Claude`, no "🤖 Generated with…" footer. Commits attribute to the human author only.

## Commit message format

```
<type>(<scope>): <subject>
```

Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `style`, `build`, `ci`, `revert`.
Scopes: see `tooling.md` §"Conventional Commits + Commitlint".

Examples:

- `feat(hook): add stdin JSON parse and queue append`
- `fix(upload): retry signed URL fetch on 403 near expiry`
- `feat(redact): add entropy scan for unmatched high-entropy strings`
- `chore(deps): bump @supabase/ssr to 0.5`

Subject is lower-case, no trailing period, max 100 chars.
