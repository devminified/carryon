import { requireGlobalAdmin } from '@/shared/lib/auth';
import { getRedactionConfig } from '@/modules/admin/data/queries';
import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { RedactionConfigForm } from '@/modules/admin/ui/redaction-config-form';

// "Summary budget" (this ticket's own wording) lives per-project on
// the Projects page (projects.daily_token_budget) — there's no global
// default to edit here, so this page sticks to the genuinely global
// settings redaction_config already holds: disabled rule IDs, the
// extra path denylist, and the pinned agent version (CO-301/CO-308).
export default async function AdminConfigPage() {
  await requireGlobalAdmin();
  const config = await getRedactionConfig();
  return (
    <AdminPageShell>
      <RedactionConfigForm config={config} />
    </AdminPageShell>
  );
}
