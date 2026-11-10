import { requireGlobalAdmin } from '@/shared/lib/auth';
import { getObservabilityDashboard } from '@/modules/admin/data/queries';
import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { ObservabilityDashboardView } from '@/modules/admin/ui/observability-dashboard';

export default async function AdminDashboardPage() {
  await requireGlobalAdmin();
  const dashboard = await getObservabilityDashboard();
  return (
    <AdminPageShell>
      <ObservabilityDashboardView dashboard={dashboard} />
    </AdminPageShell>
  );
}
