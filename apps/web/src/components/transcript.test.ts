import { describe, expect, it } from 'vitest';

import type { ChatMessage, ChatToolCall } from '../store/chat.js';
import {
  groupTranscript,
  isActiveToolCall,
  TOOL_GROUP_THRESHOLD,
  type TranscriptEntry,
} from './transcript.js';

function msg(id: string, order: number): TranscriptEntry {
  const message: ChatMessage = { id, role: 'agent', text: id, createdAt: 0, order };
  return { type: 'message', order, message };
}

function tool(id: string, order: number, status?: ChatToolCall['status']): TranscriptEntry {
  return { type: 'tool', order, call: { id, status, order } };
}

describe('groupTranscript', () => {
  it('returns messages and short tool runs as individual items', () => {
    const items = groupTranscript([msg('m1', 1), tool('t1', 2), tool('t2', 3), msg('m2', 4)]);
    expect(items.map((i) => i.type)).toEqual(['message', 'tool', 'tool', 'message']);
    expect(items.map((i) => i.key)).toEqual(['message-m1', 'tool-t1', 'tool-t2', 'message-m2']);
  });

  it('folds runs of >= TOOL_GROUP_THRESHOLD consecutive tool calls into a group', () => {
    const run = Array.from({ length: TOOL_GROUP_THRESHOLD }, (_, i) => tool(`t${i}`, i + 2));
    const items = groupTranscript([msg('m1', 1), ...run, msg('m2', 99)]);
    expect(items.map((i) => i.type)).toEqual(['message', 'tool_group', 'message']);
    const group = items[1]!;
    if (group.type !== 'tool_group') throw new Error('expected tool_group');
    expect(group.calls.map((c) => c.id)).toEqual(['t0', 't1', 't2']);
    // Key is derived from the first call, so the group keeps its React
    // identity (collapse state) while a streaming run appends more calls.
    expect(group.key).toBe('tool-group-t0');
  });

  it('breaks runs on any interleaved message', () => {
    const items = groupTranscript([
      tool('t1', 1),
      tool('t2', 2),
      msg('m1', 3),
      tool('t3', 4),
      tool('t4', 5),
      tool('t5', 6),
      tool('t6', 7),
    ]);
    expect(items.map((i) => i.type)).toEqual(['tool', 'tool', 'message', 'tool_group']);
  });

  it('groups a trailing run with no closing message', () => {
    const items = groupTranscript([msg('m1', 1), tool('t1', 2), tool('t2', 3), tool('t3', 4)]);
    expect(items.map((i) => i.type)).toEqual(['message', 'tool_group']);
  });

  it('handles an empty transcript', () => {
    expect(groupTranscript([])).toEqual([]);
  });
});

describe('isActiveToolCall', () => {
  it('treats pending/in_progress (and missing status) as active', () => {
    expect(isActiveToolCall({ id: 't', order: 1 })).toBe(true);
    expect(isActiveToolCall({ id: 't', order: 1, status: 'pending' })).toBe(true);
    expect(isActiveToolCall({ id: 't', order: 1, status: 'in_progress' })).toBe(true);
    expect(isActiveToolCall({ id: 't', order: 1, status: 'completed' })).toBe(false);
    expect(isActiveToolCall({ id: 't', order: 1, status: 'failed' })).toBe(false);
  });
});
