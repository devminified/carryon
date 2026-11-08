import { describe, expect, it } from 'vitest';
import { roleAtLeast } from './roles';

describe('roleAtLeast', () => {
  it('is true when the role meets the minimum', () => {
    expect(roleAtLeast('admin', 'viewer')).toBe(true);
    expect(roleAtLeast('resumer', 'resumer')).toBe(true);
  });

  it('is false when the role falls short of the minimum', () => {
    expect(roleAtLeast('viewer', 'admin')).toBe(false);
    expect(roleAtLeast('viewer', 'resumer')).toBe(false);
  });
});
