import { describe, expect, it } from 'vitest';
import { pinSessionSchema, renameSessionSchema } from './schemas';

describe('renameSessionSchema', () => {
  it('trims the title', () => {
    const result = renameSessionSchema.parse({
      sessionId: '11111111-1111-4111-8111-111111111111',
      title: '  fix payments retry  ',
    });
    expect(result.title).toBe('fix payments retry');
  });

  it('rejects an all-whitespace title', () => {
    expect(() =>
      renameSessionSchema.parse({
        sessionId: '11111111-1111-4111-8111-111111111111',
        title: '   ',
      }),
    ).toThrow();
  });

  it('rejects a title over 200 characters', () => {
    expect(() =>
      renameSessionSchema.parse({
        sessionId: '11111111-1111-4111-8111-111111111111',
        title: 'x'.repeat(201),
      }),
    ).toThrow();
  });

  it('accepts a title at exactly 200 characters', () => {
    const title = 'x'.repeat(200);
    const result = renameSessionSchema.parse({
      sessionId: '11111111-1111-4111-8111-111111111111',
      title,
    });
    expect(result.title).toBe(title);
  });

  it('rejects a non-uuid sessionId', () => {
    expect(() =>
      renameSessionSchema.parse({ sessionId: 'not-a-uuid', title: 'fix payments' }),
    ).toThrow();
  });
});

describe('pinSessionSchema', () => {
  it('accepts a real boolean pinned value', () => {
    const result = pinSessionSchema.parse({
      sessionId: '11111111-1111-4111-8111-111111111111',
      pinned: true,
    });
    expect(result.pinned).toBe(true);
  });

  it('rejects a non-boolean pinned value', () => {
    expect(() =>
      pinSessionSchema.parse({
        sessionId: '11111111-1111-4111-8111-111111111111',
        pinned: 'true',
      }),
    ).toThrow();
  });
});
