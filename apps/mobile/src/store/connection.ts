// Mobile connection store — React Native counterpart of apps/web/src/store/connection.ts.
//
// Same shape and behaviour as the web store (credential bundle + a long-lived
// RelayWSClient + background access-token refresh + machine switcher), with two
// differences:
//   - credential I/O is async (SecureStore), so signIn/hydrate/refresh await it;
//   - the web-only guest-share link builder (window.location) and the account-key
//     reveal/rotate modal state are omitted for v1.
//
// Mobile-local for now; lifts into packages/state later (see plan).
import {
  type ConnectionState,
  PairingClient,
  RelayWSClient,
  type UserAccountSummary,
} from '@coding-anywhere/protocol';
import { setClientProvider } from '@coding-anywhere/state';
import { create } from 'zustand';

import {
  clearCredentials,
  loadCredentials,
  saveCredentials,
  type StoredCredentials,
  updateAccessToken,
} from './credentials';

const REFRESH_SKEW_MS = 60_000;

interface ConnectionStore {
  // State
  hydrated: boolean;
  credentials: StoredCredentials | null;
  status: ConnectionState;
  client: RelayWSClient | null;
  lastError: string | null;
  refreshTimer: ReturnType<typeof setTimeout> | null;
  /** Sibling machines (accounts) the current login can switch between. */
  machines: UserAccountSummary[];

  // Actions
  hydrate(): Promise<void>;
  /** Sign in with an already-claimed credential bundle (6-digit pair code path). */
  signIn(creds: StoredCredentials): Promise<void>;
  signInWithAccountKey(args: {
    relay_url: string;
    account_key: string;
    device_label: string;
  }): Promise<void>;
  /**
   * Log in with username + password. When the user owns multiple machines and
   * `account_id` is omitted, returns `signedIn: false` plus the machine list so
   * the caller can show a picker, then call again with a chosen account.
   */
  signInWithPassword(args: {
    relay_url: string;
    username: string;
    password: string;
    device_label: string;
    account_id?: string;
  }): Promise<{ accounts: UserAccountSummary[]; signedIn: boolean }>;
  loadMachines(): Promise<void>;
  switchMachine(accountId: string): Promise<void>;
  signOut(): Promise<void>;
  refreshAccessToken(): Promise<void>;
  setStatus(s: ConnectionState): void;
  setError(message: string | null): void;
}

export const useConnection = create<ConnectionStore>((set, get) => ({
  hydrated: false,
  credentials: null,
  status: 'disconnected',
  client: null,
  lastError: null,
  refreshTimer: null,
  machines: [],

  async hydrate() {
    try {
      const creds = await loadCredentials();
      if (!creds) return;
      const fresh = await refreshIfNeeded(creds);
      const saved = await saveCredentials(fresh);
      attachClient(set, get, saved);
    } catch (err) {
      await clearCredentials();
      set({
        credentials: null,
        client: null,
        status: 'disconnected',
        lastError: err instanceof Error ? err.message : 'Session expired',
      });
    } finally {
      set({ hydrated: true });
    }
  },

  async signIn(creds) {
    const saved = await saveCredentials(creds);
    attachClient(set, get, saved);
  },

  async signInWithAccountKey({ relay_url, account_key, device_label }) {
    const pc = new PairingClient(relay_url);
    const { credentials: bundle } = await pc.login({
      account_key,
      device_label,
      device_kind: 'client',
    });
    // The login response never echoes account_key back; persist the one used.
    const saved = await saveCredentials({ relay_url, bundle: { ...bundle, account_key } });
    attachClient(set, get, saved);
  },

  async signInWithPassword({ relay_url, username, password, device_label, account_id }) {
    const pc = new PairingClient(relay_url);
    const res = await pc.userLogin({ username, password, device_label, account_id });
    if (res.credentials) {
      const saved = await saveCredentials({ relay_url, bundle: res.credentials });
      attachClient(set, get, saved);
      return { accounts: res.accounts, signedIn: true };
    }
    return { accounts: res.accounts, signedIn: false };
  },

  async loadMachines() {
    const { credentials } = get();
    if (!credentials) return;
    try {
      const pc = new PairingClient(credentials.relay_url);
      const res = await pc.listMachines({ bearer: credentials.bundle.access_token });
      set({ machines: res.accounts });
    } catch {
      set({ machines: [] });
    }
  },

  async switchMachine(accountId) {
    const { credentials } = get();
    if (!credentials) throw new Error('not signed in');
    if (accountId === credentials.bundle.account_id) return;
    const pc = new PairingClient(credentials.relay_url);
    const res = await pc.switchMachine(
      { account_id: accountId, device_label: credentials.bundle.device_label },
      { bearer: credentials.bundle.access_token },
    );
    const saved = await saveCredentials({
      relay_url: credentials.relay_url,
      bundle: res.credentials,
    });
    attachClient(set, get, saved);
    set({ machines: res.accounts });
  },

  async signOut() {
    const { client, refreshTimer } = get();
    if (client) client.stop();
    if (refreshTimer) clearTimeout(refreshTimer);
    await clearCredentials();
    set({
      credentials: null,
      client: null,
      status: 'disconnected',
      lastError: null,
      refreshTimer: null,
      machines: [],
    });
  },

  async refreshAccessToken() {
    const { credentials } = get();
    if (!credentials) return;
    const fresh = await refreshNow(credentials);
    const saved = await saveCredentials(fresh);
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

// Let the shared channel stores (@coding-anywhere/state: chat/diff/ports) reach
// the live relay client without importing this platform-specific store.
setClientProvider(() => useConnection.getState().client);

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
  void get().loadMachines();
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
      .catch(async (err: unknown) => {
        const { client } = get();
        if (client) client.stop();
        await clearCredentials();
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
