import { requireGlobalAdmin } from '@/shared/lib/auth';
import { listAllProjects } from '@/modules/admin/data/queries';
import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { RegisterProjectForm } from '@/modules/admin/ui/register-project-form';
import { ProjectSettingsForm } from '@/modules/admin/ui/project-settings-form';

export default async function AdminProjectsPage() {
  await requireGlobalAdmin();
  const projects = await listAllProjects();

  return (
    <AdminPageShell>
      <RegisterProjectForm />
      <ul className="space-y-2">
        {projects.map((project) => (
          <li key={project.id} className="border-border rounded border">
            <details>
              <summary className="flex cursor-pointer items-center justify-between px-4 py-3">
                <span className="font-medium">{project.name}</span>
                <span className="text-muted-foreground text-xs">{project.git_remote}</span>
              </summary>
              <div className="px-4 pb-4">
                <ProjectSettingsForm project={project} />
              </div>
            </details>
          </li>
        ))}
        {projects.length === 0 && <p className="text-muted-foreground text-sm">No projects registered yet.</p>}
      </ul>
    </AdminPageShell>
  );
}
