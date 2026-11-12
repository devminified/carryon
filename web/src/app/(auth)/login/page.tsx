import { Suspense } from 'react';
import { LoginForm } from '@/modules/auth/ui/login-form';

// Found in production (not reproducible locally, since `next start`
// has no CDN in front of it): this page has no per-request data
// dependency, so Next statically prerenders it — baking in one fixed
// HTML response with no nonce on its own inline hydration bootstrap
// script, served from Vercel's CDN cache to every visitor. proxy.ts's
// CSP header still carries a fresh random nonce on every request
// (crypto.randomUUID(), see that file's own comment on why a per-
// request nonce is needed at all) — for a *dynamic* page those two are
// generated together and match; for this *static* one they never can,
// so the browser's CSP blocks the page's own bootstrap script outright
// and it never hydrates (confirmed directly: the CSP header's nonce
// differs on every request while the cached HTML body — and the
// nonce baked into its inline scripts — stays byte-identical).
// `force-dynamic` makes Next render this per request instead, the
// same as every other route in this app, so the nonce Next embeds in
// its own scripts always matches the one proxy.ts just put in the
// response header for that exact request.
export const dynamic = 'force-dynamic';

export default function LoginPage() {
  return (
    <main className="bg-background flex min-h-screen flex-col items-center justify-center gap-6 px-4">
      <div className="flex flex-col items-center gap-2 text-center">
        <h1 className="text-foreground text-2xl font-semibold">Carryon</h1>
        <p className="text-muted-foreground text-sm">
          Sign in with your Devminified GitHub account.
        </p>
      </div>
      <Suspense fallback={null}>
        <LoginForm />
      </Suspense>
    </main>
  );
}
