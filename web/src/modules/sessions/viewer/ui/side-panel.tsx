'use client';

import { useState } from 'react';
import { formatBytes } from '@/shared/lib/format/bytes';
import { formatRelativeTime } from '@/shared/lib/format/date';
import { OutcomeBadge } from '../../ui/outcome-badge';
import { FamilyTree } from './family-tree';
import type { FamilyNode, SessionTranscriptMeta } from '../types';

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="text-foreground text-sm">{children}</dd>
    </div>
  );
}

export function SidePanel({
  session,
  familyNodes,
  danglingParentId,
}: {
  session: SessionTranscriptMeta;
  familyNodes: FamilyNode[];
  danglingParentId: string | null;
}) {
  const [copied, setCopied] = useState(false);
  const pullCommand = `carryon pull ${session.id}`;

  async function copyPullCommand() {
    try {
      await navigator.clipboard.writeText(pullCommand);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // clipboard access can be denied (permissions, non-secure
      // context) — the command is still visible to copy by hand
    }
  }

  return (
    <aside className="border-border flex w-72 flex-shrink-0 flex-col gap-4 border-l pl-4">
      <div>
        <h2 className="text-foreground text-sm font-semibold">
          {session.title ?? '(untitled session)'}
        </h2>
        {session.summary && <p className="text-muted-foreground mt-1 text-sm">{session.summary}</p>}
      </div>

      <dl className="flex flex-col gap-3">
        <Row label="Developer">{session.ownerLogin ?? '—'}</Row>
        <Row label="Branch">{session.branch ?? '—'}</Row>
        <Row label="Status">{session.status}</Row>
        <Row label="Outcome">
          {session.outcome ? <OutcomeBadge outcome={session.outcome} /> : '—'}
        </Row>
        <Row label="Last activity">{formatRelativeTime(session.lastActivityAt)}</Row>
        <Row label="Size">{formatBytes(session.totalBytes)}</Row>
      </dl>

      {session.filesTouched.length > 0 && (
        <div>
          <p className="text-muted-foreground text-xs">Files touched</p>
          <ul className="mt-1 flex flex-col gap-0.5">
            {session.filesTouched.map((f) => (
              <li key={f} className="text-foreground truncate font-mono text-xs">
                {f}
              </li>
            ))}
          </ul>
        </div>
      )}

      <FamilyTree nodes={familyNodes} danglingParentId={danglingParentId} />

      <div>
        <p className="text-muted-foreground mb-1 text-xs">Resume this session</p>
        <button
          type="button"
          onClick={copyPullCommand}
          className="border-border bg-background text-foreground hover:bg-border/30 w-full truncate rounded border px-2 py-1 text-left font-mono text-xs"
        >
          {copied ? 'Copied!' : pullCommand}
        </button>
      </div>
    </aside>
  );
}
