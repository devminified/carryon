import { describe, expect, it } from 'vitest';
import { formatMillis, formatRate, formatSeconds } from './format-observability';

describe('formatSeconds', () => {
  it('reports null as "No data yet", not 0s', () => {
    expect(formatSeconds(null)).toBe('No data yet');
  });

  it('formats under a minute in seconds', () => {
    expect(formatSeconds(45)).toBe('45s');
  });

  it('formats a minute or more in whole minutes', () => {
    expect(formatSeconds(125)).toBe('2m');
  });

  it('formats exactly zero as 0s, not "No data yet"', () => {
    expect(formatSeconds(0)).toBe('0s');
  });
});

describe('formatRate', () => {
  it('reports null as "No data yet", not 0%', () => {
    expect(formatRate(null)).toBe('No data yet');
  });

  it('formats a fraction as a one-decimal percentage', () => {
    expect(formatRate(0.1)).toBe('10.0%');
  });

  it('formats exactly zero as 0.0%, not "No data yet"', () => {
    expect(formatRate(0)).toBe('0.0%');
  });
});

describe('formatMillis', () => {
  it('reports null as "No data yet", not 0s', () => {
    expect(formatMillis(null)).toBe('No data yet');
  });

  it('formats under a minute (in ms) in seconds', () => {
    expect(formatMillis(45_000)).toBe('45s');
  });

  it('formats a minute or more (in ms) in whole minutes', () => {
    expect(formatMillis(125_000)).toBe('2m');
  });
});
