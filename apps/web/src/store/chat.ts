import {
  type ChatAgentDescriptor,
  ChatEvent,
  ChatMethod,
  type ChatPermissionRequestEvent,
  type ChatSession,
  type ChatSessionStatus,
  type ChatUpdateEvent,
  extractChunkText,
  makeRequest,
  type Envelope,
  type RelayWSClient,
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
}

export interface ChatToolCall {
  id: string;
  name?: string;
  input?: unknown;
  output?: unknown;
  status?: string;
  raw: Record<string, unknown>;
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
  createSession(args: { cwd: string; agentId?: string; title?: string }): Promise<ChatSession>;
  setActive(id: string | null): void;
  sendPrompt(sessionId: string, text: string): Promise<void>;
  cancel(sessionId: string): Promise<void>;
  closeSession(sessionId: string): Promise<void>;
  resolvePermission(sessionId: string, requestId: string, decision: unknown): Promise<void>;
}

const pending = new Map<
  string,
  { resolve: (env: Envelope) => void; reject: (err: Error) => void }
>();

export const useChat = create<ChatStore>((set, get) => ({
  attached: false,
  agents: [],
  sessions: {},
  activeId: null,
  lastError: null,

  attach(client) {
    if (get().attached) return;
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
  },

  async createSession({ cwd, agentId = 'devin', title }) {
    const env = await sendRequest(ChatMethod.CreateSession, {
      cwd,
      agent_id: agentId,
      title,
    });
    const payload = env.payload as { session: ChatSession };
    set((s) => ({
      sessions: { ...s.sessions, [payload.session.id]: emptySession(payload.session) },
      activeId: payload.session.id,
    }));
    return payload.session;
  },

  setActive(id) {
    set({ activeId: id });
  },

  async sendPrompt(sessionId, text) {
    // Optimistically push the user message.
    set((s) => {
      const cur = s.sessions[sessionId];
      if (!cur) return {};
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
                createdAt: Date.now(),
              },
            ],
          },
        },
      };
    });
    await sendRequest(ChatMethod.Prompt, { session_id: sessionId, text });
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
}));

function emptySession(session: ChatSession): SessionState {
  return {
    session,
    messages: [],
    toolCalls: {},
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
        const id = newId();
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: {
              ...cur,
              messages: [
                ...cur.messages,
                {
                  id,
                  role,
                  text,
                  streaming: true,
                  createdAt: Date.now(),
                },
              ],
              liveAgentMessageId: id,
            },
          },
        };
      }
      case 'user_message_chunk': {
        const text = extractChunkText(update);
        if (text == null) return {};
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: {
              ...cur,
              messages: [
                ...cur.messages,
                {
                  id: newId(),
                  role: 'user',
                  text,
                  createdAt: Date.now(),
                },
              ],
            },
          },
        };
      }
      case 'tool_call':
      case 'tool_call_update': {
        const toolCallId = (update.toolCallId as string) ?? newId();
        const next: ChatToolCall = {
          id: toolCallId,
          name: update.name as string | undefined,
          input: (update as { input?: unknown }).input,
          output: (update as { output?: unknown }).output,
          status: update.status as string | undefined,
          raw: update,
        };
        const merged = { ...cur.toolCalls[toolCallId], ...next };
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: {
              ...cur,
              toolCalls: { ...cur.toolCalls, [toolCallId]: merged },
            },
          },
        };
      }
      default:
        // Unknown update kinds keep the session ticking but we don't render
        // them yet. Add a system-level note for visibility during MVP.
        return {
          sessions: {
            ...s.sessions,
            [payload.session_id]: {
              ...cur,
              messages: [
                ...cur.messages,
                {
                  id: newId(),
                  role: 'system',
                  text: `update: ${kind}`,
                  createdAt: Date.now(),
                },
              ],
            },
          },
        };
    }
  });
  // When a chunk arrives, mark the previous live message as no longer
  // streaming once a non-chunk event lands. We don't bother tracking that
  // explicitly; it's fine for MVP.
  void get;
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
