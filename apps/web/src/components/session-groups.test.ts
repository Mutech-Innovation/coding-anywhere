import { describe, expect, it } from 'vitest';

import type { SessionState } from '../store/chat.js';
import { groupSessionList } from './session-groups.js';

function state(args: {
  id: string;
  startedAt: string;
  origin?: string;
  shareId?: string;
}): SessionState {
  return {
    session: {
      id: args.id,
      agent_id: 'devin',
      native_id: `n-${args.id}`,
      cwd: '/tmp/project',
      status: 'idle',
      started_at: args.startedAt,
      ...(args.origin ? { origin: args.origin } : {}),
      ...(args.shareId ? { share_id: args.shareId } : {}),
    },
    messages: [],
    toolCalls: {},
    transcriptOrder: 0,
    plan: null,
    modes: [],
    currentModeId: null,
    availableCommands: [],
    usage: null,
    configOptions: [],
    permissions: {},
    liveAgentMessageId: null,
  };
}

function byId(sessions: SessionState[]): string[] {
  return sessions.map((s) => s.session.id);
}

describe('groupSessionList', () => {
  it('keeps regular sessions out of groups, newest first', () => {
    const { regular, guestGroups } = groupSessionList({
      a: state({ id: 'a', startedAt: '2026-06-12T10:00:00Z' }),
      b: state({ id: 'b', startedAt: '2026-06-12T12:00:00Z' }),
    });
    expect(byId(regular)).toEqual(['b', 'a']);
    expect(guestGroups).toEqual([]);
  });

  it('groups guest sessions per share link, newest first inside and across groups', () => {
    const { regular, guestGroups } = groupSessionList({
      own: state({ id: 'own', startedAt: '2026-06-12T09:00:00Z' }),
      g1a: state({
        id: 'g1a',
        startedAt: '2026-06-12T10:00:00Z',
        origin: 'guest',
        shareId: 'share-1',
      }),
      g1b: state({
        id: 'g1b',
        startedAt: '2026-06-12T12:00:00Z',
        origin: 'guest',
        shareId: 'share-1',
      }),
      g2a: state({
        id: 'g2a',
        startedAt: '2026-06-12T13:00:00Z',
        origin: 'guest',
        shareId: 'share-2',
      }),
    });
    expect(byId(regular)).toEqual(['own']);
    expect(guestGroups.map((g) => g.shareId)).toEqual(['share-2', 'share-1']);
    expect(byId(guestGroups[1]!.sessions)).toEqual(['g1b', 'g1a']);
  });

  it("collects legacy guest sessions without share_id under the '' group", () => {
    const { guestGroups } = groupSessionList({
      old: state({ id: 'old', startedAt: '2026-06-12T10:00:00Z', origin: 'guest' }),
    });
    expect(guestGroups).toHaveLength(1);
    expect(guestGroups[0]!.shareId).toBe('');
    expect(byId(guestGroups[0]!.sessions)).toEqual(['old']);
  });

  it('handles an empty map', () => {
    expect(groupSessionList({})).toEqual({ regular: [], guestGroups: [] });
  });
});
