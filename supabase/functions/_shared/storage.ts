// CO-305: every project shares this bucket unless it has its own
// dedicated one (projects.bucket) for a client whose contract requires
// physical separation. Creating a new dedicated bucket is a manual,
// one-time ops action — see the CO-305 storage migration's own
// comment — not something any Edge Function does automatically.
export const DEFAULT_BUCKET = "carryon-sessions";

/** Resolves which Storage bucket a project's bytes belong in. */
export function resolveBucket(
  project: { bucket: string | null } | null | undefined,
): string {
  return project?.bucket ?? DEFAULT_BUCKET;
}
