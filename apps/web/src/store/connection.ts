import {
  type ConnectionState,
  RelayWSClient,
  type CredentialBundle,
} from '@coding-anywhere/protocol';
import { create } from 'zustand';

import {
  clearCredentials,
  loadCredentials,
  saveCredentials,
  type StoredCredentials,
} from './credentials.js';

interface ConnectionStore {
  // State
  credentials: StoredCredentials | null;
  status: ConnectionState;
  client: RelayWSClient | null;
  lastError: string | null;

  // Actions
  hydrate(): void;
  signIn(creds: StoredCredentials): void;
  signOut(): void;
  setStatus(s: ConnectionState): void;
  setError(message: string | null): void;
}

export const useConnection = create<ConnectionStore>((set, get) => ({
  credentials: null,
  status: 'disconnected',
  client: null,
  lastError: null,

  hydrate() {
    const creds = loadCredentials();
    if (creds) attachClient(set, get, creds);
  },

  signIn(creds) {
    saveCredentials(creds);
    attachClient(set, get, creds);
  },

  signOut() {
    const { client } = get();
    if (client) client.stop();
    clearCredentials();
    set({ credentials: null, client: null, status: 'disconnected', lastError: null });
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
  const previous = get().client;
  if (previous) previous.stop();
  const client = new RelayWSClient({
    relayURL: creds.relay_url,
    accessToken: creds.bundle.access_token,
  });
  client.onState((s) => set({ status: s }));
  client.handle('meta', () => {
    /* no-op: pings are handled inside the client */
  });
  set({ credentials: creds, client, status: 'connecting', lastError: null });
  client.start();
}

/** Returns just the credential bundle if signed in. */
export function selectBundle(s: ConnectionStore): CredentialBundle | null {
  return s.credentials?.bundle ?? null;
}
