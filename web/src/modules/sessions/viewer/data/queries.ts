import 'server-only';
import { notFound } from 'next/navigation';
import { callEdgeFunction, EdgeFunctionError } from '@/shared/lib/edge-function';
import type { SessionTranscriptResponse } from '../types';

// get-session-transcript (CO-403) gates at viewer role and writes the
// "view" audit row server-side on every real call — not idempotent,
// so this must be called exactly once per real page view, not memoized
// or called speculatively.
export async function getSessionTranscript(sessionId: string): Promise<SessionTranscriptResponse> {
  try {
    return await callEdgeFunction<SessionTranscriptResponse>('get-session-transcript', {
      sessionId,
    });
  } catch (err) {
    // 404 (no such session) and 403 (not even a viewer) both collapse
    // to the same not-found page — same "don't leak which IDs are
    // real" reasoning as getProjectForCurrentUser (CO-402).
    if (err instanceof EdgeFunctionError && (err.status === 404 || err.status === 403)) {
      notFound();
    }
    throw err;
  }
}
