import 'server-only';
import { createServerClient } from '@supabase/ssr';
import { cookies } from 'next/headers';
import type { Database } from '@/shared/types/database.types';

// RLS-scoped: every request runs as the caller's own JWT, read from their
// session cookie. Never attaches the service-role key — see admin.ts for
// the two narrow cases (architecture.md) that need it.
export async function createClient() {
  const cookieStore = await cookies();

  return createServerClient<Database>(
    process.env.NEXT_PUBLIC_SUPABASE_URL!,
    process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY!,
    {
      cookies: {
        getAll() {
          return cookieStore.getAll();
        },
        setAll(cookiesToSet) {
          // A Server Component can't set cookies — this throws there and
          // is swallowed on purpose. proxy.ts refreshes the session on
          // every request, so a Server Component's own write isn't the
          // only thing keeping the session alive.
          try {
            cookiesToSet.forEach(({ name, value, options }) => {
              cookieStore.set(name, value, options);
            });
          } catch {
            // see comment above
          }
        },
      },
    },
  );
}
