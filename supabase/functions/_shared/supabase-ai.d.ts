// CO-501: ambient type for the `Supabase` global Supabase's own edge
// runtime injects (not a standard Deno API, and not shipped by any
// npm/jsr package this codebase already depends on) — only the one
// shape _shared/embed.ts actually calls. See Supabase's own docs on
// "Generate embeddings using Supabase Edge Functions" for the real
// API this mirrors.
declare namespace Supabase {
  namespace ai {
    class Session {
      constructor(model: string);
      run(
        text: string,
        opts?: { mean_pool?: boolean; normalize?: boolean },
      ): Promise<number[]>;
    }
  }
}
