import Link from 'next/link';
import { requireUser } from '@/shared/lib/auth';
import { listProjectsForCurrentUser } from '@/modules/projects/data/queries';
import { ProjectSwitcher } from '@/modules/projects/ui/project-switcher';
import { UserMenu } from '@/modules/auth/ui/user-menu';

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  const user = await requireUser();
  const projects = await listProjectsForCurrentUser();

  return (
    <div className="flex min-h-screen flex-col">
      <header className="border-border flex items-center justify-between border-b px-6 py-3">
        <div className="flex items-center gap-4">
          <Link href="/" className="text-foreground font-semibold">
            Carryon
          </Link>
          <ProjectSwitcher projects={projects} />
        </div>
        <UserMenu user={user} />
      </header>
      <main className="flex-1 px-6 py-6">{children}</main>
    </div>
  );
}
