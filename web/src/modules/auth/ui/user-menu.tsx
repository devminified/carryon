import { signOut } from '../data/actions';
import type { CurrentUser } from '@/shared/lib/auth';

export function UserMenu({ user }: { user: CurrentUser }) {
  return (
    <form action={signOut} className="flex items-center gap-3">
      <span className="text-foreground text-sm">
        {user.githubLogin ?? user.email ?? 'Signed in'}
      </span>
      <button type="submit" className="text-muted-foreground hover:text-foreground text-sm">
        Sign out
      </button>
    </form>
  );
}
