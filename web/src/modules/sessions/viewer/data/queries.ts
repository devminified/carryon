import 'server-only';
import { notFound } from 'next/navigation';
import { callEdgeFunction, EdgeFunctionError } from '@/shared/lib/edge-function';
import type { SessionFamilyResponse, SessionTranscriptResponse } from '../types';

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

// get-session-family (CO-404) — same viewer-role floor, but writes no
// audit row of its own (the page's one get-session-transcript call
// already covers "opening the page" — this is supplementary tree data
// for the same open, not a second visit). A 404/403 here collapses to
// an empty tree rather than notFound(): by the time this is called,
// getSessionTranscript has already proven the session is real and
// viewable, so failing here would only mean a transient error, not
// "this page shouldn't exist."
export async function getSessionFamily(sessionId: string): Promise<SessionFamilyResponse> {
  try {
    return await callEdgeFunction<SessionFamilyResponse>('get-session-family', { sessionId });
  } catch (err) {
    if (err instanceof EdgeFunctionError && (err.status === 404 || err.status === 403)) {
      return { nodes: [], danglingParentId: null };
    }
    throw err;
  }
}
