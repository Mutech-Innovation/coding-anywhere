-- VAPID keypair used to authenticate the relay to push services. We keep a
-- single pair and rotate by generating a new row + updating callers.

CREATE TABLE vapid_keys (
    id          TEXT PRIMARY KEY,
    public_key  TEXT NOT NULL,
    private_key TEXT NOT NULL,
    created_at  INTEGER NOT NULL
) STRICT;

-- Per-device Web Push subscriptions. Endpoints are unique per account so
-- re-subscribing replaces the old row instead of duplicating.

CREATE TABLE push_subscriptions (
    id          TEXT PRIMARY KEY,
    account_id  TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    endpoint    TEXT NOT NULL,
    p256dh_key  TEXT NOT NULL,
    auth_key    TEXT NOT NULL,
    user_agent  TEXT,
    created_at  INTEGER NOT NULL,
    UNIQUE(account_id, endpoint)
) STRICT;

CREATE INDEX idx_push_subscriptions_account ON push_subscriptions(account_id);
