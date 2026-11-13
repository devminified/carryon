import { isGlobalAdmin, requireUser } from '@/shared/lib/auth';
import { UserMenu } from '@/modules/auth/ui/user-menu';
import { Rail } from '@/shared/ui/rail';

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  const user = await requireUser();
  const isAdmin = await isGlobalAdmin();

  return (
    <div className="flex min-h-screen">
      <Rail isAdmin={isAdmin} bottomSlot={<UserMenu user={user} />} />
      <main className="min-w-0 flex-1 overflow-x-auto px-6 py-6">{children}</main>
    </div>
  );
}
