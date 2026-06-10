-- Owner-chosen agent for guest "ask"-mode share links.
--
-- The chat channel is multi-agent (Devin for Terminal, cursor-agent), and the
-- guest must not be able to pick which agent it talks to. The share owner now
-- picks the agent at issuance; the relay injects it into the guest's
-- create_session alongside cwd/model so the guest can neither omit nor tamper
-- with it.
--
-- Empty string means "no owner-chosen agent" — the daemon then applies its
-- default (devin), matching legacy shares minted before this column existed.

ALTER TABLE guest_shares ADD COLUMN agent_id TEXT NOT NULL DEFAULT '';
