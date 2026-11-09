import { describe, expect, it } from 'vitest';
import { diffCounts, summarizeToolCall } from './tool-summary';
import type { ContentBlock } from './parse-transcript';

function toolUse(
  name: string,
  input: Record<string, unknown>,
): Extract<ContentBlock, { type: 'tool_use' }> {
  return { type: 'tool_use', id: 'toolu_1', name, input };
}

describe('diffCounts', () => {
  it('counts added and removed lines', () => {
    const { added, removed } = diffCounts('a\nb\nc\n', 'a\nx\nc\n');
    expect(added).toBe(1);
    expect(removed).toBe(1);
  });
});

describe('summarizeToolCall', () => {
  it('summarizes Edit with a real diff count', () => {
    const summary = summarizeToolCall(
      toolUse('Edit', {
        file_path: '/x/webhook.ts',
        old_string: 'a\nb\n',
        new_string: 'a\nb\nc\nd\n',
      }),
      null,
    );
    expect(summary).toBe('Edited `webhook.ts` (+2 −0)');
  });

  it('summarizes a successful Bash call', () => {
    const summary = summarizeToolCall(toolUse('Bash', { command: 'npm test' }), {
      type: 'tool_result',
      toolUseId: 'toolu_1',
      content: '',
      isError: false,
    });
    expect(summary).toBe('Ran `npm test`');
  });

  it('summarizes a failed Bash call', () => {
    const summary = summarizeToolCall(toolUse('Bash', { command: 'npm test' }), {
      type: 'tool_result',
      toolUseId: 'toolu_1',
      content: '',
      isError: true,
    });
    expect(summary).toBe('Ran `npm test`, failed');
  });

  it('falls back to a generic summary for an unknown tool', () => {
    const summary = summarizeToolCall(toolUse('SomeFutureTool', {}), null);
    expect(summary).toBe('Ran `SomeFutureTool`');
  });
});
