import { signOut } from '../data/actions';
import type { CurrentUser } from '@/shared/lib/auth';
import { Button } from '@/shared/ui/button';

export function UserMenu({ user }: { user: CurrentUser }) {
  return (
    <form action={signOut} className="flex items-center gap-3">
      <span className="text-foreground text-sm">
        {user.githubLogin ?? user.email ?? 'Signed in'}
      </span>
      <Button type="submit" variant="ghost" size="sm">
        Sign out
      </Button>
    </form>
  );
}
