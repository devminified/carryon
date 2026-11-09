import { z } from 'zod';

// CO-405: the web app's own rename/pin writes. Mirrors rename-session's
// (the CLI's path to the same operation) own Zod schema — title's
// bounds in particular must match, or the two paths would silently
// accept different inputs for the same column.
export const renameSessionSchema = z.object({
  sessionId: z.string().uuid(),
  title: z.string().trim().min(1).max(200),
});
export type RenameSessionInput = z.infer<typeof renameSessionSchema>;

export const pinSessionSchema = z.object({
  sessionId: z.string().uuid(),
  pinned: z.boolean(),
});
export type PinSessionInput = z.infer<typeof pinSessionSchema>;
