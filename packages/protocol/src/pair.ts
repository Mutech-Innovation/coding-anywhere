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
  /**
   * Long-lived account key (format: `ca_key_<43 char base64url>`). Returned
   * once at account creation and after explicit rotate; absent otherwise.
   * Persist it; other clients use it to log in without re-pairing.
   */
  account_key: z.string().optional(),
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

export const LoginResponse = z.object({
  credentials: CredentialBundle,
});
export type LoginResponse = z.infer<typeof LoginResponse>;

export const RotateKeyResponse = z.object({
  account_key: z.string(),
});
export type RotateKeyResponse = z.infer<typeof RotateKeyResponse>;

/** Response from POST /v1/share/ask — a short-lived read-only "ask" link. */
export const AskShareResponse = z.object({
  share_id: z.string(),
  token: z.string(),
  expires_at: z.number(), // unix milliseconds
  expires_in: z.number(), // seconds
  prompt_limit: z.number(),
  max_sessions: z.number(),
  url: z.string().optional(),
});
export type AskShareResponse = z.infer<typeof AskShareResponse>;

/**
 * Canonical prefix on every account key. Used for cheap shape validation in
 * UI forms before hitting the network.
 */
export const ACCOUNT_KEY_PREFIX = 'ca_key_';

/** Cheap structural check; full validation happens server-side. */
export function isAccountKeyShape(value: string): boolean {
  if (!value.startsWith(ACCOUNT_KEY_PREFIX)) return false;
  const body = value.slice(ACCOUNT_KEY_PREFIX.length);
  if (body.length < 32) return false;
  return /^[A-Za-z0-9_-]+$/.test(body);
}

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

  /**
   * Trade an account key for a freshly issued credential bundle. Used by
   * browsers that already have a long-lived key (from a previous pair, or
   * shown by `coding-anywhere-daemon key show`).
   */
  login(
    args: { account_key: string; device_label: string; device_kind?: DeviceKind },
    opts?: RequestOpts,
  ) {
    return postJSON(
      `${this.base}/v1/auth/login`,
      { device_kind: 'client' as DeviceKind, ...args },
      LoginResponse,
      opts,
    );
  }

  /**
   * Rotate the account key. Returns the new plaintext value, which must be
   * stored by the caller — the relay never shows it again.
   */
  rotateAccountKey(opts: RequestOpts & { bearer: string }) {
    return postJSON(`${this.base}/v1/auth/key/rotate`, {}, RotateKeyResponse, opts);
  }

  /**
   * Mint a short-lived, read-only "ask"-mode guest link. The caller's limits
   * (ttl/prompt/session caps) are fixed at issuance. Returns a guest token the
   * guest page presents to the relay; the link itself is built by the caller.
   */
  createAskShare(
    args: {
      ttl_seconds?: number;
      prompt_limit?: number;
      max_sessions?: number;
      cwd?: string;
      model?: string;
      /** Owner-chosen chat agent ("devin", "cursor-cli"); empty = daemon default. */
      agent_id?: string;
    },
    opts: RequestOpts & { bearer: string },
  ) {
    return postJSON(`${this.base}/v1/share/ask`, args, AskShareResponse, opts);
  }

  /**
   * Revoke a guest share link by id. The relay marks it revoked and drops any
   * live guest connection using it; already-created sessions stay on the
   * daemon until closed.
   */
  async revokeAskShare(shareId: string, opts: RequestOpts & { bearer: string }): Promise<void> {
    const res = await fetch(`${this.base}/v1/share/${encodeURIComponent(shareId)}`, {
      method: 'DELETE',
      headers: { authorization: `Bearer ${opts.bearer}` },
      signal: opts.signal,
    });
    if (!res.ok && res.status !== 204) {
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
  }

  /**
   * Clear the account key AND revoke every refresh token on the account.
   * Use only when the key (or a device's credentials) is suspected
   * compromised; recovery requires a fresh pair flow.
   */
  async revokeAccountKey(opts: RequestOpts & { bearer: string }): Promise<void> {
    const res = await fetch(`${this.base}/v1/auth/key`, {
      method: 'DELETE',
      headers: { authorization: `Bearer ${opts.bearer}` },
      signal: opts.signal,
    });
    if (!res.ok && res.status !== 204) {
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
  }
}
