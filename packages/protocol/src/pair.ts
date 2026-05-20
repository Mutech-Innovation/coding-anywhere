// REST types and a thin client for the relay's pairing endpoints.
//
// All requests are plain `fetch`; this works in the browser, in Node 18+,
// and in React Native without further plumbing.

import { z } from 'zod';

export const DeviceKind = z.enum(['daemon', 'client']);
export type DeviceKind = z.infer<typeof DeviceKind>;

export const CredentialBundle = z.object({
  account_id: z.string(),
  device_id: z.string(),
  device_kind: DeviceKind,
  device_label: z.string(),
  access_token: z.string(),
  access_token_expires_in: z.number(),
  refresh_token: z.string(),
  refresh_token_expires_in: z.number(),
  relay_protocol_version: z.number(),
});
export type CredentialBundle = z.infer<typeof CredentialBundle>;

export const InitResponse = z.object({
  pairing_token: z.string(),
  code: z.string(),
  expires_in: z.number(),
});
export type InitResponse = z.infer<typeof InitResponse>;

export const StatusResponse = z.object({
  status: z.enum(['pending', 'claimed', 'expired']),
  credentials: CredentialBundle.optional(),
});
export type StatusResponse = z.infer<typeof StatusResponse>;

export const ClaimResponse = z.object({
  credentials: CredentialBundle,
});
export type ClaimResponse = z.infer<typeof ClaimResponse>;

export const RefreshResponse = z.object({
  access_token: z.string(),
  access_token_expires_in: z.number(),
});
export type RefreshResponse = z.infer<typeof RefreshResponse>;

/**
 * Maps `ws[s]://` → `http[s]://` so the same configured relay URL works for
 * both REST and WebSocket transports.
 */
export function toHttpURL(relayURL: string): string {
  if (relayURL.startsWith('ws://')) return 'http://' + relayURL.slice('ws://'.length);
  if (relayURL.startsWith('wss://')) return 'https://' + relayURL.slice('wss://'.length);
  return relayURL;
}

export function toWsURL(relayURL: string): string {
  if (relayURL.startsWith('http://')) return 'ws://' + relayURL.slice('http://'.length);
  if (relayURL.startsWith('https://')) return 'wss://' + relayURL.slice('https://'.length);
  return relayURL;
}

/** Error returned by the relay's REST endpoints. */
export class RelayError extends Error {
  public override readonly name = 'RelayError';
  public readonly status: number;
  public readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

interface RequestOpts {
  bearer?: string;
  signal?: AbortSignal;
}

async function postJSON<T>(
  url: string,
  body: unknown,
  schema: z.ZodType<T>,
  opts: RequestOpts = {},
): Promise<T> {
  const res = await fetch(url, {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      ...(opts.bearer ? { authorization: `Bearer ${opts.bearer}` } : {}),
    },
    body: JSON.stringify(body),
    signal: opts.signal,
  });
  return parseJSONOrThrow(res, schema);
}

async function getJSON<T>(url: string, schema: z.ZodType<T>, opts: RequestOpts = {}): Promise<T> {
  const res = await fetch(url, {
    headers: opts.bearer ? { authorization: `Bearer ${opts.bearer}` } : {},
    signal: opts.signal,
  });
  return parseJSONOrThrow(res, schema);
}

async function parseJSONOrThrow<T>(res: Response, schema: z.ZodType<T>): Promise<T> {
  if (!res.ok) {
    let code = 'http_error';
    let message = res.statusText;
    try {
      const body = (await res.json()) as { code?: string; message?: string };
      if (body.code) code = body.code;
      if (body.message) message = body.message;
    } catch {
      // ignore
    }
    throw new RelayError(res.status, code, message);
  }
  const data = (await res.json()) as unknown;
  return schema.parse(data);
}

/**
 * Shape mirroring the relay's pair API. Construct one per relay URL.
 */
export class PairingClient {
  private readonly base: string;

  constructor(relayURL: string) {
    this.base = toHttpURL(relayURL).replace(/\/$/, '');
  }

  init(args: { device_kind: DeviceKind; device_label: string }, opts?: RequestOpts) {
    return postJSON(`${this.base}/v1/pair/init`, args, InitResponse, opts);
  }

  claim(args: { code: string; device_kind: DeviceKind; device_label: string }, opts?: RequestOpts) {
    return postJSON(`${this.base}/v1/pair/claim`, args, ClaimResponse, opts);
  }

  status(pairing_token: string, opts?: RequestOpts) {
    const u = `${this.base}/v1/pair/status?pairing_token=${encodeURIComponent(pairing_token)}`;
    return getJSON(u, StatusResponse, opts);
  }

  refresh(refresh_token: string, opts?: RequestOpts) {
    return postJSON(`${this.base}/v1/auth/refresh`, { refresh_token }, RefreshResponse, opts);
  }
}
