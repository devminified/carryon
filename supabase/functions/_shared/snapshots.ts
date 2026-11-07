// Shared between sign-snapshot-bundle and register-snapshot (CO-202) so
// the bundle's object key formula lives in exactly one place —
// register-snapshot derives it itself from the session's own stored
// project_id/user_id rather than trusting a client-supplied key, the
// same reasoning confirm-chunk already applies to chunk object keys
// (see _shared/chunks.ts).

export function bundleObjectKey(
  projectId: string,
  userId: string,
  sessionId: string,
  seq: number,
): string {
  return `${projectId}/${userId}/${sessionId}/code-${seq}.bundle`;
}
