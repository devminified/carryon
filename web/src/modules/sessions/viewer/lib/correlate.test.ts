import { describe, expect, it } from 'vitest';
import { buildToolResultMap, isPureToolResultMessage, visibleEntries } from './correlate';
import { parseTranscript } from './parse-transcript';

const TRANSCRIPT =
  [
    JSON.stringify({ type: 'user', uuid: 'u1', message: { role: 'user', content: 'hi' } }),
    JSON.stringify({
      type: 'assistant',
      uuid: 'a1',
      message: {
        role: 'assistant',
        content: [
          { type: 'text', text: 'reading the file' },
          { type: 'tool_use', id: 'toolu_1', name: 'Read', input: { file_path: '/x.ts' } },
        ],
      },
    }),
    JSON.stringify({
      type: 'user',
      uuid: 'u2',
      message: {
        role: 'user',
        content: [{ type: 'tool_result', tool_use_id: 'toolu_1', is_error: false, content: 'ok' }],
      },
    }),
  ].join('\n') + '\n';

describe('buildToolResultMap', () => {
  it('correlates a tool_use to its later tool_result by id', () => {
    const entries = parseTranscript(TRANSCRIPT);
    const map = buildToolResultMap(entries);
    expect(map.get('toolu_1')).toMatchObject({ type: 'tool_result', toolUseId: 'toolu_1' });
  });
});

describe('isPureToolResultMessage / visibleEntries', () => {
  it("hides a user message that's only a tool_result wrapper", () => {
    const entries = parseTranscript(TRANSCRIPT);
    const flags = entries.map(isPureToolResultMessage);
    expect(flags).toEqual([false, false, true]);

    const visible = visibleEntries(entries);
    expect(visible).toHaveLength(2);
  });

  it('keeps a user message that mixes real text with a tool_result', () => {
    const entries = parseTranscript(
      JSON.stringify({
        type: 'user',
        message: {
          role: 'user',
          content: [
            { type: 'text', text: 'also look at this' },
            { type: 'tool_result', tool_use_id: 'toolu_1', is_error: false, content: 'ok' },
          ],
        },
      }) + '\n',
    );
    expect(isPureToolResultMessage(entries[0])).toBe(false);
  });
});
