/// <reference path="./supabase-ai.d.ts" />
// CO-501: the one real embedding call in this codebase. Uses
// Supabase's own edge-runtime-bundled model, `gte-small` (Apache-2.0,
// Alibaba's GTE family) via `Supabase.ai.Session` — not a third-party
// API. No key, no cost, no network call off-box, and self-hostable by
// construction (bundled in the same edge-runtime image CO-506's
// self-hosted stack already uses). Chosen over a paid provider
// (OpenAI/Voyage/Cohere) specifically because it needed no API key to
// evaluate for real — see docs/co-501-embedding-choice.md for the
// full ADR and the real 50-query benchmark this function's own output
// was measured against.
//
// `Supabase.ai` only exists inside the real edge-runtime (`supabase
// functions serve`, or deployed) — it's `undefined` under plain
// `deno test` (verified directly). Every caller of embedText takes it
// as an injected dependency (never calls this module directly in a
// code path `deno test` exercises) for exactly that reason — tests
// inject a small deterministic fake instead.

export const EMBEDDING_DIMENSIONS = 384;

/** Real call — mean-pooled, normalized (so cosine similarity and dot
 * product agree, and pgvector's `vector_cosine_ops` HNSW index does
 * the right thing). ~60s on a container's first call ever (the model
 * loads from disk), ~100ms warm after that (measured locally) — see
 * the ADR for the full latency numbers this was budgeted against. */
export async function embedText(text: string): Promise<number[]> {
  const model = new Supabase.ai.Session("gte-small");
  return await model.run(text, { mean_pool: true, normalize: true });
}
