import { AdminNav } from './admin-nav';

// A plain component, deliberately NOT a layout.tsx — an earlier
// version of this module used admin/layout.tsx to wrap every /admin
// page and ran requireGlobalAdmin() there once. That built and
// rendered the real "This page could not be found" UI for a non-admin
// correctly, but the HTTP response kept its already-streamed 200
// status rather than a real 404 (reproduced directly: Next.js 16.4.5
// loses the 404 status code when notFound() is thrown from a
// layout.tsx with an intervening layout segment between it and the
// root, a known class of framework bug — see this ticket's own PR
// description). Every /admin page now calls requireGlobalAdmin()
// itself, directly in the page component (the one shape this app's
// existing (dashboard)/p/[project]/page.tsx already proves sends a
// real 404), and wraps its own content in this shell for the shared
// heading/nav — plain JSX composition, no extra route-level layout.
export function AdminPageShell({ children }: { children: React.ReactNode }) {
  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Admin</h1>
      <AdminNav />
      {children}
    </div>
  );
}
