import { describe, expect, it } from 'vitest';
import { isCompatClean } from './compat';

const ALL_SIX = [
  { os: 'macos-latest', versionLabel: 'pinned' },
  { os: 'macos-latest', versionLabel: 'latest' },
  { os: 'ubuntu-latest', versionLabel: 'pinned' },
  { os: 'ubuntu-latest', versionLabel: 'latest' },
  { os: 'windows-latest', versionLabel: 'pinned' },
  { os: 'windows-latest', versionLabel: 'latest' },
];

describe('isCompatClean', () => {
  it('is false with no passing runs at all', () => {
    expect(isCompatClean([])).toBe(false);
  });

  it('is true once every os/versionLabel combination has a passing run', () => {
    expect(isCompatClean(ALL_SIX)).toBe(true);
  });

  it('is false when exactly one combination is missing', () => {
    const missingOne = ALL_SIX.filter(
      (r) => !(r.os === 'windows-latest' && r.versionLabel === 'latest'),
    );
    expect(isCompatClean(missingOne)).toBe(false);
  });

  it('does not double-count a duplicate passing row for the same combination', () => {
    const withDuplicate = [...ALL_SIX, ALL_SIX[0]];
    expect(isCompatClean(withDuplicate)).toBe(true);
  });

  it('ignores an unrelated extra os/versionLabel pair', () => {
    const withExtra = [...ALL_SIX, { os: 'amiga-latest', versionLabel: 'latest' }];
    expect(isCompatClean(withExtra)).toBe(true);
  });
});
