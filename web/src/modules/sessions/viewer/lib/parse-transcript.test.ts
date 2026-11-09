import { describe, expect, it } from 'vitest';
import { parseTranscript, parseTranscriptLine } from './parse-transcript';

describe('parseTranscriptLine', () => {
  it('parses a plain-string user message', () => {
    const entry = parseTranscriptLine(
      JSON.stringify({
        type: 'user',
        uuid: 'u1',
        message: { role: 'user', content: 'fix the payment webhook' },
      }),
    );
    expect(entry).toEqual({
      kind: 'message',
      role: 'user',
      uuid: 'u1',
      blocks: [{ type: 'text', text: 'fix the payment webhook' }],
    });
  });

  it('parses an assistant message with text + tool_use blocks', () => {
    const entry = parseTranscriptLine(
      JSON.stringify({
        type: 'assistant',
        uuid: 'a1',
        message: {
          role: 'assistant',
          content: [
            { type: 'text', text: 'I will edit the file.' },
            {
              type: 'tool_use',
              id: 'toolu_1',
              name: 'Edit',
              input: { file_path: '/x/webhook.ts', old_string: 'a', new_string: 'b' },
            },
          ],
        },
      }),
    );
    expect(entry.kind).toBe('message');
    if (entry.kind !== 'message') throw new Error('expected message');
    expect(entry.blocks).toHaveLength(2);
    expect(entry.blocks[1]).toMatchObject({ type: 'tool_use', name: 'Edit' });
  });

  it('parses a tool_result with nested content blocks', () => {
    const entry = parseTranscriptLine(
      JSON.stringify({
        type: 'user',
        message: {
          role: 'user',
          content: [
            {
              type: 'tool_result',
              tool_use_id: 'toolu_1',
              is_error: false,
              content: [{ type: 'text', text: 'ok' }],
            },
          ],
        },
      }),
    );
    expect(entry.kind).toBe('message');
    if (entry.kind !== 'message') throw new Error('expected message');
    const block = entry.blocks[0];
    expect(block.type).toBe('tool_result');
    if (block.type !== 'tool_result') throw new Error('expected tool_result');
    expect(block.isError).toBe(false);
    expect(block.content).toEqual([{ type: 'text', text: 'ok' }]);
  });

  it('parses a summary line', () => {
    const entry = parseTranscriptLine(JSON.stringify({ type: 'summary', summary: 'Did stuff' }));
    expect(entry).toEqual({ kind: 'summary', uuid: null, summary: 'Did stuff' });
  });

  it('falls back to unknown for invalid JSON', () => {
    const entry = parseTranscriptLine('not json at all {');
    expect(entry.kind).toBe('unknown');
  });

  it('falls back to unknown for a future/unrecognized line shape', () => {
    const entry = parseTranscriptLine(JSON.stringify({ type: 'some_future_type', foo: 'bar' }));
    expect(entry.kind).toBe('unknown');
  });

  it('falls back to unknown for a malformed message (missing role)', () => {
    const entry = parseTranscriptLine(JSON.stringify({ type: 'user', message: { content: 'hi' } }));
    expect(entry.kind).toBe('unknown');
  });
});

describe('parseTranscript', () => {
  it('splits newline-delimited JSON and skips the trailing empty line', () => {
    const text =
      JSON.stringify({ type: 'user', message: { role: 'user', content: 'hi' } }) +
      '\n' +
      JSON.stringify({ type: 'summary', summary: 's' }) +
      '\n';
    const entries = parseTranscript(text);
    expect(entries).toHaveLength(2);
    expect(entries[0].kind).toBe('message');
    expect(entries[1].kind).toBe('summary');
  });
});
