import { z } from 'zod';

/**
 * Wire-level message envelope shared by daemon, relay and client.
 *
 * Every WebSocket frame is one of three flavours:
 *   - `req`   request expecting a response (correlated by `id`)
 *   - `res`   response to a previous request
 *   - `event` server-pushed notification (no response expected)
 *
 * `channel` namespaces the payload (sessions, chat, terminal, files, ...).
 */
export const EnvelopeType = z.enum(['req', 'res', 'event']);
export type EnvelopeType = z.infer<typeof EnvelopeType>;

export const Channel = z.enum([
  'meta', // ping/pong, version handshake
  'sessions',
  'chat',
  'terminal',
  'files',
  'diff',
  'git',
  'ports',
  'notifications',
]);
export type Channel = z.infer<typeof Channel>;

export const Envelope = z.object({
  /** ULID-style correlation id. Same value flows through req → res. */
  id: z.string().min(1),
  type: EnvelopeType,
  channel: Channel,
  /** Optional method name within the channel (e.g. `sessions.create`). */
  method: z.string().optional(),
  /** Channel-specific payload. Kept as `unknown` here; refined per channel. */
  payload: z.unknown().optional(),
  /** Present on `res` envelopes when the request failed. */
  error: z
    .object({
      code: z.string(),
      message: z.string(),
      data: z.unknown().optional(),
    })
    .optional(),
});
export type Envelope = z.infer<typeof Envelope>;

/**
 * Build a `req` envelope. `id` is the caller's responsibility — typically
 * a ULID — so that the same id can be correlated on response.
 */
export function makeRequest<P>(args: {
  id: string;
  channel: Channel;
  method: string;
  payload?: P;
}): Envelope {
  return {
    id: args.id,
    type: 'req',
    channel: args.channel,
    method: args.method,
    payload: args.payload,
  };
}

/** Build a `res` envelope echoing the request `id`. */
export function makeResponse<P>(args: {
  id: string;
  channel: Channel;
  payload?: P;
  error?: Envelope['error'];
}): Envelope {
  return {
    id: args.id,
    type: 'res',
    channel: args.channel,
    payload: args.payload,
    error: args.error,
  };
}

/** Build an `event` envelope. The id is allocated freshly by the sender. */
export function makeEvent<P>(args: {
  id: string;
  channel: Channel;
  method: string;
  payload?: P;
}): Envelope {
  return {
    id: args.id,
    type: 'event',
    channel: args.channel,
    method: args.method,
    payload: args.payload,
  };
}
