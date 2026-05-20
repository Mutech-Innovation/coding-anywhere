import { describe, expect, it } from 'vitest';

import {
  decodeConfigOptionUpdate,
  decodeToolCallUpdate,
  decodeUsageUpdate,
  extractChunkText,
} from './chat.js';

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

describe('decodeUsageUpdate', () => {
  it('maps ACP usage_update fields', () => {
    expect(
      decodeUsageUpdate({
        sessionUpdate: 'usage_update',
        used: 53000,
        size: 200000,
        cost: { amount: 0.045, currency: 'USD' },
      }),
    ).toEqual({
      used: 53000,
      size: 200000,
      cost: { amount: 0.045, currency: 'USD' },
    });
  });
});

describe('decodeConfigOptionUpdate', () => {
  it('maps ACP config_option_update fields', () => {
    expect(
      decodeConfigOptionUpdate({
        sessionUpdate: 'config_option_update',
        configOptions: [
          {
            id: 'model',
            name: 'Model',
            type: 'select',
            currentValue: 'model-2',
            options: [{ value: 'model-2', label: 'Model 2' }],
          },
        ],
      }),
    ).toEqual([
      {
        id: 'model',
        name: 'Model',
        type: 'select',
        currentValue: 'model-2',
        options: [{ value: 'model-2', label: 'Model 2' }],
      },
    ]);
  });
});
