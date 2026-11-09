import { requireGlobalAdmin } from '@/shared/lib/auth';
import { listMembersForProject, listProjectsForPicker } from '@/modules/admin/data/queries';
import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { ProjectPicker } from '@/modules/admin/ui/project-picker';
import { MembersTable } from '@/modules/admin/ui/members-table';
import { AddMemberForm } from '@/modules/admin/ui/add-member-form';

export default async function AdminMembersPage({
  searchParams,
}: {
  searchParams: Promise<{ project?: string }>;
}) {
  await requireGlobalAdmin();
  const { project: projectId } = await searchParams;
  const projects = await listProjectsForPicker();
  const members = projectId ? await listMembersForProject(projectId) : [];

  return (
    <AdminPageShell>
      <ProjectPicker projects={projects} />
      {projectId ? (
        <>
          <MembersTable members={members} />
          <AddMemberForm projectId={projectId} />
        </>
      ) : (
        <p className="text-muted-foreground mt-4 text-sm">Pick a project to view and manage its members.</p>
      )}
    </AdminPageShell>
  );
}
