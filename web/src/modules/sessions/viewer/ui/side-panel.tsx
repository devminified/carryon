'use client';

import { useState } from 'react';
import { formatBytes } from '@/shared/lib/format/bytes';
import { formatRelativeTime } from '@/shared/lib/format/date';
import { useLiveNow } from '@/shared/hooks/use-live-now';
import { Button } from '@/shared/ui/button';
import { OutcomeBadge } from '../../ui/outcome-badge';
import { FamilyTree } from './family-tree';
import { RenameControl } from './rename-control';
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
  canRename,
  now: initialNow,
}: {
  session: SessionTranscriptMeta;
  familyNodes: FamilyNode[];
  danglingParentId: string | null;
  // CO-405: the owner or a project admin — see the page's own
  // canRename computation. RLS is still the real enforcement for the
  // rename itself.
  canRename: boolean;
  // CO-601: server-computed timestamp — see use-live-now.ts's own
  // comment for why this can't be computed client-side in this
  // component without risking a hydration mismatch.
  now: string;
}) {
  const [copied, setCopied] = useState(false);
  const pullCommand = `carryon pull ${session.id}`;
  const now = useLiveNow(initialNow);

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
        {canRename ? (
          <RenameControl sessionId={session.id} title={session.title} />
        ) : (
          <h2 className="text-foreground text-sm font-semibold">
            {session.title ?? '(untitled session)'}
          </h2>
        )}
        {session.summary && (
          <>
            <p className="text-muted-foreground mt-1 text-sm">{session.summary}</p>
            {session.summarizedAt && (
              <p className="text-muted-foreground/70 mt-0.5 text-xs">
                AI-generated · {formatRelativeTime(session.summarizedAt, now)}
              </p>
            )}
          </>
        )}
      </div>

      <dl className="flex flex-col gap-3">
        <Row label="Developer">{session.ownerLogin ?? '—'}</Row>
        <Row label="Branch">{session.branch ?? '—'}</Row>
        <Row label="Status">{session.status}</Row>
        <Row label="Outcome">
          {session.outcome
            ? <OutcomeBadge outcome={session.outcome} blockedReason={session.blockedReason} />
            : '—'}
        </Row>
        <Row label="Last activity">{formatRelativeTime(session.lastActivityAt, now)}</Row>
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

      <FamilyTree nodes={familyNodes} danglingParentId={danglingParentId} now={now} />

      <div>
        <p className="text-muted-foreground mb-1 text-xs">Resume this session</p>
        <Button
          type="button"
          variant="outline"
          onClick={copyPullCommand}
          className="h-auto w-full justify-start truncate px-2 py-1 font-mono text-xs"
        >
          {copied ? 'Copied!' : pullCommand}
        </Button>
      </div>
    </aside>
  );
}
