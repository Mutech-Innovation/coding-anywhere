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
  /** Fetch the persisted state for one session (messages, tool calls, plan, mode, etc). */
  GetHistory: 'get_history',
  /** Switch the agent's session mode (ACP `session/set_mode`). */
  SetMode: 'set_mode',
  /**
   * Change a session-scoped config option (ACP `session/set_config_option`).
   * The web UI's model picker uses this to switch the active model; the
   * agent responds with an updated `config_option_update` event.
   */
  SetConfigOption: 'set_config_option',
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
  /** "guest" for sessions created via a read-only share link. Such sessions
   * are visible to the owner's clients (grouped by share_id) but view-only:
   * the daemon rejects owner prompt/set_mode/set_config_option on them. */
  origin: z.string().optional(),
  /** Guest share link id the session was created through (origin="guest"
   * only). Lets the owner's UI group one link's sessions together. */
  share_id: z.string().optional(),
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
  cwd?: string;
  title?: string;
}

export interface ChatPromptRequest {
  session_id: string;
  text: string;
  attachments?: ChatAttachment[];
}

export interface ChatAttachment {
  type: 'image' | 'file';
  name: string;
  data?: string;
  uri?: string;
  mime_type?: string;
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
  UsageUpdate: 'usage_update',
  ConfigOptionUpdate: 'config_option_update',
  SessionInfoUpdate: 'session_info_update',
} as const;

/** Decoded text content from an ACP message-chunk update, if any. */
export function extractChunkText(update: Record<string, unknown>): string | null {
  const content = update.content as { type?: string; text?: string } | undefined;
  if (!content || typeof content.type !== 'string') return null;
  if (content.type === 'text' && typeof content.text === 'string') return content.text;
  return null;
}

// --- ACP value types we render directly ---

/** ACP `ToolKind`. Drives icon selection. */
export const ToolKind = {
  Read: 'read',
  Edit: 'edit',
  Delete: 'delete',
  Move: 'move',
  Search: 'search',
  Execute: 'execute',
  Think: 'think',
  Fetch: 'fetch',
  SwitchMode: 'switch_mode',
  Other: 'other',
} as const;
export type ToolKind = (typeof ToolKind)[keyof typeof ToolKind];

/** ACP `ToolCallStatus`. */
export const ToolCallStatus = {
  Pending: 'pending',
  InProgress: 'in_progress',
  Completed: 'completed',
  Failed: 'failed',
} as const;
export type ToolCallStatus = (typeof ToolCallStatus)[keyof typeof ToolCallStatus];

export interface ToolCallLocation {
  path: string;
  line?: number | null;
}

/** Subset of ACP `ToolCallContent` we render. Other variants are kept raw. */
export type ToolCallContentBlock =
  | {
      type: 'content';
      content: { type: string; text?: string; uri?: string; [k: string]: unknown };
    }
  | { type: 'diff'; path: string; oldText?: string | null; newText: string }
  | { type: 'terminal'; terminalId: string }
  | { type: string; [k: string]: unknown };

/** ACP `PlanEntryStatus`. */
export const PlanEntryStatus = {
  Pending: 'pending',
  InProgress: 'in_progress',
  Completed: 'completed',
} as const;
export type PlanEntryStatus = (typeof PlanEntryStatus)[keyof typeof PlanEntryStatus];

/** ACP `PlanEntryPriority`. */
export const PlanEntryPriority = {
  High: 'high',
  Medium: 'medium',
  Low: 'low',
} as const;
export type PlanEntryPriority = (typeof PlanEntryPriority)[keyof typeof PlanEntryPriority];

export interface PlanEntry {
  content: string;
  priority: PlanEntryPriority;
  status: PlanEntryStatus;
}

export interface SessionMode {
  id: string;
  name: string;
  description?: string;
}

export interface AvailableCommand {
  name: string;
  description: string;
  input?: { hint: string };
}

export interface ChatUsageCost {
  amount: number;
  currency: string;
}

export interface ChatUsage {
  used: number;
  size: number;
  cost?: ChatUsageCost;
}

export interface ChatConfigOptionChoice {
  value?: unknown;
  label?: string;
  name?: string;
  [k: string]: unknown;
}

export interface ChatConfigOption {
  id: string;
  name?: string;
  type?: string;
  currentValue?: unknown;
  options?: ChatConfigOptionChoice[];
  [k: string]: unknown;
}

/** Strongly-typed view of an ACP `tool_call` / `tool_call_update` body. */
export interface ToolCallView {
  toolCallId: string;
  title?: string;
  kind?: ToolKind;
  status?: ToolCallStatus;
  content?: ToolCallContentBlock[];
  locations?: ToolCallLocation[];
  rawInput?: unknown;
  rawOutput?: unknown;
}

/** Decode the relevant tool-call fields from a raw ACP update. */
export function decodeToolCallUpdate(update: Record<string, unknown>): ToolCallView | null {
  const id = update.toolCallId;
  if (typeof id !== 'string') return null;
  const out: ToolCallView = { toolCallId: id };
  if (typeof update.title === 'string') out.title = update.title;
  if (typeof update.kind === 'string') out.kind = update.kind as ToolKind;
  if (typeof update.status === 'string') out.status = update.status as ToolCallStatus;
  if (Array.isArray(update.content)) out.content = update.content as ToolCallContentBlock[];
  if (Array.isArray(update.locations)) out.locations = update.locations as ToolCallLocation[];
  if ('rawInput' in update) out.rawInput = update.rawInput;
  if ('rawOutput' in update) out.rawOutput = update.rawOutput;
  return out;
}

export function decodeUsageUpdate(update: Record<string, unknown>): ChatUsage | null {
  const { used, size } = update;
  if (typeof used !== 'number' || typeof size !== 'number') return null;
  const usage: ChatUsage = { used, size };
  if (isRecord(update.cost)) {
    const { amount, currency } = update.cost;
    if (typeof amount === 'number' && typeof currency === 'string') {
      usage.cost = { amount, currency };
    }
  }
  return usage;
}

export function decodeConfigOptionUpdate(
  update: Record<string, unknown>,
): ChatConfigOption[] | null {
  if (!Array.isArray(update.configOptions)) return null;
  const options: ChatConfigOption[] = [];
  for (const raw of update.configOptions) {
    if (!isRecord(raw) || typeof raw.id !== 'string') continue;
    const option: ChatConfigOption = { ...raw, id: raw.id };
    if (Array.isArray(raw.options)) {
      option.options = raw.options
        .filter(isRecord)
        .map((choice) => ({ ...choice }) as ChatConfigOptionChoice);
    }
    options.push(option);
  }
  return options;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}
