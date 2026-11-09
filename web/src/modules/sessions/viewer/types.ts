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
