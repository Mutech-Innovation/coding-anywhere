import {
  type AvailableCommand,
  type ChatAgentDescriptor,
  type ChatAttachment,
  type ChatConfigOption,
  ChatEvent,
  ChatMethod,
  type ChatPermissionRequestEvent,
  type ChatSession,
  type ChatSessionStatus,
  type ChatUsage,
  type ChatUpdateEvent,
  decodeConfigOptionUpdate,
  decodeToolCallUpdate,
  decodeUsageUpdate,
  extractChunkText,
  makeRequest,
  type Envelope,
  type PlanEntry,
  type RelayWSClient,
  type SessionMode,
  type ToolCallContentBlock,
  type ToolCallLocation,
  type ToolCallStatus,
  type ToolKind,
} from '@coding-anywhere/protocol';
import { newId } from '@coding-anywhere/utils';
import { create } from 'zustand';

import { useConnection } from './connection.js';

export type ChatRole = 'user' | 'agent' | 'agent_thought' | 'system';

export interface ChatMessage {
  id: string;
  role: ChatRole;
  text: string;
  /** Set when the chunk stream is still appending. */
  streaming?: boolean;
  createdAt: number;
  order: number;
  attachments?: ChatAttachment[];
}

export interface ChatToolCall {
  id: string;
  /** Human-readable title from ACP `title` (e.g. "Read package.json"). */
  title?: string;
  /** ACP `kind` — drives icon selection. */
  kind?: ToolKind;
  status?: ToolCallStatus;
  /** Tool-emitted content blocks. */
  content?: ToolCallContentBlock[];
  /** Files the tool is touching. */
  locations?: ToolCallLocation[];
  /** Raw input/output payloads. Useful for debugging. */
  rawInput?: unknown;
  rawOutput?: unknown;
  /** Transcript order, so the UI can render tool calls alongside messages. */
  order: number;
}

export interface ChatPermission {
  requestId: string;
  params: Record<string, unknown>;
  pending: boolean;
}

export interface SessionState {
  session: ChatSession;
  messages: ChatMessage[];
  toolCalls: Record<string, ChatToolCall>;
  /** Insertion counter — increments for each new transcript item. */
  transcriptOrder: number;
  /** Latest agent plan, if any. */
  plan: PlanEntry[] | null;
  /** Mode-related state (set if the agent advertised modes at session/new). */
  modes: SessionMode[];
  currentModeId: string | null;
  /** Slash commands the agent advertises for this session. */
  availableCommands: AvailableCommand[];
  usage: ChatUsage | null;
  configOptions: ChatConfigOption[];
  permissions: Record<string, ChatPermission>;
  // Convenience: id of the in-progress streaming agent message (if any) so we
  // know which one to append to.
  liveAgentMessageId: string | null;
}

interface ChatStore {
  attached: boolean;
  agents: ChatAgentDescriptor[];
  sessions: Record<string, SessionState>;
  activeId: string | null;
  lastError: string | null;

  attach(client: RelayWSClient): void;
  detach(): void;
  loadAgents(): Promise<void>;
  refreshSessions(): Promise<void>;
  createSession(args: { cwd?: string; agentId?: string; title?: string }): Promise<ChatSession>;
  setActive(id: string | null): void;
  sendPrompt(sessionId: string, text: string, attachments?: ChatAttachment[]): Promise<void>;
  cancel(sessionId: string): Promise<void>;
  closeSession(sessionId: string): Promise<void>;
  resolvePermission(sessionId: string, requestId: string, decision: unknown): Promise<void>;
  setMode(sessionId: string, modeId: string): Promise<void>;
  /** Reload one session's full snapshot (updates + prompts + modes) from the daemon. */
  refreshHistory(sessionId: string): Promise<void>;
  /** Locally clear messages/tool calls/plan for the session — used by /clear etc. */
  resetLocalSession(sessionId: string): void;
}

const pending = new Map<
  string,
  { resolve: (env: Envelope) => void; reject: (err: Error) => void }
>();
let handlerClient: RelayWSClient | null = null;

export const useChat = create<ChatStore>((set, get) => ({
  attached: false,
  agents: [],
  sessions: {},
  activeId: null,
  lastError: null,

  attach(client) {
    if (get().attached && handlerClient === client) return;
    handlerClient = client;
    set({ attached: true });

    client.handle('chat', (env) => {
      if (env.type === 'res') {
        const p = pending.get(env.id);
        if (p) {
          pending.delete(env.id);
          if (env.error) p.reject(new Error(`${env.error.code}: ${env.error.message}`));
          else p.resolve(env);
        }
        return;
      }
      if (env.type !== 'event') return;

      switch (env.method) {
        case ChatEvent.SessionCreated: {
          const payload = env.payload as { session: ChatSession } | undefined;
          if (!payload?.session) return;
          set((s) => ({
            sessions: {
              ...s.sessions,
              [payload.session.id]: emptySession(payload.session),
            },
          }));
          break;
        }
        case ChatEvent.SessionClosed: {
          const payload = env.payload as { session_id: string } | undefined;
          if (!payload?.session_id) return;
          set((s) => {
            const { [payload.session_id]: _gone, ...rest } = s.sessions;
            const activeId = s.activeId === payload.session_id ? null : s.activeId;
            return { sessions: rest, activeId };
          });
          break;
        }
        case ChatEvent.SessionStatus: {
          const payload = env.payload as
            | { session_id: string; status: ChatSessionStatus }
            | undefined;
          if (!payload?.session_id) return;
          set((s) => {
            const cur = s.sessions[payload.session_id];
            if (!cur) return {};
            return {
              sessions: {
                ...s.sessions,
                [payload.session_id]: {
                  ...cur,
                  session: { ...cur.session, status: payload.status },
                },
              },
            };
          });
          break;
        }
        case ChatEvent.Update: {
          const payload = env.payload as ChatUpdateEvent | undefined;
          if (!payload?.session_id) return;
          ingestUpdate(set, get, payload);
          break;
        }
        case ChatEvent.PermissionRequest: {
          const payload = env.payload as ChatPermissionRequestEvent | undefined;
          if (!payload?.session_id || !payload.request_id) return;
          set((s) => {
            const cur = s.sessions[payload.session_id];
            if (!cur) return {};
            return {
              sessions: {
                ...s.sessions,
                [payload.session_id]: {
                  ...cur,
                  permissions: {
                    ...cur.permissions,
                    [payload.request_id]: {
                      requestId: payload.request_id,
                      params: payload.params,
                      pending: true,
                    },
                  },
                },
              },
            };
          });
          break;
        }
        case ChatEvent.PermissionResolved: {
          const payload = env.payload as { session_id: string; request_id: string } | undefined;
          if (!payload?.session_id) return;
          set((s) => {
            const cur = s.sessions[payload.session_id];
            if (!cur) return {};
            const { [payload.request_id]: _gone, ...rest } = cur.permissions;
            return {
              sessions: {
                ...s.sessions,
                [payload.session_id]: { ...cur, permissions: rest },
              },
            };
          });
          break;
        }
        default:
          break;
      }
    });
  },

  detach() {
    handlerClient = null;
    set({
      attached: false,
      agents: [],
      sessions: {},
      activeId: null,
      lastError: null,
    });
    pending.clear();
  },

  async loadAgents() {
    const env = await sendRequest(ChatMethod.Agents);
    const payload = env.payload as { agents?: ChatAgentDescriptor[] } | undefined;
    set({ agents: payload?.agents ?? [] });
  },

  async refreshSessions() {
    const env = await sendRequest(ChatMethod.ListSessions);
    const payload = env.payload as { sessions?: ChatSession[] } | undefined;
    const list = payload?.sessions ?? [];
    set((s) => ({
      sessions: Object.fromEntries(
        list.map((sess) => [sess.id, s.sessions[sess.id] ?? emptySession(sess)]),
      ),
    }));
    // Eagerly fetch the snapshot for any session we don't have content for
    // yet — covers the "open another browser tab" case. Failures are
    // non-fatal; we just keep the empty placeholder.
    const tasks = list
      .filter((sess) => (get().sessions[sess.id]?.messages.length ?? 0) === 0)
      .map((sess) =>
        get()
          .refreshHistory(sess.id)
          .catch(() => {}),
      );
    await Promise.all(tasks);
  },

  async createSession({ cwd, agentId = 'devin', title }) {
    const env = await sendRequest(ChatMethod.CreateSession, {
      cwd,
      agent_id: agentId,
      title,
    });
    const payload = env.payload as {
      session: ChatSession;
      available_modes?: SessionMode[];
      current_mode_id?: string;
    };
    set((s) => ({
      sessions: {
        ...s.sessions,
        [payload.session.id]: {
          ...emptySession(payload.session),
          modes: payload.available_modes ?? [],
          currentModeId: payload.current_mode_id ?? null,
        },
      },
      activeId: payload.session.id,
    }));
    return payload.session;
  },

  setActive(id) {
    set({ activeId: id });
  },

  async sendPrompt(sessionId, text, attachments = []) {
    // Optimistically push the user message.
    set((s) => {
      const cur = s.sessions[sessionId];
      if (!cur) return {};
      const order = cur.transcriptOrder + 1;
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...cur,
            messages: [
              ...cur.messages,
              {
                id: newId(),
                role: 'user',
                text,
                attachments: attachments.length > 0 ? attachments : undefined,
                createdAt: Date.now(),
                order,
              },
            ],
            transcriptOrder: order,
          },
        },
      };
    });
    // /clear and /new ask Devin to start a fresh conversation. We don't
    // know what session id the agent will hand us back over ACP, so for
    // now we simply wipe the local view; the agent will resume sending
    // updates against the new logical thread.
    const trimmed = text.trim();
    if (trimmed === '/clear' || trimmed === '/new') {
      // Run after the optimistic user message above so the user can still
      // see what they typed in the transcript.
      setTimeout(() => get().resetLocalSession(sessionId), 0);
    }
    await sendRequest(ChatMethod.Prompt, { session_id: sessionId, text, attachments });
  },

  async cancel(sessionId) {
    await sendRequest(ChatMethod.Cancel, { session_id: sessionId });
  },

  async closeSession(sessionId) {
    await sendRequest(ChatMethod.CloseSession, { session_id: sessionId });
  },

  async resolvePermission(sessionId, requestId, decision) {
    set((s) => {
      const cur = s.sessions[sessionId];
      if (!cur) return {};
      const perm = cur.permissions[requestId];
      if (!perm) return {};
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...cur,
            permissions: {
              ...cur.permissions,
              [requestId]: { ...perm, pending: false },
            },
          },
        },
      };
    });
    await sendRequest(ChatMethod.ResolvePermission, {
      request_id: requestId,
      decision,
    });
  },

  async setMode(sessionId, modeId) {
    // Optimistic — daemon will broadcast a current_mode_update if the agent
    // confirms. If it errors, the agent's true mode is whatever the daemon
    // reports; we leave the optimistic value in place.
    set((s) => {
      const cur = s.sessions[sessionId];
      if (!cur) return {};
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: { ...cur, currentModeId: modeId },
        },
      };
    });
    await sendRequest(ChatMethod.SetMode, { session_id: sessionId, mode_id: modeId });
  },

  async refreshHistory(sessionId) {
    const env = await sendRequest(ChatMethod.GetHistory, { session_id: sessionId });
    const hist = env.payload as {
      session: ChatSession;
      updates?: Array<Record<string, unknown>>;
      user_prompts?: Array<{ text: string; created_at: string; sequence_id?: number }>;
      available_modes?: SessionMode[];
      current_mode_id?: string;
    };
    set((s) => ({
      sessions: {
        ...s.sessions,
        [sessionId]: {
          ...emptySession(hist.session),
          modes: hist.available_modes ?? [],
          currentModeId: hist.current_mode_id ?? null,
        },
      },
    }));
    // Replay user prompts and updates in order using the sequence id where
    // available so the timeline is reconstructed faithfully.
    const promptEvents = (hist.user_prompts ?? []).map((p) => ({
      kind: 'prompt' as const,
      seq: p.sequence_id ?? 0,
      text: p.text,
      createdAt: Date.parse(p.created_at) || Date.now(),
    }));
    const updateEvents = (hist.updates ?? []).map((u, i) => ({
      kind: 'update' as const,
      seq: ((u as { sequence_id?: number }).sequence_id as number | undefined) ?? i + 1,
      update: u,
    }));
    const merged = [...promptEvents, ...updateEvents].sort((a, b) => a.seq - b.seq);
    for (const ev of merged) {
      if (ev.kind === 'prompt') {
        set((s) => {
          const cur = s.sessions[sessionId];
          if (!cur) return {};
          const order = cur.transcriptOrder + 1;
          return {
            sessions: {
              ...s.sessions,
              [sessionId]: {
                ...cur,
                messages: [
                  ...cur.messages,
                  { id: newId(), role: 'user', text: ev.text, createdAt: ev.createdAt, order },
                ],
                transcriptOrder: order,
              },
            },
          };
        });
      } else {
        ingestUpdate(set, get, { session_id: sessionId, update: ev.update });
      }
    }
  },

  resetLocalSession(sessionId) {
    set((s) => {
      const cur = s.sessions[sessionId];
      if (!cur) return {};
      return {
        sessions: {
          ...s.sessions,
          [sessionId]: {
            ...emptySession(cur.session),
            modes: cur.modes,
            currentModeId: cur.currentModeId,
            availableCommands: cur.availableCommands,
            configOptions: cur.configOptions,
          },
        },
      };
    });
  },
}));

function emptySession(session: ChatSession): SessionState {
  return {
    session,
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
  };
}

function ingestUpdate(
  set: (mut: (s: ChatStore) => Partial<ChatStore>) => void,
  get: () => ChatStore,
  payload: ChatUpdateEvent,
) {
  set((s) => {
    const cur = s.sessions[payload.session_id];
    if (!cur) return {};
    const update = payload.update;
    const kind = update.sessionUpdate as string | undefined;
    if (!kind) return {};

    switch (kind) {
      case 'agent_message_chunk':
      case 'agent_thought_chunk': {
        const text = extractChunkText(update);
        if (text == null) return {};
        const role: ChatRole = kind === 'agent_thought_chunk' ? 'agent_thought' : 'agent';
        const live = cur.liveAgentMessageId
          ? cur.messages.find((m) => m.id === cur.liveAgentMessageId)
          : undefined;
        if (live && live.role === role && live.streaming) {
          // Append to existing message.
          const next = cur.messages.map((m) =>
            m.id === live.id ? { ...m, text: m.text + text } : m,
          );
          return {
            sessions: {
              ...s.sessions,
              [payload.session_id]: { ...cur, messages: next },
            },
          };
        }
        const base = closeLiveMessage(cur);
        const id = newId();
        const order = base.transcriptOrder + 1;
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: {
              ...base,
              messages: [
                ...base.messages,
                {
                  id,
                  role,
                  text,
                  streaming: true,
                  createdAt: Date.now(),
                  order,
                },
              ],
              liveAgentMessageId: id,
              transcriptOrder: order,
            },
          },
        };
      }
      case 'user_message_chunk': {
        const text = extractChunkText(update);
        if (text == null) return {};
        const base = closeLiveMessage(cur);
        const order = base.transcriptOrder + 1;
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: {
              ...base,
              messages: [
                ...base.messages,
                {
                  id: newId(),
                  role: 'user',
                  text,
                  createdAt: Date.now(),
                  order,
                },
              ],
              transcriptOrder: order,
            },
          },
        };
      }
      case 'tool_call':
      case 'tool_call_update': {
        const view = decodeToolCallUpdate(update);
        if (!view) return {};
        const base = closeLiveMessage(cur);
        const existing = base.toolCalls[view.toolCallId];
        const order = existing?.order ?? base.transcriptOrder + 1;
        const merged: ChatToolCall = {
          id: view.toolCallId,
          title: view.title ?? existing?.title,
          kind: view.kind ?? existing?.kind,
          status: view.status ?? existing?.status,
          content: view.content ?? existing?.content,
          locations: view.locations ?? existing?.locations,
          rawInput: 'rawInput' in view ? view.rawInput : existing?.rawInput,
          rawOutput: 'rawOutput' in view ? view.rawOutput : existing?.rawOutput,
          order,
        };
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: {
              ...base,
              toolCalls: { ...base.toolCalls, [view.toolCallId]: merged },
              transcriptOrder: existing ? base.transcriptOrder : order,
            },
          },
        };
      }
      case 'plan': {
        const entries = update.entries as PlanEntry[] | undefined;
        if (!Array.isArray(entries)) return {};
        const base = closeLiveMessage(cur);
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: { ...base, plan: entries },
          },
        };
      }
      case 'current_mode_update': {
        const id = update.currentModeId as string | undefined;
        if (typeof id !== 'string') return {};
        const base = closeLiveMessage(cur);
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: { ...base, currentModeId: id },
          },
        };
      }
      case 'available_commands_update': {
        const cmds = update.availableCommands as AvailableCommand[] | undefined;
        if (!Array.isArray(cmds)) return {};
        const base = closeLiveMessage(cur);
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: { ...base, availableCommands: cmds },
          },
        };
      }
      case 'usage_update': {
        const usage = decodeUsageUpdate(update);
        if (!usage) return {};
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: { ...cur, usage },
          },
        };
      }
      case 'config_option_update': {
        const configOptions = decodeConfigOptionUpdate(update);
        if (!configOptions) return {};
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: { ...cur, configOptions },
          },
        };
      }
      case 'session_info_update': {
        const title = update.title as string | null | undefined;
        if (title === undefined) return {};
        const base = closeLiveMessage(cur);
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: {
              ...base,
              session: { ...base.session, title: title ?? '' },
            },
          },
        };
      }
      default:
        // Unknown update kinds keep the session ticking but we don't render
        // them yet. Add a system-level note for visibility during MVP.
        const base = closeLiveMessage(cur);
        const order = base.transcriptOrder + 1;
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: {
              ...base,
              messages: [
                ...base.messages,
                {
                  id: newId(),
                  role: 'system',
                  text: `update: ${kind}`,
                  createdAt: Date.now(),
                  order,
                },
              ],
              transcriptOrder: order,
            },
          },
        };
    }
  });
  // Non-chunk updates close the previous live message before entering the
  // transcript, so later chunks start a new bubble after the intervening item.
  void get;
}

function closeLiveMessage(cur: SessionState): SessionState {
  if (!cur.liveAgentMessageId) return cur;
  const liveId = cur.liveAgentMessageId;
  const live = cur.messages.find((m) => m.id === liveId);
  if (!live?.streaming) return { ...cur, liveAgentMessageId: null };
  // Devin's `swe-1-6-fast` (and likely other models) sometimes finishes an
  // assistant turn with `content == "None"` — Python's `None` str-ified —
  // when the model decided to emit only tool calls and no accompanying
  // text. The agent then streams that body as one or more
  // `agent_message_chunk` updates whose concatenated text is exactly
  // "None", and we'd otherwise finalise a bare "None" bubble between the
  // thought card and the tool-call card. Drop the message instead.
  if (live.role === 'agent' && live.text === 'None') {
    return {
      ...cur,
      messages: cur.messages.filter((m) => m.id !== liveId),
      liveAgentMessageId: null,
    };
  }
  return {
    ...cur,
    messages: cur.messages.map((m) => (m.id === liveId ? { ...m, streaming: false } : m)),
    liveAgentMessageId: null,
  };
}

async function sendRequest(method: string, payload: unknown = {}): Promise<Envelope> {
  const client = useConnection.getState().client;
  if (!client) throw new Error('not connected');
  const id = newId();
  const env = makeRequest({ id, channel: 'chat', method, payload });
  return new Promise<Envelope>((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`chat.${method} timed out`));
    }, 60_000);
    pending.set(id, {
      resolve: (e) => {
        clearTimeout(timer);
        resolve(e);
      },
      reject: (err) => {
        clearTimeout(timer);
        reject(err);
      },
    });
    client.send(env);
  });
}
