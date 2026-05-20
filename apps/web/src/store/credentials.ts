import type { CredentialBundle } from '@coding-anywhere/protocol';

const STORAGE_KEY = 'coding-anywhere/credentials';

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

export function loadCredentials(now = Date.now()): StoredCredentials | null {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as StoredCredentials;
    if (!parsed.relay_url || !parsed.bundle?.access_token || !parsed.bundle?.refresh_token)
      return null;
    return normalizeCredentials(parsed, now, parsed.access_token_expires_at ?? 0);
  } catch {
    return null;
  }
}

export function saveCredentials(creds: StoredCredentials, now = Date.now()): StoredCredentials {
  const normalized = normalizeCredentials(creds, now);
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(normalized));
  return normalized;
}

export function clearCredentials(): void {
  window.localStorage.removeItem(STORAGE_KEY);
}
