# CO-501: embedding provider and model choice

**Decision: `gte-small` via Supabase's own `Supabase.ai.Session`, not a paid third-party API.**

## Context

CO-501 needs an embedding model for session/passage vectors and query embedding at search time. The ticket asks for an ADR comparing quality, cost, and self-host options before building — and when this ticket was picked up, no `ANTHROPIC_API_KEY`-style embedding API key existed for this project, which surfaced as a real blocking question rather than a theoretical one. The answer came from the user directly: "do we have any opensource embedding which we can use?" — so the options below were evaluated with that constraint already in view.

## Options considered

| Option | Quality | Cost | Self-host |
| --- | --- | --- | --- |
| OpenAI `text-embedding-3-small` (1536-dim) | High | ~$0.02 / 1M tokens | No — requires an OpenAI API key and a per-call network dependency, in every environment including a future self-hosted install |
| Cohere `embed-v4` | High | Paid, per-call | No |
| **`gte-small` via `Supabase.ai.Session`** | Good (see benchmark below) | **$0** — runs inside the edge runtime process, no external call | **Yes, by construction** — the model ships inside the Supabase edge runtime itself (Deno's own bundled ONNX runtime), so a self-hosted Carryon install gets it for free with no extra provisioning step |

`gte-small` is Alibaba's General Text Embeddings model (Apache-2.0), 384-dimensional output. Supabase bundles it directly into the edge-function runtime via a global `Supabase.ai.Session("gte-small")` object — confirmed for real against a local `supabase functions serve` instance (not assumed from documentation): the `Supabase` global is present and returns real 384-dim vectors inside the deployed/served edge runtime, and is `undefined` under plain `deno test` (a hard constraint that shaped `_shared/embed.ts`'s dependency-injection design — see that file's own comment and `summarize.ts`'s required, non-optional `embedText` field).

**Why this wins on Carryon's own terms, not just cost:** CLAUDE.md's "no secret leaves the laptop unredacted" and the three-trust-zones principle (`architecture.md`) are about session content specifically — `summarize-session` already sends redacted, condensed text to Anthropic for titles/summaries, so a third-party embedding API isn't ruled out on security grounds alone. The deciding factor is Carryon's own eventual self-hosted, open-source release (Phase 5's own stated goal): an embedding provider that requires a paid API key would mean "semantic search doesn't work until you also sign up for and pay another vendor" for every self-hosted install. `gte-small` removes that dependency entirely — it's part of the same Supabase stack a self-hosted Carryon already runs.

**Tradeoff accepted:** a dedicated 1536-dim commercial model (OpenAI, Cohere) likely has moderately higher retrieval quality than a 384-dim general-purpose model. The benchmark below measures whether `gte-small` is good enough for this product's actual use case (finding one's own team's past sessions by plain-language description, not open-domain web search) — not whether it's the best embedding model in the abstract.

## Latency (measured directly, local `supabase functions serve`)

- **Cold** (first call in a fresh edge-runtime container, model load included): ~30–60s, measured directly across two separate cold starts (31.2s and ~59.5s) during this ticket's own verification — this delay happens once per container lifetime, not once per request.
- **Warm** (every call after the first in the same container): ~100–110ms, measured directly.

This shapes where `embedText()` gets called: never on a latency-critical path with no retry tolerance. `summarizeSession()`'s own session/passage embedding calls are each wrapped in their own try/catch (a failed embed never blocks the summary write — see `summarize.ts`), and the `search` Edge Function's one embedding call per query is the acceptable cost of a user-initiated search request, not a background job.

## Benchmark: 50 labeled queries (the ticket's own testing point)

Run for real against the actual production code path — not a standalone cosine-similarity script. 49 distinct, realistic past-engineering-task fixtures (title + one-line summary + files touched, covering payments, auth, caching, CI, infra, mobile, and more) were inserted as real `sessions` rows with real `gte-small` embeddings (computed the same way `summarizeSession()` computes them: `title + "\n" + summary + "\n" + files_touched.join(" ")`), scoped to a real test project with a real test user as a real project member. For each fixture, one natural-language query — deliberately paraphrased away from the title/summary wording, matching the ticket's own example ("where did we fix duplicate webhooks") — was sent to the real, deployed `search` Edge Function as that real authenticated user, and the response was checked for whether the fixture's own session ID appeared in the top 3 ranked results.

```
Top-1 accuracy: 44/49 = 89.8%
Top-3 accuracy: 48/49 = 98.0%
```

Both results clear the ticket's own bar ("the right session is in the top 3 at least 80% of the time") with a wide margin. The one miss: a query paraphrasing an SSO login failure ("why were some customers unable to log in through single sign on") ranked the correct session (a SAML clock-skew fix) 5th, behind four unrelated sessions — plausible given "single sign on" and "SAML assertion clock skew" share little surface vocabulary, and no `session_passages` rows existed for this benchmark (see Scope below), so the full-text/passage terms of the hybrid score contributed nothing here.

**Scope of this benchmark, honestly stated:** it exercises the session-level `embedding` column and `search_sessions`' full-text term (`search_vector` over title+summary) — the real production formula's `(1 - cosine) * 0.6 + best_passage_score * 0.3 + text_rank * 0.1` — but with no `session_passages` rows, so `best_passage_score` was always 0 for every fixture. A real session with real condensed-transcript passages would very likely do at least as well, since the passage term can only add signal, never subtract it. This benchmark is a credible lower bound on real-world quality, not an upper bound.

## Re-embedding on summary change

`summarizeSession()` recomputes `sessions.embedding` on every successful run (merged into the same `update` call that writes `title`/`summary`/`outcome`, not a second write — see that file's own comment on why) — so a resummarize (CO-408's admin "Re-summarize" button, or the 30-minute live-session sweep) always replaces the old vector with one computed from the new title/summary, never leaving a stale embedding behind. `session_passages` rows are append-only instead (new turns get new passages, keyed by a running `seq`) since a transcript turn is immutable once Claude Code writes it — there is nothing to re-embed there, only new material to add.
