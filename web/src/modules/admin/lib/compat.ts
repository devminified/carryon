// CO-503: "admins can't move the pinned version (CO-308) to a version
// without a green run" — pure matching logic kept out of
// data/actions.ts (which imports the admin Supabase client) so it
// stays unit-testable without mocking a database round trip. The
// caller fetches claude_compat_runs rows for one carryon_version and
// hands them here; this just checks whether every expected (os,
// versionLabel) combination the nightly workflow's own matrix covers
// has a passing row among them.
export const COMPAT_OSES = ['macos-latest', 'ubuntu-latest', 'windows-latest'] as const;
export const COMPAT_VERSION_LABELS = ['pinned', 'latest'] as const;

export type CompatRunRow = { os: string; versionLabel: string };

export function isCompatClean(passedRuns: CompatRunRow[]): boolean {
  const seen = new Set(passedRuns.map((r) => `${r.os}:${r.versionLabel}`));
  return COMPAT_OSES.every((os) => COMPAT_VERSION_LABELS.every((label) => seen.has(`${os}:${label}`)));
}
