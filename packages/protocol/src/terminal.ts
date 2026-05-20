// Wire types for the `terminal` channel. Mirrors internal/wire/terminal.go.
//
// Convention: methods are constants (TerminalMethod* / TerminalEvent*) so
// callers don't pass string literals around. Bytes are base64-encoded
// because the underlying transport is JSON.

import { z } from 'zod';

export const TerminalMethod = {
  Open: 'open',
  List: 'list',
  Attach: 'attach',
  Detach: 'detach',
  Input: 'input',
  Resize: 'resize',
  Close: 'close',
} as const;
export type TerminalMethod = (typeof TerminalMethod)[keyof typeof TerminalMethod];

export const TerminalEvent = {
  Opened: 'opened',
  Closed: 'closed',
  Data: 'data',
  Exit: 'exit',
} as const;
export type TerminalEvent = (typeof TerminalEvent)[keyof typeof TerminalEvent];

export const TerminalSessionInfo = z.object({
  id: z.string(),
  pid: z.number(),
  cols: z.number(),
  rows: z.number(),
  command: z.array(z.string()),
  cwd: z.string(),
  started_at: z.string(),
});
export type TerminalSessionInfo = z.infer<typeof TerminalSessionInfo>;

export const TerminalOpenRequest = z.object({
  command: z.array(z.string()).optional(),
  cwd: z.string().optional(),
  cols: z.number().optional(),
  rows: z.number().optional(),
});
export type TerminalOpenRequest = z.infer<typeof TerminalOpenRequest>;

export const TerminalOpenResponse = z.object({
  session: TerminalSessionInfo,
});
export type TerminalOpenResponse = z.infer<typeof TerminalOpenResponse>;

export const TerminalListResponse = z.object({
  sessions: z.array(TerminalSessionInfo),
});
export type TerminalListResponse = z.infer<typeof TerminalListResponse>;

export const TerminalAttachRequest = z.object({ id: z.string() });
export const TerminalAttachResponse = z.object({
  session: TerminalSessionInfo,
  snapshot: z.string(),
  truncated: z.boolean().optional(),
});
export type TerminalAttachResponse = z.infer<typeof TerminalAttachResponse>;

export const TerminalDetachRequest = z.object({ id: z.string() });
export const TerminalInputRequest = z.object({ id: z.string(), data: z.string() });
export const TerminalResizeRequest = z.object({
  id: z.string(),
  cols: z.number(),
  rows: z.number(),
});
export const TerminalCloseRequest = z.object({ id: z.string() });

export const TerminalAck = z.object({ ok: z.boolean() });
export type TerminalAck = z.infer<typeof TerminalAck>;

export const TerminalOpenedEvent = z.object({ session: TerminalSessionInfo });
export const TerminalClosedEvent = z.object({
  id: z.string(),
  exit_code: z.number(),
  err: z.string().optional(),
});
export const TerminalDataEvent = z.object({
  id: z.string(),
  data: z.string(),
});
export type TerminalDataEvent = z.infer<typeof TerminalDataEvent>;

export const TerminalExitEvent = TerminalClosedEvent;

/** Browser-friendly base64 encode of a Uint8Array. */
export function bytesToBase64(bytes: Uint8Array): string {
  let s = '';
  for (let i = 0; i < bytes.length; i += 0x8000) {
    s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  // btoa is available in browsers and React Native. Node 16+ also exposes it.
  return btoa(s);
}

/** Browser-friendly base64 decode to a Uint8Array. */
export function base64ToBytes(b64: string): Uint8Array {
  const s = atob(b64);
  const out = new Uint8Array(s.length);
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i);
  return out;
}
