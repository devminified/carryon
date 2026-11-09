import type { AuditLogEntry } from '../types';

// A minimal, correct CSV writer — quotes a field only when it needs
// it (contains a comma, quote, or newline), doubling embedded quotes
// per RFC 4180. No library: this is the one place in the whole app
// that needs CSV, and the full escaping rule is three lines.
function csvField(value: string): string {
  if (/[",\n]/.test(value)) {
    return `"${value.replace(/"/g, '""')}"`;
  }
  return value;
}

const COLUMNS = ['at', 'action', 'actor', 'actorLogin', 'projectId', 'projectName', 'sessionId', 'reason'] as const;

export function auditLogToCsv(rows: AuditLogEntry[]): string {
  const header = COLUMNS.join(',');
  const lines = rows.map((row) =>
    [
      row.at,
      row.action,
      row.actor ?? '',
      row.actorLogin ?? '',
      row.project_id ?? '',
      row.projectName ?? '',
      row.session_id ?? '',
      row.reason ?? '',
    ]
      .map((v) => csvField(String(v)))
      .join(','),
  );
  return [header, ...lines].join('\n') + '\n';
}
