import type { Database } from '@/shared/types/database.types';

// Order matters: viewer < resumer < admin, matching the Postgres enum's
// own declared order (database-rls.md) — never re-sort this.
export const ROLES = ['viewer', 'resumer', 'admin'] as const;
export type Role = Database['public']['Enums']['project_role'];

export function roleAtLeast(role: Role, min: Role): boolean {
  return ROLES.indexOf(role) >= ROLES.indexOf(min);
}
