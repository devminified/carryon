import { notFound } from 'next/navigation';
import { getProjectForCurrentUser } from '@/modules/projects/data/queries';

export default async function ProjectPage({ params }: { params: Promise<{ project: string }> }) {
  const { project: projectId } = await params;
  const project = await getProjectForCurrentUser(projectId);
  if (!project) notFound();

  return (
    <div>
      <h1 className="text-foreground text-lg font-semibold">{project.name}</h1>
      <p className="text-muted-foreground mt-2 text-sm">Session list coming in CO-402.</p>
    </div>
  );
}
