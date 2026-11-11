# Contributing to Carryon

Thanks for considering a contribution. This doc covers the practical path from "I want to fix/add something" to an opened PR — for the full engineering conventions (architecture, invariants, testing, naming), see `CLAUDE.md` and `.claude/rules/`; that's the single source of truth this doc intentionally doesn't repeat.

## Before you start

- **For anything beyond a small fix**, open an issue first and describe what you're planning — it's much cheaper to align on approach before code than after. Check [existing issues](https://github.com/devminified/carryon/issues) for the same idea first.
- **Security issues** go to `SECURITY.md`, never a public issue.

## Local setup

```bash
git clone https://github.com/devminified/carryon.git
cd carryon
go build ./...                       # the laptop binary
cd web && pnpm install && cd ..       # the web app

supabase start                       # local Supabase (needs the Supabase CLI + Docker)
cd web && pnpm dev                   # the web app on :3000
```

See the README's own "Install" and "Development" sections for the full picture, including env vars (`.env.example`, `web/.env.local`, `supabase/functions/.env.example`).

## Making a change

1. Branch off `main`: `git checkout -b fix/<slug>` or `feat/<slug>` (this repo's own tickets use `co-<id>-<slug>`, but an outside contributor's branch name doesn't need to match that scheme).
2. Follow the module checklist that matches what you're touching — `.claude/rules/module-checklist.md` has one for a new laptop pipeline stage, a new Edge Function/schema change, a new web page, and a new CLI command.
3. Match the testing bar already in place: `go test -race ./...` for Go, the pgTAP suite for RLS changes, Vitest/Playwright for the web app — see `.claude/rules/testing.md` for the full matrix. A PR that adds a new redaction rule needs both a positive and a negative corpus-test case (`.claude/rules/redaction.md`).
4. Run the same checks CI runs before pushing:

   ```bash
   go vet ./... && golangci-lint run && go test -race ./...
   cd web && pnpm lint && pnpm typecheck && pnpm test
   ```

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/): `<type>(<scope>): <subject>`, lower-case subject, no trailing period, under 100 characters. See `.claude/rules/tooling.md` for the full type/scope list this repo's own commitlint config enforces.

## Opening the PR

- One focused PR per change — a schema change, a UI feature, and a refactor each get their own PR, even if related.
- Describe what changed, why, and how you tested it (a DB-level guard you verified, a browser flow you clicked through) — `.claude/rules/git-workflow.md` has the full template this repo's own PRs use.
- CI (lint, Go tests on macOS/Linux/Windows, the Supabase/pgTAP suite, the web app's lint/typecheck/test/build, and a Playwright e2e run) must pass before a maintainer will review. All of it runs automatically on your PR.

## What maintainers will push back on

The same anti-patterns `CLAUDE.md` and `.claude/rules/architecture.md` already reject on sight — disabling RLS, bypassing the laptop's redaction engine, putting business logic in `web/src/app/`, a Server Action with no matching RLS policy, `any` types, skipping `with check` on an `insert`/`update` policy. If you're not sure whether something you're planning crosses one of these lines, ask in the issue before writing the code.

## License

By contributing, you agree that your contribution is licensed under this repo's MIT license (`LICENSE`).
