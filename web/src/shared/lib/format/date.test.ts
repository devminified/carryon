import { describe, expect, it } from 'vitest';
import { formatRelativeTime } from './date';

describe('formatRelativeTime', () => {
  const now = new Date('2026-01-01T12:00:00Z');

  it('renders seconds for very recent activity', () => {
    expect(formatRelativeTime('2026-01-01T11:59:50Z', now)).toMatch(/second/);
  });

  it('renders minutes within the hour', () => {
    expect(formatRelativeTime('2026-01-01T11:50:00Z', now)).toMatch(/minute/);
  });

  it('renders hours within the day', () => {
    expect(formatRelativeTime('2026-01-01T06:00:00Z', now)).toMatch(/hour/);
  });

  it('renders days beyond that', () => {
    expect(formatRelativeTime('2025-12-29T12:00:00Z', now)).toMatch(/day/);
  });
});
