import 'server-only';
import { createClient } from '@/shared/lib/supabase/server';

// Same headers internal/api.Client.post uses against Supabase's own
// Edge Functions (apikey + the caller's own access token, never the
// service-role key) — called server-side here since a Server Component
// already has the signed-in user's session available without a round
// trip to the browser first.
export async function callEdgeFunction<T>(name: string, body: unknown): Promise<T> {
  const supabase = await createClient();
  const { data, error } = await supabase.auth.getSession();
  if (error || !data.session) {
    throw new Error(`callEdgeFunction(${name}): no active session`);
  }

  const res = await fetch(`${process.env.NEXT_PUBLIC_SUPABASE_URL}/functions/v1/${name}`, {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      apikey: process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY!,
      authorization: `Bearer ${data.session.access_token}`,
    },
    body: JSON.stringify(body),
    cache: 'no-store',
  });

  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new EdgeFunctionError(name, res.status, text);
  }
  return res.json() as Promise<T>;
}

export class EdgeFunctionError extends Error {
  constructor(
    public readonly functionName: string,
    public readonly status: number,
    public readonly body: string,
  ) {
    super(`${functionName}: HTTP ${status} ${body}`);
  }
}
