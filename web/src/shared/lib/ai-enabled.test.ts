import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';

// AI_ENABLED is a module-level const computed once from
// process.env.NEXT_PUBLIC_AI_ENABLED at import time (same as Next.js
// itself evaluates NEXT_PUBLIC_* at build time) — stubbing the env
// var and re-importing the module (resetModules) is what lets each
// case see its own value instead of whatever ran first.
describe('AI_ENABLED', () => {
  const ORIGINAL = process.env.NEXT_PUBLIC_AI_ENABLED;

  beforeEach(() => {
    vi.resetModules();
  });

  afterEach(() => {
    if (ORIGINAL === undefined) {
      delete process.env.NEXT_PUBLIC_AI_ENABLED;
    } else {
      process.env.NEXT_PUBLIC_AI_ENABLED = ORIGINAL;
    }
    vi.resetModules();
  });

  it('defaults to enabled when the env var is unset (every existing deployment)', async () => {
    delete process.env.NEXT_PUBLIC_AI_ENABLED;
    const { AI_ENABLED } = await import('./ai-enabled');
    expect(AI_ENABLED).toBe(true);
  });

  it('is disabled only when explicitly set to the string "false"', async () => {
    process.env.NEXT_PUBLIC_AI_ENABLED = 'false';
    const { AI_ENABLED } = await import('./ai-enabled');
    expect(AI_ENABLED).toBe(false);
  });

  it('stays enabled for any other value, including "true"', async () => {
    process.env.NEXT_PUBLIC_AI_ENABLED = 'true';
    const { AI_ENABLED } = await import('./ai-enabled');
    expect(AI_ENABLED).toBe(true);
  });
});
