import { describe, expect, it } from 'vitest';
import { matchesSessionListFilters } from './search-filters';
import type { SessionListItem } from '../types';

function item(overrides: Partial<SessionListItem> = {}): SessionListItem {
  return {
    id: 's1',
    projectId: 'p1',
    userId: 'u1',
    title: 'fix webhook',
    summary: null,
    branch: 'main',
    status: 'ended',
    outcome: 'done',
    blockedReason: null,
    lastActivityAt: '2026-06-15T12:00:00Z',
    totalBytes: 0,
    ownerLogin: 'ali',
    pinned: false,
    filesTouched: [],
    summarizedAt: null,
    ...overrides,
  };
}

describe('matchesSessionListFilters', () => {
  it('matches with no filters applied', () => {
    expect(matchesSessionListFilters(item(), {})).toBe(true);
  });

  it('rejects a developer filter that does not match the owner login', () => {
    expect(matchesSessionListFilters(item({ ownerLogin: 'ali' }), { dev: 'sam' })).toBe(false);
    expect(matchesSessionListFilters(item({ ownerLogin: 'ali' }), { dev: 'ali' })).toBe(true);
  });

  it('rejects a status filter mismatch', () => {
    expect(matchesSessionListFilters(item({ status: 'live' }), { status: 'ended' })).toBe(false);
  });

  it('rejects an outcome filter mismatch', () => {
    expect(matchesSessionListFilters(item({ outcome: 'blocked' }), { outcome: 'done' })).toBe(
      false,
    );
  });

  it('rejects a branch filter mismatch', () => {
    expect(matchesSessionListFilters(item({ branch: 'main' }), { branch: 'feature/x' })).toBe(
      false,
    );
  });

  it('rejects activity outside the from/to date range', () => {
    const i = item({ lastActivityAt: '2026-06-15T12:00:00Z' });
    expect(matchesSessionListFilters(i, { from: '2026-07-01T00:00:00Z' })).toBe(false);
    expect(matchesSessionListFilters(i, { to: '2026-06-01T00:00:00Z' })).toBe(false);
    expect(
      matchesSessionListFilters(i, { from: '2026-06-01T00:00:00Z', to: '2026-07-01T00:00:00Z' }),
    ).toBe(true);
  });

  it('matches when every active filter is satisfied at once', () => {
    const i = item({
      ownerLogin: 'ali',
      status: 'ended',
      outcome: 'done',
      branch: 'main',
      lastActivityAt: '2026-06-15T12:00:00Z',
    });
    expect(
      matchesSessionListFilters(i, {
        dev: 'ali',
        status: 'ended',
        outcome: 'done',
        branch: 'main',
        from: '2026-06-01T00:00:00Z',
        to: '2026-07-01T00:00:00Z',
      }),
    ).toBe(true);
  });
});
