import { describe, expect, it } from 'vitest';
import { sortItems } from './use-live-sessions';
import type { SessionListItem } from '../types';

function item(id: string, pinned: boolean, lastActivityAt: string): SessionListItem {
  return {
    id,
    projectId: 'p1',
    userId: 'u1',
    title: null,
    summary: null,
    branch: null,
    status: 'ended',
    outcome: null,
    lastActivityAt,
    totalBytes: 0,
    ownerLogin: null,
    pinned,
  };
}

describe('sortItems', () => {
  it('puts every pinned item before every unpinned item, regardless of activity time', () => {
    const items = [
      item('old-unpinned', false, '2026-01-01T00:00:00Z'),
      item('old-pinned', true, '2026-01-01T00:00:00Z'),
      item('new-unpinned', false, '2026-06-01T00:00:00Z'),
    ];
    const sorted = sortItems(items).map((i) => i.id);
    expect(sorted).toEqual(['old-pinned', 'new-unpinned', 'old-unpinned']);
  });

  it('orders within each pinned bucket by last_activity_at descending', () => {
    const items = [
      item('a', true, '2026-01-01T00:00:00Z'),
      item('b', true, '2026-03-01T00:00:00Z'),
      item('c', false, '2026-02-01T00:00:00Z'),
      item('d', false, '2026-05-01T00:00:00Z'),
    ];
    const sorted = sortItems(items).map((i) => i.id);
    expect(sorted).toEqual(['b', 'a', 'd', 'c']);
  });

  it('does not mutate the input array', () => {
    const items = [item('a', false, '2026-01-01T00:00:00Z'), item('b', true, '2026-01-01T00:00:00Z')];
    const original = [...items];
    sortItems(items);
    expect(items).toEqual(original);
  });
});
