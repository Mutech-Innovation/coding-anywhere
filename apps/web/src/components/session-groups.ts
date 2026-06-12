// Pure helpers for the chat session list: split the store's session map into
// the owner's regular sessions and guest (share-link) session groups. Kept
// free of React/Mantine so it can be unit-tested and later shared with the
// mobile renderer.

import type { SessionState } from '../store/chat.js';

export interface GuestSessionGroup {
  /** Share link id; '' for legacy guest sessions created before share_id existed. */
  shareId: string;
  /** The link's sessions, newest first. */
  sessions: SessionState[];
}

/**
 * Splits the session map into the owner's regular sessions and guest
 * (share-link) session groups, one group per share link, everything newest
 * first. Guest sessions are owner-visible but view-only; grouping them under
 * their link keeps a busy link's chatter manageable.
 */
export function groupSessionList(sessions: Record<string, SessionState>): {
  regular: SessionState[];
  guestGroups: GuestSessionGroup[];
} {
  const newestFirst = (a: SessionState, b: SessionState) =>
    new Date(b.session.started_at).getTime() - new Date(a.session.started_at).getTime();
  const regular: SessionState[] = [];
  const byShare = new Map<string, SessionState[]>();
  for (const s of Object.values(sessions)) {
    if (s.session.origin === 'guest') {
      const key = s.session.share_id ?? '';
      const group = byShare.get(key);
      if (group) group.push(s);
      else byShare.set(key, [s]);
    } else {
      regular.push(s);
    }
  }
  regular.sort(newestFirst);
  const guestGroups = [...byShare.entries()].map(([shareId, list]) => {
    list.sort(newestFirst);
    return { shareId, sessions: list };
  });
  guestGroups.sort((a, b) => newestFirst(a.sessions[0]!, b.sessions[0]!));
  return { regular, guestGroups };
}
