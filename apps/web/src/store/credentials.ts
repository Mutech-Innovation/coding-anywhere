import type { CredentialBundle } from '@coding-anywhere/protocol';

const STORAGE_KEY = 'coding-anywhere/credentials';

export interface StoredCredentials {
  relay_url: string;
  bundle: CredentialBundle;
}

export function loadCredentials(): StoredCredentials | null {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as StoredCredentials;
    if (!parsed.relay_url || !parsed.bundle?.access_token) return null;
    return parsed;
  } catch {
    return null;
  }
}

export function saveCredentials(creds: StoredCredentials): void {
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(creds));
}

export function clearCredentials(): void {
  window.localStorage.removeItem(STORAGE_KEY);
}
