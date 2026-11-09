import type { ManifestChunk } from './lib/fetch-transcript';

export type SessionTranscriptMeta = {
  id: string;
  projectId: string;
  projectName: string;
  ownerLogin: string | null;
  originalCwd: string;
  branch: string | null;
  status: string;
  outcome: string | null;
  title: string | null;
  summary: string | null;
  filesTouched: string[];
  parentSessionId: string | null;
  totalBytes: number;
  startedAt: string;
  lastActivityAt: string;
};

export type SessionFork = {
  id: string;
  title: string | null;
  startedAt: string;
};

export type SessionTranscriptResponse = {
  session: SessionTranscriptMeta;
  chunks: ManifestChunk[];
  forks: SessionFork[];
};

// CO-404: one node in the fork family tree. An inaccessible node
// (get-session-family's own real per-node RLS check) carries only id/
// parentId — never title/ownerLogin/startedAt, matching the ticket's
// "without details" wording.
export type FamilyNode =
  | { id: string; parentId: string | null; accessible: false }
  | {
      id: string;
      parentId: string | null;
      accessible: true;
      title: string | null;
      ownerLogin: string | null;
      startedAt: string;
      isCurrent: boolean;
    };

export type SessionFamilyResponse = {
  nodes: FamilyNode[];
  danglingParentId: string | null;
};
