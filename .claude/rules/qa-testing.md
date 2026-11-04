# QA Testing — Phase-Gate Sweep SOP

> Binding rules for a manual or Playwright/MCP-assisted QA pass at a phase gate (Doc's build plan: Phase 1 "working pull," Phase 2 "code snapshot + resume," Phase 3 "safe," Phase 4 "browsable," Phase 5 "shareable").
> **Sibling rule:** `.claude/rules/testing.md` covers automated unit/integration/e2e tests. This file covers the manual/exploratory sweep layered on top before a phase gate or a release.

## 1. Golden rules

1. **Never run a destructive test case against `carryon-prod`.** Use `carryon-staging`. The seed script (once it exists) should refuse to run against a URL containing `prod`.
2. **One bug = one row in the log.** Don't bundle two unrelated defects into one finding.
3. **Never edit a closed bug entry.** Once it has `→ PR #NNN merged YYYY-MM-DD`, it's history.
4. **Evidence beats prose.** A failing case needs a screenshot or a terminal transcript. "It didn't work" with no evidence is not a bug.
5. **Don't mark a case Passed by inference.** Exercise it end-to-end or mark it `Skipped`/`Blocked`.
6. **Stop on P0.** Pause the sweep, file the bug, notify the ticket owner.

## 2. The real gate, per Doc's build plan

> "If you can't resume a teammate's live session from your own machine, nothing in Phase 2 is worth building yet."

Every phase gate has one non-negotiable scenario that must work end-to-end on real machines, not just in CI:

| Gate | Scenario |
| --- | --- |
| Phase 1 | A live session on machine A is visible (via `carryon status`/`ls`) and uploading on machine B's view of the API within NFR-02's 2-minute lag. |
| Phase 2 | `carryon pull` on a second machine restores the code **and** resumes the Claude Code session, including uncommitted work (snapshot). |
| Phase 3 | A `viewer` role genuinely cannot pull; redaction corpus holds against a live paste of a fake secret. |
| Phase 4 | The web app lists sessions by project/developer/date and shows one read-only, matching what actually pulled. |
| Phase 5 | Search returns the right session for a known query; self-hosted deployment boots from a clean checkout. |

Run the matching scenario by hand before signing off any gate, even if CI is green.

## 3. Severity rubric

- **P0** — breaks capture, pull/resume, or a redaction/RLS leak (a secret or a session visible to a role that shouldn't see it). Blocks the gate.
- **P1** — silently wrong data (wrong byte count, a chunk out of order, a wrong role shown). Hotfix branch off `main`.
- **P2** — UX glitch (CLI help text, web copy). Batch with the next ticket touching that surface.
- **P3** — cosmetic. Park.

When in doubt, pick the more severe. A security-tagged finding (redaction miss, RLS leak, signed URL with no expiry) is never downgraded below P0/P1 regardless of how minor it looks.

## 4. NFR verification checklist

Map every finding back to the NFR table in Doc (Overview) it threatens, and don't close a phase gate until each NFR relevant to that phase has a passing, reproducible test — automated where `testing.md` already covers it, manual otherwise:

- **NFR-01** (hook p99 < 50ms) — benchmark on each OS.
- **NFR-04** (durability across crash/offline/reboot) — chaos test, byte-identical rebuild.
- **NFR-05** (idempotency) — retry an upload twice, assert one chunk.
- **NFR-07** (secret handling, signed URL TTL) — redaction corpus + manually confirm a signed URL expires.
- **NFR-08** (access control) — pgTAP plus one manual cross-role check per new table.
- **NFR-11** (offline tolerance) — disconnect network for the stated window, confirm no data loss on reconnect.
- **NFR-12** (privacy scope) — confirm an unregistered repo never uploads anything, by testing a negative case, not just the happy path.

## 5. Evidence conventions

- Screenshots/terminal captures live under `test/fixtures/qa-evidence/v<phase>/`, one subdirectory per phase gate, never overwritten.
- Filenames: `NN-<area>-<scenario-slug>.png` (or `.txt` for a terminal transcript).
- Mask any real secret or real session content in evidence before it's committed — a QA screenshot showing an actual developer's transcript is itself a P0.

## 6. Logging a finding

```markdown
### #N — Pn — short title

**Where:** command / page / NFR (one line)

**Symptom:** what happened (2-4 sentences)

**Repro:**
1. ...
2. ...

**Expected:** … **Actual:** … **Evidence:** `test/fixtures/qa-evidence/v<phase>/NN-slug.png`

**Suggested fix:** optional, one paragraph.
```

On fix, append `→ PR #NNN merged YYYY-MM-DD` to the heading. The body never changes after that.

## 7. When to defer

Legitimately unrunnable cases (e.g. the Tier 3 cell-architecture scaling tests, before any tier justifies them) are deferred, not fake-passed: `Status = Skipped`, a reason, and the trigger that would unblock it. A case deferred across three consecutive gates gets escalated — either build the fixture or delete the case.
