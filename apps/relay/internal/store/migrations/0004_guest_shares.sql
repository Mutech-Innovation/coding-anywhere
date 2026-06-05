-- Guest "ask"-mode share links.
--
-- A guest share is a short-lived, read-only entry point that lets someone
-- chat with the account's Devin in ask mode through a public link, without
-- pairing. The share's id doubles as the device id embedded in the guest
-- JWT so the relay can re-check revocation and per-link limits on connect.
--
-- No refresh token is ever issued for a guest; the JWT simply expires. The
-- relay additionally restricts guest connections to a safe chat subset and
-- the daemon forces ask mode + auto-denies permission requests.

CREATE TABLE guest_shares (
    id           TEXT PRIMARY KEY,
    account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    revoked      INTEGER NOT NULL DEFAULT 0,
    -- Caller-chosen limits, fixed at issuance. prompt_limit 0 == unlimited;
    -- max_sessions <= 0 is treated as 1 by the relay.
    prompt_limit INTEGER NOT NULL DEFAULT 0,
    max_sessions INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE INDEX idx_guest_shares_account ON guest_shares(account_id);
