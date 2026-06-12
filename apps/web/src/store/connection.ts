import {
  type ConnectionState,
  PairingClient,
  RelayWSClient,
  type CredentialBundle,
} from '@coding-anywhere/protocol';
import { create } from 'zustand';

import {
  clearCredentials,
  loadCredentials,
  saveCredentials,
  type StoredCredentials,
  updateAccessToken,
} from './credentials.js';

const REFRESH_SKEW_MS = 60_000;

interface ConnectionStore {
  // State
  credentials: StoredCredentials | null;
  status: ConnectionState;
  client: RelayWSClient | null;
  lastError: string | null;
  refreshTimer: ReturnType<typeof setTimeout> | null;
  /**
   * A plaintext account key that was just minted (first pair, or rotate) and
   * has not yet been acknowledged by the UI. Components display it once in a
   * "save this somewhere safe" modal, then call dismissKeyReveal().
   */
  pendingKeyReveal: string | null;
  /**
   * UI flag for the revoke-key confirmation modal. Lives in the store
   * because the trigger (a Menu.Item) and the modal render in different
   * subtrees: the Menu.Dropdown subtree unmounts on item click, so any
   * component-local useState there gets wiped before the modal can render.
   */
  revokeKeyOpen: boolean;
  /** Last error from a key-management action (show/rotate/revoke). */
  keyActionError: string | null;
  /** UI flag for the "share read-only ask link" modal. */
  shareAskOpen: boolean;

  // Actions
  hydrate(): Promise<void>;
  signIn(creds: StoredCredentials): void;
  signInWithAccountKey(args: {
    relay_url: string;
    account_key: string;
    device_label: string;
  }): Promise<void>;
  signOut(): void;
  refreshAccessToken(): Promise<void>;
  /** Rotate the account key on the relay; returns the new plaintext value. */
  rotateAccountKey(): Promise<string>;
  /** Clear the account key + revoke all sessions; signs out locally. */
  revokeAccountKey(): Promise<void>;
  /** Mark the pending key as shown; clears it from store state. */
  dismissKeyReveal(): void;
  /**
   * Surface the cached account key into pendingKeyReveal so the reveal
   * modal opens. Sets keyActionError if the local credentials lack a key
   * (e.g. paired on an older client).
   */
  requestRevealKey(): void;
  setRevokeKeyOpen(open: boolean): void;
  setKeyActionError(message: string | null): void;
  setShareAskOpen(open: boolean): void;
  /**
   * Mint a read-only "ask"-mode guest link and return a ready-to-share URL
   * (this app's origin + /guest) plus the expiry (unix ms).
   */
  createAskShare(args: {
    ttlSeconds: number;
    promptLimit: number;
    maxSessions: number;
    cwd: string;
    model: string;
    agentId: string;
  }): Promise<{ url: string; expiresAt: number }>;
  /**
   * Revoke a guest share link by id: the relay marks it revoked and drops
   * live guest connections. Already-created guest sessions stay on the
   * daemon (close them from the session list).
   */
  revokeAskShare(shareId: string): Promise<void>;
  setStatus(s: ConnectionState): void;
  setError(message: string | null): void;
}

export const useConnection = create<ConnectionStore>((set, get) => ({
  credentials: null,
  status: 'disconnected',
  client: null,
  lastError: null,
  refreshTimer: null,
  pendingKeyReveal: null,
  revokeKeyOpen: false,
  keyActionError: null,
  shareAskOpen: false,

  async hydrate() {
    const creds = loadCredentials();
    if (!creds) return;
    try {
      const fresh = await refreshIfNeeded(creds);
      const saved = saveCredentials(fresh);
      attachClient(set, get, saved);
    } catch (err) {
      clearCredentials();
      set({
        credentials: null,
        client: null,
        status: 'disconnected',
        lastError: err instanceof Error ? err.message : 'Session expired',
      });
    }
  },

  signIn(creds) {
    const saved = saveCredentials(creds);
    attachClient(set, get, saved);
    if (saved.bundle.account_key) {
      set({ pendingKeyReveal: saved.bundle.account_key });
    }
  },

  dismissKeyReveal() {
    set({ pendingKeyReveal: null });
  },

  requestRevealKey() {
    const { credentials } = get();
    const key = credentials?.bundle.account_key;
    if (key) {
      set({ pendingKeyReveal: key, keyActionError: null });
      return;
    }
    set({
      keyActionError:
        '本地凭据里没有账户密钥（可能在之前的版本配的对）。请用"重置账户密钥"重新生成一个。',
    });
  },

  setRevokeKeyOpen(open) {
    set({ revokeKeyOpen: open, keyActionError: open ? null : get().keyActionError });
  },

  setKeyActionError(message) {
    set({ keyActionError: message });
  },

  setShareAskOpen(open) {
    set({ shareAskOpen: open });
  },

  async createAskShare({ ttlSeconds, promptLimit, maxSessions, cwd, model, agentId }) {
    const { credentials } = get();
    if (!credentials) throw new Error('not signed in');
    const pc = new PairingClient(credentials.relay_url);
    const res = await pc.createAskShare(
      {
        ttl_seconds: ttlSeconds,
        prompt_limit: promptLimit,
        max_sessions: maxSessions,
        cwd,
        model,
        agent_id: agentId,
      },
      { bearer: credentials.bundle.access_token },
    );
    // Build the link from this app's origin (where /guest is served) and the
    // relay URL the guest page needs to dial. The guest token rides in the
    // query so a freshly opened tab connects with no further input.
    const relay = guestRelayURL(credentials.relay_url, window.location.hostname);
    const url =
      `${window.location.origin}/guest?token=${encodeURIComponent(res.token)}` +
      `&relay=${encodeURIComponent(relay)}`;
    return { url, expiresAt: res.expires_at };
  },

  async revokeAskShare(shareId) {
    const { credentials } = get();
    if (!credentials) throw new Error('not signed in');
    const pc = new PairingClient(credentials.relay_url);
    await pc.revokeAskShare(shareId, { bearer: credentials.bundle.access_token });
  },

  async signInWithAccountKey({ relay_url, account_key, device_label }) {
    const pc = new PairingClient(relay_url);
    const { credentials: bundle } = await pc.login({
      account_key,
      device_label,
      device_kind: 'client',
    });
    // The login response never echoes account_key back. Persist the one the
    // user just used so subsequent rotate/show flows have it available.
    const saved = saveCredentials({
      relay_url,
      bundle: { ...bundle, account_key },
    });
    attachClient(set, get, saved);
  },

  async rotateAccountKey() {
    const { credentials } = get();
    if (!credentials) throw new Error('not signed in');
    const pc = new PairingClient(credentials.relay_url);
    const { account_key } = await pc.rotateAccountKey({
      bearer: credentials.bundle.access_token,
    });
    const next: StoredCredentials = {
      ...credentials,
      bundle: { ...credentials.bundle, account_key },
    };
    const saved = saveCredentials(next);
    set({ credentials: saved, pendingKeyReveal: account_key });
    return account_key;
  },

  async revokeAccountKey() {
    const { credentials, client, refreshTimer } = get();
    if (!credentials) throw new Error('not signed in');
    const pc = new PairingClient(credentials.relay_url);
    await pc.revokeAccountKey({ bearer: credentials.bundle.access_token });
    // Server has invalidated our refresh token too — drop everything locally.
    if (client) client.stop();
    if (refreshTimer) clearTimeout(refreshTimer);
    clearCredentials();
    set({
      credentials: null,
      client: null,
      status: 'disconnected',
      lastError: null,
      refreshTimer: null,
      revokeKeyOpen: false,
      keyActionError: null,
      pendingKeyReveal: null,
    });
  },

  signOut() {
    const { client, refreshTimer } = get();
    if (client) client.stop();
    if (refreshTimer) clearTimeout(refreshTimer);
    clearCredentials();
    set({
      credentials: null,
      client: null,
      status: 'disconnected',
      lastError: null,
      refreshTimer: null,
      revokeKeyOpen: false,
      keyActionError: null,
      pendingKeyReveal: null,
    });
  },

  async refreshAccessToken() {
    const { credentials } = get();
    if (!credentials) return;
    const fresh = await refreshNow(credentials);
    const saved = saveCredentials(fresh);
    const { client } = get();
    if (client) client.setAccessToken(saved.bundle.access_token);
    set({ credentials: saved, lastError: null });
    scheduleRefresh(set, get, saved);
  },

  setStatus(s) {
    set({ status: s });
  },

  setError(message) {
    set({ lastError: message });
  },
}));

function attachClient(
  set: (partial: Partial<ConnectionStore>) => void,
  get: () => ConnectionStore,
  creds: StoredCredentials,
) {
  const { client: previous, refreshTimer } = get();
  if (previous) previous.stop();
  if (refreshTimer) clearTimeout(refreshTimer);
  const client = new RelayWSClient({
    relayURL: creds.relay_url,
    accessToken: creds.bundle.access_token,
  });
  client.onState((s) => set({ status: s }));
  client.handle('meta', () => {
    /* no-op: pings are handled inside the client */
  });
  set({ credentials: creds, client, status: 'connecting', lastError: null });
  scheduleRefresh(set, get, creds);
  client.start();
}

async function refreshIfNeeded(creds: StoredCredentials): Promise<StoredCredentials> {
  const expiresAt = creds.access_token_expires_at ?? 0;
  if (expiresAt - Date.now() > REFRESH_SKEW_MS) return creds;
  return refreshNow(creds);
}

async function refreshNow(creds: StoredCredentials): Promise<StoredCredentials> {
  const res = await new PairingClient(creds.relay_url).refresh(creds.bundle.refresh_token);
  return updateAccessToken(creds, res.access_token, res.access_token_expires_in);
}

function scheduleRefresh(
  set: (partial: Partial<ConnectionStore>) => void,
  get: () => ConnectionStore,
  creds: StoredCredentials,
) {
  const current = get().refreshTimer;
  if (current) clearTimeout(current);
  const delay = Math.max(0, (creds.access_token_expires_at ?? 0) - Date.now() - REFRESH_SKEW_MS);
  const refreshTimer = setTimeout(() => {
    get()
      .refreshAccessToken()
      .catch((err: unknown) => {
        const { client } = get();
        if (client) client.stop();
        clearCredentials();
        set({
          credentials: null,
          client: null,
          status: 'disconnected',
          lastError: err instanceof Error ? err.message : 'Session expired',
          refreshTimer: null,
        });
      });
  }, delay);
  set({ refreshTimer });
}

/** Returns just the credential bundle if signed in. */
export function selectBundle(s: ConnectionStore): CredentialBundle | null {
  return s.credentials?.bundle ?? null;
}

/**
 * The relay URL a GUEST's browser should dial, given the relay URL stored in
 * this client's credentials and the host the app is currently viewed from.
 *
 * Pairing on the dev box typically stores `http://localhost:8443` — correct
 * for THIS browser, dead wrong inside a share link opened on another machine
 * (it would point at the guest's own loopback, which either refuses fast or,
 * worse, hits whatever the guest happens to run on that port and hangs the
 * page on "正在连接"). The dev stack serves web and relay from the same
 * machine, so when the stored relay host is loopback and the page is being
 * viewed from a non-loopback host, substitute the page's hostname (keeping
 * the relay's scheme + port). Exported for tests.
 */
export function guestRelayURL(relayURL: string, pageHost: string): string {
  try {
    const u = new URL(relayURL);
    if (isLoopbackHost(u.hostname) && pageHost && !isLoopbackHost(pageHost)) {
      u.hostname = pageHost;
      return u.toString().replace(/\/$/, '');
    }
  } catch {
    // Malformed relay URL — leave it as-is; connecting will surface it.
  }
  return relayURL;
}

function isLoopbackHost(host: string): boolean {
  const h = host.replace(/^\[|\]$/g, '').toLowerCase();
  return h === 'localhost' || h === '127.0.0.1' || h === '::1';
}
