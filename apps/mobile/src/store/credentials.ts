// Mobile credential storage — the React Native counterpart of the web app's
// apps/web/src/store/credentials.ts. The pure helpers (normalize / updateAccessToken)
// are identical; only the load/save/clear I/O differs: the browser uses
// localStorage (synchronous), here we use expo-secure-store (async, Keychain-backed).
//
// NOTE: SecureStore keys may only contain [A-Za-z0-9._-], so the web's
// "coding-anywhere/credentials" key becomes an underscore form here. These stores
// are mobile-local for now; they get lifted into packages/state later (see plan).
import type { CredentialBundle } from '@coding-anywhere/protocol';
import * as SecureStore from 'expo-secure-store';

const STORAGE_KEY = 'coding_anywhere_credentials';

export interface StoredCredentials {
  relay_url: string;
  bundle: CredentialBundle;
  access_token_expires_at?: number;
  refresh_token_expires_at?: number;
}

export function normalizeCredentials(
  creds: StoredCredentials,
  now = Date.now(),
  accessTokenExpiresAt?: number,
): StoredCredentials {
  return {
    ...creds,
    access_token_expires_at:
      accessTokenExpiresAt ??
      creds.access_token_expires_at ??
      now + creds.bundle.access_token_expires_in * 1000,
    refresh_token_expires_at:
      creds.refresh_token_expires_at ?? now + creds.bundle.refresh_token_expires_in * 1000,
  };
}

export function updateAccessToken(
  creds: StoredCredentials,
  accessToken: string,
  accessTokenExpiresIn: number,
  now = Date.now(),
): StoredCredentials {
  return normalizeCredentials(
    {
      ...creds,
      bundle: {
        ...creds.bundle,
        access_token: accessToken,
        access_token_expires_in: accessTokenExpiresIn,
      },
    },
    now,
    now + accessTokenExpiresIn * 1000,
  );
}

export async function loadCredentials(now = Date.now()): Promise<StoredCredentials | null> {
  try {
    const raw = await SecureStore.getItemAsync(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as StoredCredentials;
    if (!parsed.relay_url || !parsed.bundle?.access_token || !parsed.bundle?.refresh_token)
      return null;
    return normalizeCredentials(parsed, now, parsed.access_token_expires_at ?? 0);
  } catch {
    return null;
  }
}

export async function saveCredentials(
  creds: StoredCredentials,
  now = Date.now(),
): Promise<StoredCredentials> {
  const normalized = normalizeCredentials(creds, now);
  await SecureStore.setItemAsync(STORAGE_KEY, JSON.stringify(normalized));
  return normalized;
}

export async function clearCredentials(): Promise<void> {
  await SecureStore.deleteItemAsync(STORAGE_KEY);
}
