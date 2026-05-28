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
        transcriptOrder: 0,
        plan: null,
        modes: [],
        currentModeId: null,
        availableCommands: [],
        usage: null,
        configOptions: [],
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

function pushSessionCreated(session: Record<string, unknown>) {
  attachOnce();
  chatHandler?.({
    type: 'event',
    method: 'session_created',
    payload: { session },
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

  it('stores usage and config updates without rendering debug messages', () => {
    seedSession();
    pushUpdate({
      sessionUpdate: 'usage_update',
      used: 53000,
      size: 200000,
      cost: { amount: 0.045, currency: 'USD' },
    });
    pushUpdate({
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
    });

    const state = useChat.getState().sessions['sess-1']!;
    expect(state.messages).toHaveLength(0);
    expect(state.usage).toEqual({
      used: 53000,
      size: 200000,
      cost: { amount: 0.045, currency: 'USD' },
    });
    expect(state.configOptions[0]?.id).toBe('model');
    expect(state.configOptions[0]?.currentValue).toBe('model-2');
  });

  it('drops "None" agent messages emitted by Devin between thought and tool calls', () => {
    // Devin's swe-1-6-fast model sometimes ends an assistant turn with
    // content == "None" (Python's None str-ified) when the LLM picked
    // only tool_calls; we don't want bare "None" bubbles in the
    // transcript.
    seedSession();
    pushUpdate({
      sessionUpdate: 'agent_thought_chunk',
      content: { type: 'text', text: 'Let me inspect the file.' },
    });
    pushUpdate({
      sessionUpdate: 'agent_message_chunk',
      content: { type: 'text', text: 'None' },
    });
    pushUpdate({
      sessionUpdate: 'tool_call',
      toolCallId: 'call_read',
      title: 'Read file',
      kind: 'read',
      status: 'in_progress',
    });

    const state = useChat.getState().sessions['sess-1']!;
    expect(state.messages.map((m) => ({ role: m.role, text: m.text }))).toEqual([
      { role: 'agent_thought', text: 'Let me inspect the file.' },
    ]);
    expect(Object.keys(state.toolCalls)).toEqual(['call_read']);
  });

  it('drops "None" agent messages even when streamed across multiple chunks', () => {
    seedSession();
    pushUpdate({
      sessionUpdate: 'agent_message_chunk',
      content: { type: 'text', text: 'No' },
    });
    pushUpdate({
      sessionUpdate: 'agent_message_chunk',
      content: { type: 'text', text: 'ne' },
    });
    pushUpdate({
      sessionUpdate: 'tool_call',
      toolCallId: 'call_x',
      title: 'Edit file',
      kind: 'edit',
      status: 'in_progress',
    });

    const state = useChat.getState().sessions['sess-1']!;
    expect(state.messages).toHaveLength(0);
    expect(state.toolCalls['call_x']?.title).toBe('Edit file');
  });

  it('keeps agent messages whose content merely contains "None" as a substring', () => {
    seedSession();
    pushUpdate({
      sessionUpdate: 'agent_message_chunk',
      content: { type: 'text', text: 'None of the files matched.' },
    });
    pushUpdate({
      sessionUpdate: 'tool_call',
      toolCallId: 'call_y',
      title: 'Search',
      kind: 'search',
      status: 'in_progress',
    });

    const state = useChat.getState().sessions['sess-1']!;
    expect(state.messages.map((m) => m.text)).toEqual(['None of the files matched.']);
  });

  it('keeps tool calls inline between surrounding message chunks', () => {
    seedSession();
    pushUpdate({
      sessionUpdate: 'agent_message_chunk',
      content: { type: 'text', text: 'I will inspect the file.' },
    });
    pushUpdate({
      sessionUpdate: 'tool_call',
      toolCallId: 'call_read',
      title: 'Read file',
      kind: 'read',
      status: 'in_progress',
    });
    pushUpdate({
      sessionUpdate: 'tool_call_update',
      toolCallId: 'call_read',
      status: 'completed',
    });
    pushUpdate({
      sessionUpdate: 'agent_message_chunk',
      content: { type: 'text', text: 'The file is straightforward.' },
    });

    const state = useChat.getState().sessions['sess-1']!;
    const tool = state.toolCalls['call_read']!;

    expect(state.messages).toHaveLength(2);
    expect(state.messages[0]!.streaming).toBe(false);
    expect(state.messages[0]!.order).toBeLessThan(tool.order);
    expect(tool.order).toBeLessThan(state.messages[1]!.order);
    expect(state.messages.map((m) => m.text)).toEqual([
      'I will inspect the file.',
      'The file is straightforward.',
    ]);
  });

  it('preserves existing state when session_created arrives for a known session', () => {
    // The daemon broadcasts session_created right after the
    // chat.create_session response, so the originating client receives
    // it after it has already populated configOptions / messages /
    // currentModeId from the response payload. The handler must not
    // clobber that state with emptySession() — that would hide the
    // model picker and mode controls.
    seedSession();
    pushUpdate({
      sessionUpdate: 'config_option_update',
      configOptions: [
        {
          id: 'model',
          name: 'Model',
          type: 'select',
          currentValue: 'claude-opus-4-7-max',
          options: [{ value: 'claude-opus-4-7-max', name: 'Claude Opus 4.7 Max' }],
        },
      ],
    });
    pushUpdate({ sessionUpdate: 'current_mode_update', currentModeId: 'plan' });

    pushSessionCreated({ ...baseSession });

    const state = useChat.getState().sessions['sess-1']!;
    expect(state.configOptions).toHaveLength(1);
    expect(state.configOptions[0]?.currentValue).toBe('claude-opus-4-7-max');
    expect(state.currentModeId).toBe('plan');
    // session metadata should still be present.
    expect(state.session.id).toBe('sess-1');
  });

  it('seeds fresh state when session_created arrives for an unknown session', () => {
    // A session opened on a different device should still produce a
    // fresh entry in the local store via the broadcast.
    seedSession();
    pushSessionCreated({ ...baseSession, id: 'sess-from-phone' });

    const state = useChat.getState().sessions['sess-from-phone']!;
    expect(state).toBeDefined();
    expect(state.messages).toEqual([]);
    expect(state.configOptions).toEqual([]);
  });

  it('closeLiveAgentMessage marks the live bubble as not streaming so the ' +
     'next agent_message_chunk starts a fresh bubble', () => {
    // Reproduces the back-to-back slash-command regression: without
    // calling closeLiveAgentMessage between turns, chunks for /status
    // followed by /workspace get appended into a single assistant
    // bubble. After closing, the next chunk creates a new bubble.
    seedSession();
    pushUpdate({
      sessionUpdate: 'agent_message_chunk',
      content: { type: 'text', text: '✓ Authenticated via ACP host.' },
    });

    // Bubble for turn 1 is streaming.
    let state = useChat.getState().sessions['sess-1']!;
    expect(state.liveAgentMessageId).not.toBeNull();
    expect(state.messages).toHaveLength(1);
    expect(state.messages[0]!.streaming).toBe(true);

    // Simulate session/prompt response arriving.
    useChat.getState().closeLiveAgentMessage('sess-1');

    state = useChat.getState().sessions['sess-1']!;
    expect(state.liveAgentMessageId).toBeNull();
    expect(state.messages[0]!.streaming).toBe(false);

    // Turn 2 chunk should land in a brand-new bubble, not append.
    pushUpdate({
      sessionUpdate: 'agent_message_chunk',
      content: { type: 'text', text: '**Workspace directories:**' },
    });

    state = useChat.getState().sessions['sess-1']!;
    expect(state.messages).toHaveLength(2);
    expect(state.messages.map((m) => m.text)).toEqual([
      '✓ Authenticated via ACP host.',
      '**Workspace directories:**',
    ]);
    expect(state.messages[0]!.streaming).toBe(false);
    expect(state.messages[1]!.streaming).toBe(true);
  });

  it('closeLiveAgentMessage is a no-op when no live bubble exists', () => {
    seedSession();
    // No bubble yet.
    expect(useChat.getState().sessions['sess-1']!.liveAgentMessageId).toBeNull();
    expect(() => useChat.getState().closeLiveAgentMessage('sess-1')).not.toThrow();
    expect(useChat.getState().sessions['sess-1']!.liveAgentMessageId).toBeNull();
    expect(useChat.getState().sessions['sess-1']!.messages).toEqual([]);
  });
});
