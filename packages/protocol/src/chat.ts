// Wire types for the `chat` channel. Mirrors internal/wire/chat.go.
//
// We pass ACP session/update payloads through verbatim as JSON; the helper
// types here cover only the subset of `sessionUpdate` discriminators we
// actually render today. Anything we don't recognise is kept as-is so the
// UI can fall back to a debug view.

import { z } from 'zod';

export const ChatMethod = {
  ListSessions: 'list_sessions',
  CreateSession: 'create_session',
  Prompt: 'prompt',
  Cancel: 'cancel',
  CloseSession: 'close_session',
  Agents: 'agents',
  ResolvePermission: 'resolve_permission',
} as const;
export type ChatMethod = (typeof ChatMethod)[keyof typeof ChatMethod];

export const ChatEvent = {
  SessionCreated: 'session_created',
  SessionClosed: 'session_closed',
  SessionStatus: 'session_status',
  Update: 'update',
  PermissionRequest: 'permission_request',
  PermissionResolved: 'permission_resolved',
} as const;
export type ChatEvent = (typeof ChatEvent)[keyof typeof ChatEvent];

export const ChatSessionStatus = z.enum(['idle', 'thinking', 'awaiting_permission', 'closed']);
export type ChatSessionStatus = z.infer<typeof ChatSessionStatus>;

export const ChatSession = z.object({
  id: z.string(),
  agent_id: z.string(),
  native_id: z.string(),
  cwd: z.string(),
  title: z.string().optional(),
  status: ChatSessionStatus,
  started_at: z.string(),
});
export type ChatSession = z.infer<typeof ChatSession>;

export const ChatAgentDescriptor = z.object({
  id: z.string(),
  name: z.string(),
  available: z.boolean(),
  reason: z.string().optional(),
});
export type ChatAgentDescriptor = z.infer<typeof ChatAgentDescriptor>;

// --- Requests ---

export interface ChatCreateSessionRequest {
  agent_id?: string;
  cwd: string;
  title?: string;
}

export interface ChatPromptRequest {
  session_id: string;
  text: string;
}

export interface ChatCancelRequest {
  session_id: string;
}

export interface ChatCloseSessionRequest {
  session_id: string;
}

export interface ChatResolvePermissionRequest {
  request_id: string;
  decision: unknown;
}

// --- Events ---

export interface ChatSessionCreatedEvent {
  session: ChatSession;
}

export interface ChatSessionClosedEvent {
  session_id: string;
  reason?: string;
}

export interface ChatSessionStatusEvent {
  session_id: string;
  status: ChatSessionStatus;
}

export interface ChatUpdateEvent {
  session_id: string;
  /** Raw ACP `update` object — discriminator is `sessionUpdate`. */
  update: Record<string, unknown>;
}

export interface ChatPermissionRequestEvent {
  session_id: string;
  request_id: string;
  params: Record<string, unknown>;
}

export interface ChatPermissionResolvedEvent {
  session_id: string;
  request_id: string;
}

/** Recognised values for `update.sessionUpdate` (ACP). */
export const SessionUpdateKind = {
  AgentMessageChunk: 'agent_message_chunk',
  UserMessageChunk: 'user_message_chunk',
  AgentThoughtChunk: 'agent_thought_chunk',
  ToolCall: 'tool_call',
  ToolCallUpdate: 'tool_call_update',
  Plan: 'plan',
  CurrentMode: 'current_mode_update',
  AvailableCommandsUpdate: 'available_commands_update',
} as const;

/** Decoded text content from an ACP message-chunk update, if any. */
export function extractChunkText(update: Record<string, unknown>): string | null {
  const content = update.content as { type?: string; text?: string } | undefined;
  if (!content || typeof content.type !== 'string') return null;
  if (content.type === 'text' && typeof content.text === 'string') return content.text;
  return null;
}
