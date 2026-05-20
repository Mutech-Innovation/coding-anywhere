import { describe, expect, it } from 'vitest';

import { decodeToolCallUpdate, extractChunkText } from './chat.js';

describe('extractChunkText', () => {
  it('returns inline text from an agent_message_chunk update', () => {
    expect(
      extractChunkText({
        sessionUpdate: 'agent_message_chunk',
        content: { type: 'text', text: 'hello' },
      }),
    ).toBe('hello');
  });

  it('returns null when content is missing or non-text', () => {
    expect(extractChunkText({})).toBeNull();
    expect(extractChunkText({ content: { type: 'image', data: 'AA==' } })).toBeNull();
  });
});

describe('decodeToolCallUpdate', () => {
  it('maps ACP tool_call fields onto the strongly-typed view', () => {
    const view = decodeToolCallUpdate({
      sessionUpdate: 'tool_call',
      toolCallId: 'call_123',
      title: 'Read package.json',
      kind: 'read',
      status: 'in_progress',
      rawInput: { path: 'package.json' },
      rawOutput: null,
      locations: [{ path: '/repo/package.json', line: 12 }],
      content: [{ type: 'content', content: { type: 'text', text: '{...}' } }],
    });
    expect(view).toEqual({
      toolCallId: 'call_123',
      title: 'Read package.json',
      kind: 'read',
      status: 'in_progress',
      rawInput: { path: 'package.json' },
      rawOutput: null,
      locations: [{ path: '/repo/package.json', line: 12 }],
      content: [{ type: 'content', content: { type: 'text', text: '{...}' } }],
    });
  });

  it('omits unset fields so they merge cleanly', () => {
    const view = decodeToolCallUpdate({
      sessionUpdate: 'tool_call_update',
      toolCallId: 'call_123',
      status: 'completed',
    });
    expect(view).toEqual({ toolCallId: 'call_123', status: 'completed' });
  });

  it('returns null without a toolCallId', () => {
    expect(decodeToolCallUpdate({ sessionUpdate: 'tool_call' })).toBeNull();
  });
});
