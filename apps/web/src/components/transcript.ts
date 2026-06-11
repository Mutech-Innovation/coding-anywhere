import type { ChatMessage, ChatToolCall } from '../store/chat.js';

/**
 * Transcript grouping for the chat timeline (used by ChatPanel + GuestChat).
 *
 * Agents often fire long runs of consecutive tool calls (10-20 reads/greps in
 * a row), which used to render as a wall of cards. We fold any run of
 * TOOL_GROUP_THRESHOLD or more consecutive tool calls into a single
 * collapsible group; shorter runs keep their individual cards. Any message
 * (including thought bubbles) breaks a run, so grouping never reorders the
 * timeline.
 */
export const TOOL_GROUP_THRESHOLD = 3;

/** A sorted timeline entry, before grouping. */
export type TranscriptEntry =
  | { type: 'message'; order: number; message: ChatMessage }
  | { type: 'tool'; order: number; call: ChatToolCall };

/** What the timeline actually renders, after grouping. */
export type TranscriptRenderItem =
  | { type: 'message'; key: string; message: ChatMessage }
  | { type: 'tool'; key: string; call: ChatToolCall }
  | { type: 'tool_group'; key: string; calls: ChatToolCall[] };

/**
 * Folds runs of >= TOOL_GROUP_THRESHOLD consecutive tool calls into
 * `tool_group` items. `entries` must already be sorted by `order`. Group keys
 * are derived from the first call in the run, so a group keeps its React
 * identity (and collapse state) while a streaming run appends more calls.
 */
export function groupTranscript(entries: TranscriptEntry[]): TranscriptRenderItem[] {
  const out: TranscriptRenderItem[] = [];
  let run: ChatToolCall[] = [];
  const flush = () => {
    if (run.length >= TOOL_GROUP_THRESHOLD) {
      out.push({ type: 'tool_group', key: `tool-group-${run[0]!.id}`, calls: run });
    } else {
      for (const call of run) out.push({ type: 'tool', key: `tool-${call.id}`, call });
    }
    run = [];
  };
  for (const entry of entries) {
    if (entry.type === 'tool') {
      run.push(entry.call);
    } else {
      flush();
      out.push({ type: 'message', key: `message-${entry.message.id}`, message: entry.message });
    }
  }
  flush();
  return out;
}

/** A tool call that hasn't finished yet (drives the group's "live" row). */
export function isActiveToolCall(call: ChatToolCall): boolean {
  const status = call.status ?? 'pending';
  return status === 'pending' || status === 'in_progress';
}
