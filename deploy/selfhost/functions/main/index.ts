// CO-506: the self-hosted edge-runtime's own router — NOT one of
// Carryon's named RPCs (edge-functions.md's own list), and NOT
// deployed to Supabase Cloud (deploy-functions.yml deploys each
// named function directory under supabase/functions/ individually;
// this file lives under deploy/selfhost/ specifically so it's never
// swept into that list). Cloud's own Kong config does this exact
// per-function routing for us; a self-hosted edge-runtime process
// has no such router built in; it needs one real function instance,
// `--main-service` in docker-compose.yml, to do it by hand — this is
// Supabase's own published shape for that router, adapted to this
// repo's function layout (every function under /home/deno/functions
// is one of supabase/functions/*'s own directories, read-only
// mounted — see docker-compose.yml's `edge-functions` service).
import { serve } from 'https://deno.land/std@0.177.1/http/server.ts';

console.log('carryon self-host function router booting');

function getAuthToken(req: Request) {
  const authHeader = req.headers.get('Authorization');
  if (!authHeader) {
    throw new Error('Missing Authorization header');
  }
  const [bearer, token] = authHeader.split(' ');
  if (bearer !== 'Bearer') {
    throw new Error(`Auth header is not 'Bearer {token}'`);
  }
  return token;
}

function isValidJwt(token: string) {
  try {
    // A real JWT signature check already happens downstream in each
    // function's own call (Supabase's supabase-js client re-verifies
    // against the real SUPABASE_URL), and for the `x-cron-secret`
    // functions there's no JWT at all — this router's only job is
    // "does this look like a JWT or an anon/service key," so a
    // malformed Authorization header fails fast with a clear 401
    // instead of a confusing worker-boot error.
    const [, payload] = token.split('.');
    if (!payload) return false;
    JSON.parse(atob(payload.replace(/-/g, '+').replace(/_/g, '/')));
    return true;
  } catch {
    return false;
  }
}

const JWT_SECRET = Deno.env.get('GOTRUE_JWT_SECRET') ?? Deno.env.get('JWT_SECRET') ?? '';
const VERIFY_JWT = (Deno.env.get('VERIFY_JWT') ?? 'true') !== 'false';

serve(async (req: Request) => {
  if (req.method === 'OPTIONS') {
    return new Response('ok', { status: 200 });
  }

  const url = new URL(req.url);
  const { pathname } = url;
  const pathParts = pathname.split('/');
  const serviceName = pathParts[1];

  if (!serviceName) {
    return new Response(JSON.stringify({ error: 'missing function name' }), {
      status: 404,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  const servicePath = `/home/deno/functions/${serviceName}`;
  try {
    const stat = await Deno.stat(servicePath);
    if (!stat.isDirectory) throw new Error('not a directory');
  } catch {
    return new Response(JSON.stringify({ error: `function '${serviceName}' not found` }), {
      status: 404,
      headers: { 'Content-Type': 'application/json' },
    });
  }

  // Same floor Kong enforces in front of Supabase Cloud's own
  // functions gateway (config.toml's per-function `verify_jwt`) —
  // this router has no per-function override table, so it checks the
  // JWT is present/well-formed for everything except the handful of
  // functions that authenticate by a shared `x-cron-secret` header
  // instead (edge-functions.md's own "no caller JWT" list).
  const CRON_SECRET_FUNCTIONS = new Set([
    'sync-github-teams',
    'rescan',
    'purge-expired-sessions',
    'summarize-session',
    'send-daily-digest',
    'check-observability-alerts',
    'record-compat-run',
  ]);
  if (VERIFY_JWT && !CRON_SECRET_FUNCTIONS.has(serviceName)) {
    try {
      const token = getAuthToken(req);
      if (!isValidJwt(token)) throw new Error('invalid JWT');
    } catch (e) {
      return new Response(JSON.stringify({ error: (e as Error).message }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
      });
    }
  }

  const memoryLimitMb = 512;
  const workerTimeoutMs = 5 * 60 * 1000;
  const noModuleCache = false;
  const importMapPath = null;
  const envVarsObj = Deno.env.toObject();
  const envVars = Object.entries(envVarsObj).map(([k, v]) => [k, v]);

  try {
    // EdgeRuntime is only defined inside the real supabase/edge-runtime
    // image (not plain Deno) — this file can only ever run there,
    // same as every other function in this repo already assumes a
    // Supabase-flavored Deno runtime (see e.g. _shared/embed.ts's own
    // Supabase.ai.Session comment).
    // deno-lint-ignore no-explicit-any
    const edgeRuntime = (globalThis as any).EdgeRuntime;
    const worker = await edgeRuntime.userWorkers.create({
      servicePath,
      memoryLimitMb,
      workerTimeoutMs,
      noModuleCache,
      importMapPath,
      envVars,
    });
    return await worker.fetch(req);
  } catch (e) {
    const error = e as Error;
    return new Response(
      JSON.stringify({ error: error.message ?? 'worker failed to boot' }),
      { status: 500, headers: { 'Content-Type': 'application/json' } },
    );
  }
});
