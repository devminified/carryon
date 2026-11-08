import 'server-only';
import { createClient as createSupabaseClient } from '@supabase/supabase-js';
import type { Database } from '@/shared/types/database.types';

// Service-role client — architecture.md's two narrow cases only (public
// ingress that must bypass RLS, or an admin operation RLS can't express,
// e.g. auth.admin.*). This is the one file allowed to import
// SUPABASE_SERVICE_ROLE_KEY; never import this module from app/dashboard/.
export function createAdminClient() {
  return createSupabaseClient<Database>(
    process.env.NEXT_PUBLIC_SUPABASE_URL!,
    process.env.SUPABASE_SERVICE_ROLE_KEY!,
    { auth: { autoRefreshToken: false, persistSession: false } },
  );
}
