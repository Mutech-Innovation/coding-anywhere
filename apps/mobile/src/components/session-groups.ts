// Pure helpers for the chat session list: split the store's session map into
// the owner's regular sessions and guest (share-link) groups, bucket regular
// sessions by workspace (cwd), and bucket one workspace's sessions by day.
// Kept free of React/Native so it can be unit-tested and shared.

import type { SessionState } from '../store/chat';

export interface GuestSessionGroup {
  /** Share link id; '' for legacy guest sessions created before share_id existed. */
  shareId: string;
  /** The link's sessions, newest first. */
  sessions: SessionState[];
}

export interface WorkspaceGroup {
  /** Absolute project directory the sessions ran in. '' when unknown. */
  cwd: string;
  /** Human label — the last path segment of cwd (or a fallback). */
  label: string;
  /** This workspace's regular sessions, newest first. */
  sessions: SessionState[];
  /** started_at (ms) of the newest session — drives workspace ordering. */
  lastActivity: number;
}

export interface DaySection {
  /** Bucket label: 今天 / 昨天 / a localized date. */
  label: string;
  sessions: SessionState[];
}

function startedAt(s: SessionState): number {
  return new Date(s.session.started_at).getTime() || 0;
}

const newestFirst = (a: SessionState, b: SessionState) => startedAt(b) - startedAt(a);

/** Last path segment of an absolute cwd, e.g. /Users/me/app → "app". */
export function workspaceLabel(cwd: string): string {
  const trimmed = (cwd || '').replace(/[/\\]+$/, '');
  if (!trimmed) return '未指定目录';
  const seg = trimmed.split(/[/\\]/).pop();
  return seg && seg.length > 0 ? seg : trimmed;
}

/**
 * Splits the session map into the owner's regular sessions and guest
 * (share-link) groups, one group per share link, everything newest first.
 * (Mirrors apps/web/src/components/session-groups.ts.)
 */
export function groupSessionList(sessions: Record<string, SessionState>): {
  regular: SessionState[];
  guestGroups: GuestSessionGroup[];
} {
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

/**
 * Buckets the owner's regular sessions by workspace (cwd). Guest sessions are
 * excluded — they live under their share link, not a project. Workspaces are
 * ordered by most-recent activity; each group's sessions are newest first.
 */
export function groupByWorkspace(sessions: Record<string, SessionState>): WorkspaceGroup[] {
  const { regular } = groupSessionList(sessions);
  const byCwd = new Map<string, SessionState[]>();
  for (const s of regular) {
    const cwd = s.session.cwd ?? '';
    const list = byCwd.get(cwd);
    if (list) list.push(s);
    else byCwd.set(cwd, [s]);
  }
  const groups: WorkspaceGroup[] = [];
  for (const [cwd, list] of byCwd) {
    list.sort(newestFirst);
    groups.push({
      cwd,
      label: workspaceLabel(cwd),
      sessions: list,
      lastActivity: startedAt(list[0]!),
    });
  }
  groups.sort((a, b) => b.lastActivity - a.lastActivity);
  return groups;
}

function dayKey(d: Date): string {
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}

/** Localized day label for a session's start time: 今天 / 昨天 / yyyy/mm/dd. */
export function dayLabel(ms: number, now: number = Date.now()): string {
  const d = new Date(ms);
  const today = new Date(now);
  const yesterday = new Date(now);
  yesterday.setDate(today.getDate() - 1);
  if (dayKey(d) === dayKey(today)) return '今天';
  if (dayKey(d) === dayKey(yesterday)) return '昨天';
  return d.toLocaleDateString();
}

/**
 * Buckets a flat, newest-first session list into day sections (今天 / 昨天 /
 * date), preserving order. Used by the per-workspace sessions screen.
 */
export function groupSessionsByDay(
  sessions: SessionState[],
  now: number = Date.now(),
): DaySection[] {
  const ordered = [...sessions].sort(newestFirst);
  const sections: DaySection[] = [];
  let current: DaySection | null = null;
  let currentKey = '';
  for (const s of ordered) {
    const ms = startedAt(s);
    const key = dayKey(new Date(ms));
    if (!current || key !== currentKey) {
      current = { label: dayLabel(ms, now), sessions: [] };
      currentKey = key;
      sections.push(current);
    }
    current.sessions.push(s);
  }
  return sections;
}
