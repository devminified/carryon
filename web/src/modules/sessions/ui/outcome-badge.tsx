import { Badge } from '@/shared/ui/badge';
import type { SessionOutcome } from '../types';

const LABEL: Record<SessionOutcome, string> = {
  done: 'Done',
  in_progress: 'In progress',
  blocked: 'Blocked',
};

// Badge's own variants (secondary/outline/destructive/...) don't carry
// a semantic green/yellow/red distinction — these three outcomes
// always have, so the original colors are kept via className rather
// than collapsed onto a variant that would lose the distinction.
const CLASS: Record<SessionOutcome, string> = {
  done: 'bg-success/10 text-success dark:bg-success/20',
  in_progress: 'bg-warning/10 text-warning dark:bg-warning/20',
  blocked: 'bg-destructive/10 text-destructive dark:bg-destructive/20',
};

export function OutcomeBadge({
  outcome,
  blockedReason,
}: {
  outcome: string | null;
  // CO-408: a native tooltip is enough here — this badge already
  // appears in the dense session-table row, so a second, always-
  // visible line of text isn't worth the layout cost for the one
  // outcome value that has extra context to show.
  blockedReason?: string | null;
}) {
  if (!outcome || !(outcome in LABEL)) return null;
  const o = outcome as SessionOutcome;
  return (
    <Badge
      className={CLASS[o]}
      title={o === 'blocked' ? (blockedReason ?? undefined) : undefined}
    >
      {LABEL[o]}
    </Badge>
  );
}
