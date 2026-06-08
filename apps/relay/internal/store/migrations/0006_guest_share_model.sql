-- Owner-chosen model for guest "ask"-mode share links.
--
-- Guests have no model picker, and the relay blocks them from calling
-- set_config_option. The share owner now optionally locks a model at issuance;
-- the relay injects it into the guest's create_session and the daemon applies
-- it via session/set_config_option right after the session is created.
--
-- Empty string means "no owner-chosen model" — the guest session uses the
-- agent's (Devin CLI's) default model.

ALTER TABLE guest_shares ADD COLUMN model TEXT NOT NULL DEFAULT '';
