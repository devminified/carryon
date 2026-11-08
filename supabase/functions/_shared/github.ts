// CO-303: GitHub App auth and team membership reads for
// sync-github-teams. A GitHub App (not the OAuth App auth.md documents
// for login) — one-time external setup, see auth.md's "GitHub App
// setup" section. Every team is addressed by its stable numeric ID,
// never its slug, so a team rename never breaks the mapping — there's
// nothing here that could even notice a rename.

import { createPrivateKey, createSign } from "node:crypto";

const GITHUB_API = "https://api.github.com";

// GitHub App JWTs are capped at 10 minutes; this is comfortably inside
// that and inside the 15-minute sync interval, so a fresh App JWT (and
// a fresh installation token from it) is minted on every run rather
// than cached across cold starts.
const APP_JWT_TTL_SECONDS = 600;

export interface GitHubTeamMember {
  id: number;
  login: string;
}

function base64url(input: string | Uint8Array): string {
  const bytes = typeof input === "string"
    ? new TextEncoder().encode(input)
    : input;
  return btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

/** Signs a short-lived GitHub App JWT (RS256, iss = the App's own ID)
 * from the App's PEM private key — the first step of the standard
 * GitHub App → installation token exchange.
 *
 * Uses node:crypto directly rather than a JWT library: GitHub's own
 * "Generate a private key" button downloads a PKCS#1 ("RSA PRIVATE
 * KEY") PEM, and jose's importPKCS8 flatly rejects that format
 * (verified directly — "must be PKCS#8 formatted string"), which would
 * otherwise force every operator to manually convert the key with
 * openssl before it's usable. node:crypto's createPrivateKey accepts
 * PKCS#1 natively (confirmed against a real downloaded key), so the
 * key GitHub actually hands you just works, unmodified. */
export function signAppJWT(appId: string, privateKeyPEM: string): string {
  const now = Math.floor(Date.now() / 1000);
  const header = { alg: "RS256", typ: "JWT" };
  const payload = {
    iat: now - 60, // GitHub's own documented clock-skew allowance
    exp: now + APP_JWT_TTL_SECONDS,
    iss: appId,
  };
  const signingInput = `${base64url(JSON.stringify(header))}.${
    base64url(JSON.stringify(payload))
  }`;

  const key = createPrivateKey(privateKeyPEM);
  const signer = createSign("RSA-SHA256");
  signer.update(signingInput);
  signer.end();
  const signature = signer.sign(key);

  return `${signingInput}.${base64url(new Uint8Array(signature))}`;
}

/** Exchanges an App JWT for a short-lived (~1h) installation access
 * token, scoped to whatever the App's installation on this org was
 * granted — team-membership read access, nothing more. */
export async function getInstallationToken(
  appJWT: string,
  installationId: string,
): Promise<string> {
  const res = await fetch(
    `${GITHUB_API}/app/installations/${installationId}/access_tokens`,
    {
      method: "POST",
      headers: {
        Authorization: `Bearer ${appJWT}`,
        Accept: "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
      },
    },
  );
  if (!res.ok) {
    throw new Error(
      `getInstallationToken: GitHub returned ${res.status}: ${await res
        .text()}`,
    );
  }
  const body = await res.json();
  return body.token as string;
}

/** Lists every member of a team, by the team's stable numeric ID —
 * never its slug, so a rename can't break this. Paginates to
 * completion (GitHub's default page size is 30; teams can have more
 * members than that). */
export async function listTeamMembers(
  installationToken: string,
  teamId: string,
): Promise<GitHubTeamMember[]> {
  const members: GitHubTeamMember[] = [];
  let page = 1;
  for (;;) {
    const res = await fetch(
      `${GITHUB_API}/teams/${teamId}/members?per_page=100&page=${page}`,
      {
        headers: {
          Authorization: `Bearer ${installationToken}`,
          Accept: "application/vnd.github+json",
          "X-GitHub-Api-Version": "2022-11-28",
        },
      },
    );
    if (!res.ok) {
      throw new Error(
        `listTeamMembers(${teamId}): GitHub returned ${res.status}: ${await res
          .text()}`,
      );
    }
    const body = await res.json() as { id: number; login: string }[];
    for (const m of body) members.push({ id: m.id, login: m.login });
    if (body.length < 100) break;
    page++;
  }
  return members;
}

/** The full App → installation token → team members chain, using the
 * App's own env-provided credentials. This is what sync-github-teams'
 * real Deps.listTeamMembers wraps; tests inject a fake instead (see
 * index.test.ts) — there's no live GitHub App to call from CI. */
export async function realListTeamMembers(
  teamId: string,
): Promise<GitHubTeamMember[]> {
  const appId = Deno.env.get("GITHUB_APP_ID")!;
  const privateKey = Deno.env.get("GITHUB_APP_PRIVATE_KEY")!;
  const installationId = Deno.env.get("GITHUB_APP_INSTALLATION_ID")!;

  const appJWT = signAppJWT(appId, privateKey);
  const installationToken = await getInstallationToken(
    appJWT,
    installationId,
  );
  return await listTeamMembers(installationToken, teamId);
}
