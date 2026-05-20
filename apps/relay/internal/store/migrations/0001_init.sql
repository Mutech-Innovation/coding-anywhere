-- Initial schema for the coding-anywhere relay.
--
-- Storage is intentionally narrow: the relay routes traffic and authenticates
-- devices, but does not store conversation content or files. Anything sensitive
-- written here (refresh tokens, pairing-time credentials) is short-lived or
-- one-way hashed.

CREATE TABLE accounts (
    id           TEXT PRIMARY KEY,
    created_at   INTEGER NOT NULL
) STRICT;

CREATE TABLE devices (
    id            TEXT PRIMARY KEY,
    account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL CHECK (kind IN ('daemon','client')),
    label         TEXT NOT NULL,
    created_at    INTEGER NOT NULL,
    last_seen_at  INTEGER
) STRICT;
CREATE INDEX idx_devices_account_id ON devices(account_id);

CREATE TABLE pairings (
    pairing_token            TEXT PRIMARY KEY,
    code                     TEXT NOT NULL UNIQUE,
    initiator_account_id     TEXT REFERENCES accounts(id) ON DELETE SET NULL,
    initiator_device_kind    TEXT NOT NULL CHECK (initiator_device_kind IN ('daemon','client')),
    initiator_device_label   TEXT NOT NULL,
    initiator_device_id      TEXT REFERENCES devices(id) ON DELETE SET NULL,
    created_at               INTEGER NOT NULL,
    expires_at               INTEGER NOT NULL,
    claimed                  INTEGER NOT NULL DEFAULT 0,
    claimed_account_id       TEXT REFERENCES accounts(id) ON DELETE SET NULL,
    claimed_device_id        TEXT REFERENCES devices(id) ON DELETE SET NULL,
    claimed_credentials_json TEXT
) STRICT;
CREATE INDEX idx_pairings_code ON pairings(code);

CREATE TABLE refresh_tokens (
    token_hash    TEXT PRIMARY KEY,
    account_id    TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    device_id     TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    created_at    INTEGER NOT NULL,
    expires_at    INTEGER NOT NULL,
    revoked       INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE INDEX idx_refresh_account_device ON refresh_tokens(account_id, device_id);
