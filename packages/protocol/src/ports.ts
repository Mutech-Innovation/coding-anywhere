// Wire types for the `ports` channel. Mirrors internal/wire/ports.go.

import { z } from 'zod';

import { toHttpURL } from './pair.js';

export const PortsMethod = {
  List: 'list',
  Fetch: 'fetch',
} as const;
export type PortsMethod = (typeof PortsMethod)[keyof typeof PortsMethod];

export const PortsEntry = z.object({
  port: z.number(),
  address: z.string(),
  family: z.string(),
  pid: z.number().optional(),
  command: z.string().optional(),
});
export type PortsEntry = z.infer<typeof PortsEntry>;

export const PortsListResponse = z.object({
  entries: z.array(PortsEntry),
});
export type PortsListResponse = z.infer<typeof PortsListResponse>;

/**
 * Build a relay URL that proxies HTTP traffic to a localhost port on the
 * dev machine. The token is included as a query param so opening the URL in
 * a fresh tab works without a custom Authorization header.
 *
 * The relay URL is stored in its WebSocket form (`ws[s]://`), but `/v1/proxy`
 * is a plain HTTP endpoint opened as a page in a browser tab, so the scheme is
 * mapped to `http[s]://` (a `wss://` href fails with ERR_UNKNOWN_URL_SCHEME).
 */
export function proxyURL(args: {
  relayURL: string;
  port: number;
  path?: string;
  token: string;
}): string {
  const base = toHttpURL(args.relayURL).replace(/\/$/, '');
  const path = args.path ?? '/';
  const safePath = path.startsWith('/') ? path : `/${path}`;
  return `${base}/v1/proxy/${args.port}${safePath}?token=${encodeURIComponent(args.token)}`;
}
