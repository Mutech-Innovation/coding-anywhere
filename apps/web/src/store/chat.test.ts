import { afterEach, describe, expect, it } from 'vitest';

import { useChat } from './chat.js';

const baseSession = {
  id: 'sess-1',
  agent_id: 'devin',
  native_id: 'acp-sess-1',
  cwd: '/tmp',
  status: 'idle' as const,
  started_at: new Date(0).toISOString(),
};

function seedSession() {
  useChat.setState({
    attached: false,
    agents: [],
    activeId: 'sess-1',
    lastError: null,
    sessions: {
      'sess-1': {
        session: { ...baseSession },
        messages: [],
        toolCalls: {},
        toolCallOrder: 0,
        plan: null,
        modes: [],
        currentModeId: null,
        availableCommands: [],
        permissions: {},
        liveAgentMessageId: null,
      },
    },
  });
}

// Captured chat-channel handler from the most recent attach() call.
let chatHandler: ((env: unknown) => void) | null = null;

function attachOnce() {
  if (chatHandler) return;
  const fakeClient = {
    handle: (_: string, handler: (env: unknown) => void) => {
      chatHandler = handler;
    },
    send: () => undefined,
  };
  useChat.getState().attach(fakeClient as never);
}

function pushUpdate(update: Record<string, unknown>) {
  attachOnce();
  chatHandler?.({
    type: 'event',
    method: 'update',
    payload: { session_id: 'sess-1', update },
  });
}

describe('chat store ingestUpdate', () => {
  afterEach(() => seedSession());

  it('stores ACP tool_call fields under the strongly-typed shape', () => {
    seedSession();
    pushUpdate({
      sessionUpdate: 'tool_call',
      toolCallId: 'call_abc',
      title: 'Read package.json',
      kind: 'read',
      status: 'in_progress',
      rawInput: { path: 'package.json' },
      locations: [{ path: '/repo/package.json', line: 12 }],
      content: [{ type: 'content', content: { type: 'text', text: '{...}' } }],
    });
    const tc = useChat.getState().sessions['sess-1']!.toolCalls['call_abc']!;
    expect(tc.title).toBe('Read package.json');
    expect(tc.kind).toBe('read');
    expect(tc.status).toBe('in_progress');
    expect((tc.rawInput as { path: string }).path).toBe('package.json');
    expect(tc.locations?.[0]?.path).toBe('/repo/package.json');
    expect(tc.order).toBe(1);
  });

  it('merges tool_call_update fields without losing prior values', () => {
    seedSession();
    pushUpdate({
      sessionUpdate: 'tool_call',
      toolCallId: 'call_abc',
      title: 'Read package.json',
      kind: 'read',
      status: 'in_progress',
      rawInput: { path: 'package.json' },
    });
    pushUpdate({
      sessionUpdate: 'tool_call_update',
      toolCallId: 'call_abc',
      status: 'completed',
      rawOutput: { ok: true },
    });
    const tc = useChat.getState().sessions['sess-1']!.toolCalls['call_abc']!;
    expect(tc.status).toBe('completed');
    expect(tc.title).toBe('Read package.json'); // not lost
    expect((tc.rawOutput as { ok: boolean }).ok).toBe(true);
  });

  it('replaces the plan from a plan update', () => {
    seedSession();
    pushUpdate({
      sessionUpdate: 'plan',
      entries: [
        { content: 'Read repo', priority: 'medium', status: 'in_progress' },
        { content: 'Write fix', priority: 'high', status: 'pending' },
      ],
    });
    const plan = useChat.getState().sessions['sess-1']!.plan!;
    expect(plan).toHaveLength(2);
    expect(plan[1]?.priority).toBe('high');
  });

  it('updates currentModeId on current_mode_update', () => {
    seedSession();
    pushUpdate({ sessionUpdate: 'current_mode_update', currentModeId: 'code' });
    expect(useChat.getState().sessions['sess-1']!.currentModeId).toBe('code');
  });

  it('captures available commands', () => {
    seedSession();
    pushUpdate({
      sessionUpdate: 'available_commands_update',
      availableCommands: [
        { name: 'plan', description: 'Plan' },
        { name: 'web', description: 'Search', input: { hint: 'query' } },
      ],
    });
    const cmds = useChat.getState().sessions['sess-1']!.availableCommands;
    expect(cmds.map((c) => c.name)).toEqual(['plan', 'web']);
  });
});
