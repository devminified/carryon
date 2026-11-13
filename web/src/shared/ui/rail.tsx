import Link from 'next/link';
import { FolderGit2, ShieldCheck } from 'lucide-react';
import { Button } from './button';
import { LogoMark } from './logo';

// CO-703: the app's one persistent piece of global chrome, replacing
// the old single top bar. Deliberately lives in shared/ui/, not a new
// "modules/layout" domain module — folder-structure.md's own rule
// ("a module must not import another module's internals") would
// otherwise block this from hosting the real sign-out control
// directly, since that's modules/auth's internal data/actions.ts.
// `bottomSlot` is how the caller ((dashboard)/layout.tsx) composes
// that in instead — the same "pass an already-composed node down"
// pattern CO-608 already used for the session viewer's family tree.
export function Rail({
  isAdmin,
  bottomSlot,
}: {
  isAdmin: boolean;
  bottomSlot: React.ReactNode;
}) {
  return (
    <nav
      aria-label="Primary"
      className="border-border flex w-14 shrink-0 flex-col items-center gap-1 border-r py-3.5"
    >
      <Link
        href="/"
        aria-label="Carryon"
        className="bg-primary text-primary-foreground mb-4 flex size-8 items-center justify-center rounded-lg"
      >
        <LogoMark className="size-[18px]" />
      </Link>

      <Button asChild variant="ghost" size="icon" title="Sessions">
        <Link href="/" aria-label="Sessions">
          <FolderGit2 className="size-[18px]" />
        </Link>
      </Button>

      {/* CO-410: hidden for everyone but a global admin — a UI
          convenience only, same caveat as the old top bar's "Admin"
          link carried; requireGlobalAdmin() in every /admin page is
          the real enforcement. */}
      {isAdmin && (
        <Button asChild variant="ghost" size="icon" title="Admin">
          <Link href="/admin/projects" aria-label="Admin">
            <ShieldCheck className="size-[18px]" />
          </Link>
        </Button>
      )}

      <div className="flex-1" />

      {bottomSlot}
    </nav>
  );
}
