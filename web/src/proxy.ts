import { createServerClient } from '@supabase/ssr';
import { NextResponse, type NextRequest } from 'next/server';

// Next.js 16 renamed middleware.ts to proxy.ts (same mechanism) — see
// node_modules/next/dist/docs/.../middleware.md. Refreshes the Supabase
// session cookie on every request and redirects unauthenticated access
// to protected routes — the real route-protection enforcement a
// Server Component's requireUser() backs up, not the other way around:
// Proxy isn't a full session/authorization solution on its own (Next's
// own proxy guide), so requireUser()/requireRole() still run server-side.
const PUBLIC_PATHS = ['/login', '/auth/callback'];

export async function proxy(request: NextRequest) {
  // CO-507: a fresh nonce every request, carried on the REQUEST's own
  // headers (not just the response's) — this is what lets Next.js's
  // own RSC/hydration bootstrap scripts (the inline <script> tags a
  // strict script-src would otherwise block outright, confirmed by a
  // real browser test against this exact CSP: the whole app shell
  // failed to hydrate) pick up the matching nonce automatically
  // during server-side rendering, per Next's own documented pattern
  // (content-security-policy.md's "Adding a nonce with Proxy").
  // 'strict-dynamic' is what then lets those nonce'd scripts load
  // their own dependencies without each one needing 'self' too.
  const nonce = Buffer.from(crypto.randomUUID()).toString('base64');
  const cspHeaderValue = buildCSP(nonce);
  const requestHeaders = new Headers(request.headers);
  requestHeaders.set('x-nonce', nonce);
  requestHeaders.set('Content-Security-Policy', cspHeaderValue);

  let response = NextResponse.next({ request: { headers: requestHeaders } });

  const supabase = createServerClient(
    process.env.NEXT_PUBLIC_SUPABASE_URL!,
    process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY!,
    {
      cookies: {
        getAll() {
          return request.cookies.getAll();
        },
        setAll(cookiesToSet) {
          cookiesToSet.forEach(({ name, value }) => request.cookies.set(name, value));
          response = NextResponse.next({ request: { headers: requestHeaders } });
          cookiesToSet.forEach(({ name, value, options }) =>
            response.cookies.set(name, value, options),
          );
        },
      },
    },
  );

  const { data } = await supabase.auth.getUser();
  const isPublic = PUBLIC_PATHS.some((p) => request.nextUrl.pathname.startsWith(p));

  if (!data.user && !isPublic) {
    const loginUrl = new URL('/login', request.url);
    loginUrl.searchParams.set('next', request.nextUrl.pathname);
    return NextResponse.redirect(loginUrl);
  }

  if (data.user && request.nextUrl.pathname.startsWith('/login')) {
    return NextResponse.redirect(new URL('/', request.url));
  }

  // CO-402/CO-403: a real 404 for a resource the caller can't see (RLS
  // returns zero rows either way — "doesn't exist" and "not a member"
  // collapse to the same answer, by design) has to happen here, before
  // the page starts streaming — (dashboard)/loading.tsx means the
  // page's own response already committed its 200 status by the time
  // its Server Component calls notFound() (Next's own loading.tsx doc,
  // "Status codes": "Calling notFound() before streaming starts
  // returns a 404" — after it, the 200 already sent can't be un-sent).
  // Each of these two queries is cheap enough to run on every request
  // matching its own narrow path shape.
  const uuid = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
  const projectMatch = request.nextUrl.pathname.match(new RegExp(`^/p/(${uuid})(?:/|$)`, 'i'));
  const sessionMatch = request.nextUrl.pathname.match(
    new RegExp(`^/sessions/(${uuid})(?:/|$)`, 'i'),
  );

  if (data.user && projectMatch) {
    const { data: project } = await supabase
      .from('projects')
      .select('id')
      .eq('id', projectMatch[1])
      .maybeSingle();
    if (!project) return notFoundResponse(cspHeaderValue);
  }

  if (data.user && sessionMatch) {
    // owner_or_member_can_read_sessions (CO-302) is itself "owner OR
    // viewer+" — exactly the minimum role get-session-transcript
    // requires, so a null row here already means the real page load
    // would also 404/403, no separate role check needed.
    const { data: session } = await supabase
      .from('sessions')
      .select('id')
      .eq('id', sessionMatch[1])
      .maybeSingle();
    if (!session) return notFoundResponse(cspHeaderValue);
  }

  // CO-410: same reasoning as the two checks above, same reason a
  // page-level notFound() alone isn't enough — every /admin page (and
  // the audit CSV export route) re-checks requireGlobalAdmin() itself
  // too (defence in depth, auth.md), but only this middleware check
  // can actually make the HTTP status a real 404 rather than a 200
  // with 404-looking content.
  if (data.user && request.nextUrl.pathname.startsWith('/admin')) {
    const { data: adminRows } = await supabase
      .from('project_members')
      .select('project_id')
      .eq('user_id', data.user.id)
      .eq('role', 'admin')
      .limit(1);
    if (!adminRows || adminRows.length === 0) return notFoundResponse(cspHeaderValue);
  }

  return withSecurityHeaders(response, cspHeaderValue);
}

function notFoundResponse(cspHeaderValue: string): NextResponse {
  return withSecurityHeaders(
    new NextResponse('This page could not be found.', {
      status: 404,
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    }),
    cspHeaderValue,
  );
}

// CO-507: a real gap found by this ticket's own security review —
// CLAUDE.md's anti-pattern list already says "strict CSP" for the
// transcript viewer specifically because a transcript can contain
// text Claude read from the open web (prompt-injection surface), but
// no CSP or other standard security header existed anywhere in this
// app until now. A nonce (not a blanket 'unsafe-inline') is what lets
// Next.js's own inline bootstrap scripts run while still blocking an
// attacker-injected one — see this file's own `nonce` comment above
// for why a plain strict script-src alone broke the app in a real
// browser test. Set in this one place every request already passes
// through, rather than per-page, so it can never be missed on a new
// route.
function buildCSP(nonce: string): string {
  const supabaseURL = process.env.NEXT_PUBLIC_SUPABASE_URL ?? '';
  // React itself calls eval() in development only (to reconstruct
  // server-side error call stacks in the browser — Next's own CSP
  // guide documents this exact caveat); confirmed directly in a real
  // browser that dev mode logs a blocked-eval console error without
  // this. Neither React nor Next.js call eval() in a production
  // build, so this never widens the policy actually shipped.
  const isDev = process.env.NODE_ENV === 'development';
  return [
    "default-src 'self'",
    // 'wasm-unsafe-eval', not 'unsafe-eval' — the viewer's own WASM
    // zstd decoder (lib/zstd.ts, CO-403) needs WebAssembly.instantiate,
    // which a strict script-src blocks by default same as any other
    // eval-family call (confirmed by a real browser test: without
    // this, the whole session viewer's <main> failed to render).
    // This allows exactly that one WASM-compile operation, not plain
    // eval()/new Function() — except in dev, per the isDev note above.
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic' 'wasm-unsafe-eval'${isDev ? " 'unsafe-eval'" : ''}`,
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: https:",
    `connect-src 'self' ${supabaseURL}`,
    "font-src 'self'",
    "object-src 'none'",
    "base-uri 'self'",
    "frame-ancestors 'none'",
    "form-action 'self'",
  ].join('; ');
}

function withSecurityHeaders(response: NextResponse, cspHeaderValue: string): NextResponse {
  response.headers.set('Content-Security-Policy', cspHeaderValue);
  response.headers.set('X-Content-Type-Options', 'nosniff');
  response.headers.set('Referrer-Policy', 'strict-origin-when-cross-origin');
  response.headers.set('X-Frame-Options', 'DENY');
  return response;
}

export const config = {
  matcher: ['/((?!_next/static|_next/image|favicon.ico).*)'],
};
