-- Account-level long-lived key.
--
-- This is what lets a fresh browser (or any client that lost its localStorage)
-- log back into an existing account without having to walk through the
-- pairing-code flow again. The plaintext key is only ever shown once at
-- issuance time (or when explicitly rotated); SQLite only stores the
-- SHA-256 hex digest so a database leak does not let an attacker log in.
--
-- A NULL hash means "no key is currently issued for this account" — in that
-- state /v1/auth/login refuses to authenticate anyone for the account, and a
-- new key must be minted via rotate (or the account has been "revoked").

ALTER TABLE accounts ADD COLUMN account_key_hash TEXT;

CREATE UNIQUE INDEX idx_accounts_account_key_hash
    ON accounts(account_key_hash)
    WHERE account_key_hash IS NOT NULL;
