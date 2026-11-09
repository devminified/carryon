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
  let response = NextResponse.next({ request });

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
          response = NextResponse.next({ request });
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
    if (!project) return notFoundResponse();
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
    if (!session) return notFoundResponse();
  }

  return response;
}

function notFoundResponse(): NextResponse {
  return new NextResponse('This page could not be found.', {
    status: 404,
    headers: { 'content-type': 'text/plain; charset=utf-8' },
  });
}

export const config = {
  matcher: ['/((?!_next/static|_next/image|favicon.ico).*)'],
};
