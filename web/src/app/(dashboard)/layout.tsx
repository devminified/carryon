import Link from 'next/link';
import { isGlobalAdmin, requireUser } from '@/shared/lib/auth';
import { listProjectsForCurrentUser } from '@/modules/projects/data/queries';
import { ProjectSwitcher } from '@/modules/projects/ui/project-switcher';
import { UserMenu } from '@/modules/auth/ui/user-menu';
import { Wordmark } from '@/shared/ui/logo';

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  const user = await requireUser();
  const [projects, isAdmin] = await Promise.all([listProjectsForCurrentUser(), isGlobalAdmin()]);

  return (
    <div className="flex min-h-screen flex-col">
      <header className="border-border flex items-center justify-between border-b px-6 py-3">
        <div className="flex items-center gap-4">
          <Link href="/" className="text-foreground">
            <Wordmark />
          </Link>
          <ProjectSwitcher projects={projects} />
          {/* CO-410: hidden for everyone but a global admin — a UI
              convenience only, same caveat as every other role-gated
              control in this app; requireGlobalAdmin() in the /admin
              layout is the real enforcement. */}
          {isAdmin && (
            <Link href="/admin" className="text-muted-foreground hover:text-foreground text-sm">
              Admin
            </Link>
          )}
        </div>
        <UserMenu user={user} />
      </header>
      <main className="flex-1 px-6 py-6">{children}</main>
    </div>
  );
}
