-- Working directory for guest "ask"-mode share links.
--
-- Guests have no directory picker, but the daemon's chat agent (Devin) refuses
-- to operate without an explicit, project-scoped cwd. The share owner now
-- chooses that directory at issuance; the relay injects it into the guest's
-- create_session so the guest can neither omit nor tamper with it.
--
-- Empty string means "no owner-chosen cwd" (pre-migration shares); the relay
-- leaves the create_session payload untouched in that case.

ALTER TABLE guest_shares ADD COLUMN cwd TEXT NOT NULL DEFAULT '';
