import type { SessionOutcome } from '../types';

const LABEL: Record<SessionOutcome, string> = {
  done: 'Done',
  in_progress: 'In progress',
  blocked: 'Blocked',
};

const CLASS: Record<SessionOutcome, string> = {
  done: 'bg-green-100 text-green-800',
  in_progress: 'bg-yellow-100 text-yellow-800',
  blocked: 'bg-red-100 text-red-800',
};

export function OutcomeBadge({ outcome }: { outcome: string | null }) {
  if (!outcome || !(outcome in LABEL)) return null;
  const o = outcome as SessionOutcome;
  return <span className={`rounded px-2 py-0.5 text-xs font-medium ${CLASS[o]}`}>{LABEL[o]}</span>;
}
