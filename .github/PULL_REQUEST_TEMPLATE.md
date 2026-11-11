## What changed

## Why

## How this was tested

- [ ] `go vet ./...`, `golangci-lint run`, `go test -race ./...` pass (or the equivalent web checks: `pnpm lint`, `pnpm typecheck`, `pnpm test`)
- [ ] A DB-level guard (RLS policy, migration) was verified against a forbidden role, if this touches the schema
- [ ] Clicked through the actual change in a browser, if this touches `web/`

## Related issue

Closes #
