-- Human identity layer on top of accounts.
--
-- Until now the only "login" was the long-lived account key (a random
-- bearer string). This adds a real user: a username + password (stored only
-- as an argon2id PHC hash, never plaintext) that can own one or more
-- accounts. That lets a single operator log in from any browser with a
-- memorable credential and reach every dev machine they own, instead of
-- pasting a `ca_key_…` or re-pairing each time.
--
-- Each dev machine stays its own account (one daemon per account, so the
-- relay's hub fan-out is untouched); `account_owners` is the many-to-many
-- bridge from a user to the accounts they can drive.

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    created_at    INTEGER NOT NULL
) STRICT;

CREATE TABLE account_owners (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, account_id)
) STRICT;
CREATE INDEX idx_account_owners_user ON account_owners(user_id);
CREATE INDEX idx_account_owners_account ON account_owners(account_id);
