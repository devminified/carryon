import { describe, expect, it } from 'vitest';
import { auditLogToCsv } from './csv';
import type { AuditLogEntry } from '../types';

function row(overrides: Partial<AuditLogEntry> = {}): AuditLogEntry {
  return {
    id: 1,
    action: 'view',
    actor: 'u1',
    at: '2026-06-15T12:00:00Z',
    ip: null,
    project_id: 'p1',
    reason: null,
    session_id: 's1',
    user_agent: null,
    actorLogin: 'ali',
    projectName: 'carryon',
    ...overrides,
  };
}

describe('auditLogToCsv', () => {
  it('writes a header row followed by one row per entry', () => {
    const csv = auditLogToCsv([row()]);
    const lines = csv.trim().split('\n');
    expect(lines).toHaveLength(2);
    expect(lines[0]).toBe('at,action,actor,actorLogin,projectId,projectName,sessionId,reason');
    expect(lines[1]).toBe('2026-06-15T12:00:00Z,view,u1,ali,p1,carryon,s1,');
  });

  it('quotes a field containing a comma', () => {
    const csv = auditLogToCsv([row({ reason: 'deleted, with a reason' })]);
    expect(csv).toContain('"deleted, with a reason"');
  });

  it('doubles an embedded quote and wraps the field in quotes', () => {
    const csv = auditLogToCsv([row({ reason: 'said "hello"' })]);
    expect(csv).toContain('"said ""hello"""');
  });

  it('quotes a field containing a newline', () => {
    const csv = auditLogToCsv([row({ reason: 'line one\nline two' })]);
    expect(csv).toContain('"line one\nline two"');
  });

  it('renders null fields as empty', () => {
    const csv = auditLogToCsv([row({ actor: null, actorLogin: null, project_id: null, projectName: null, session_id: null })]);
    const dataLine = csv.trim().split('\n')[1];
    expect(dataLine).toBe('2026-06-15T12:00:00Z,view,,,,,,');
  });

  it('is empty (header only) for zero rows', () => {
    const csv = auditLogToCsv([]);
    expect(csv.trim().split('\n')).toHaveLength(1);
  });
});
