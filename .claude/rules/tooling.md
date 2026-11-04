# Code Quality & Git Hooks

## Go

`golangci-lint` (enable at least `govet`, `staticcheck`, `errcheck`, `gosec` given the amount of credential/secret handling in `internal/redact` and `internal/api`) + `go vet`. Run: `golangci-lint run`. CI also runs `go test -race ./...`.

## Deno (`supabase/functions/`)

`deno lint` + `deno test`. Each function validates its input with Zod (`npm:zod`) before touching the database — see `edge-functions.md`.

## ESLint (flat config, `web/`)

Extends `next/core-web-vitals`, `typescript-eslint` strict + stylistic type-checked, `eslint-plugin-react-hooks` (errors, not warnings), `eslint-plugin-tailwindcss`.

Custom rules enforcing the architecture:

- `no-restricted-imports` — forbid importing from `data/` outside `hooks/`, Server Components, and other `data/` files
- `no-restricted-syntax` — ban `enum`
- `@typescript-eslint/no-explicit-any` — error

Run: `pnpm lint` (from `web/`). Pre-commit runs only on staged files via `lint-staged`.

## Prettier

```json
{
  "printWidth": 100,
  "singleQuote": true,
  "semi": true,
  "trailingComma": "all",
  "arrowParens": "always",
  "tabWidth": 2,
  "plugins": ["prettier-plugin-tailwindcss"]
}
```

Go files are formatted with `gofmt`/`goimports`, not Prettier.

## Conventional Commits + Commitlint

Format: `<type>(<scope>): <subject>`.

Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `perf`, `style`, `build`, `ci`, `revert`.

Scopes (suggested, matching `internal/` packages and ticket areas): `hook`, `agent`, `queue`, `state`, `reader`, `redact`, `upload`, `snapshot`, `pull`, `claudepath`, `project`, `api`, `service`, `db`, `functions`, `auth`, `web`, `ui`, `search`, `ci`, `release`, `deps`.

`.commitlintrc.json`:

```json
{
  "extends": ["@commitlint/config-conventional"],
  "rules": {
    "scope-enum": [2, "always", [
      "hook", "agent", "queue", "state", "reader", "redact", "upload", "snapshot",
      "pull", "claudepath", "project", "api", "service",
      "db", "functions", "auth", "web", "ui", "search",
      "ci", "release", "deps"
    ]],
    "subject-case": [2, "always", "lower-case"],
    "header-max-length": [2, "always", 100]
  }
}
```

## Husky + lint-staged

`.husky/pre-commit` (run from `web/`, or a repo-root script that dispatches by changed path):

```sh
pnpm lint-staged
pnpm typecheck
```

`.husky/commit-msg`:

```sh
pnpm commitlint --edit "$1"
```

`.lintstagedrc.json`:

```json
{
  "*.{ts,tsx}": ["eslint --fix", "prettier --write"],
  "*.go": ["gofmt -l", "golangci-lint run --fix"],
  "*.{md,json,yml,yaml}": ["prettier --write"]
}
```

> `*.sql` is intentionally excluded — Postgres `plpgsql`/RLS/`create policy` syntax isn't reliably formattable by Prettier SQL plugins. Migrations are hand-formatted; `supabase db lint` is the authoritative check.
