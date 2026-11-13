import { LogOut } from 'lucide-react';
import { signOut } from '../data/actions';
import type { CurrentUser } from '@/shared/lib/auth';
import { Button } from '@/shared/ui/button';

// CO-703: moved into the rail's bottom slot as an icon-sized trigger —
// one click signs out directly, no menu first. The visible identity
// text this used to show is now just a hover title; the accessible
// name stays the literal action ("Sign out", via aria-label, which
// always wins over visible content for name computation) so the
// existing e2e assertion needs no change.
export function UserMenu({ user }: { user: CurrentUser }) {
  const name = user.githubLogin ?? user.email ?? 'Signed in';
  return (
    <form action={signOut}>
      <Button
        type="submit"
        variant="ghost"
        size="icon"
        aria-label="Sign out"
        title={`Signed in as ${name} · Sign out`}
      >
        <LogOut className="size-[18px]" />
      </Button>
    </form>
  );
}
