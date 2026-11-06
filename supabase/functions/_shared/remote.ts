// A light sanity check on an already-canonical git remote — not a
// re-implementation of internal/project.NormalizeRemote. Deliberately not
// duplicated: two independent URL parsers (Go and TypeScript) drifting
// apart over time is a worse bug than trusting the CLI's normalization
// and only checking the *shape* here. CO-104's ticket scope is "returns
// the project ID for a canonical remote" — the caller normalizes, this
// just rejects obviously-wrong input.
export function isCanonicalRemote(remote: string): boolean {
  if (!remote || remote !== remote.toLowerCase()) return false;
  if (remote.includes("://") || remote.includes("@")) return false;
  if (remote.startsWith("/") || remote.endsWith("/")) return false;
  if (remote.endsWith(".git")) return false;
  const slash = remote.indexOf("/");
  if (slash <= 0 || slash === remote.length - 1) return false;
  return true;
}
