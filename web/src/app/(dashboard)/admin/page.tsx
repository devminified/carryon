import { redirect } from 'next/navigation';
import { requireGlobalAdmin } from '@/shared/lib/auth';

export default async function AdminIndexPage() {
  await requireGlobalAdmin();
  redirect('/admin/projects');
}
