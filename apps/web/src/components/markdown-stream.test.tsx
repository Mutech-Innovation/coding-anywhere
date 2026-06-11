// Pins the streaming-markdown segmentation that keeps long streamed answers
// from re-parsing/re-rendering the whole bubble per chunk (the guest-page OOM:
// per-chunk cost grew with answer length, O(answer²) per turn).
import { MantineProvider } from '@mantine/core';
import { act, render } from '@testing-library/react';
import { beforeAll, describe, expect, it } from 'vitest';

import { useChat } from '../store/chat.js';
import { MessageBubble, nextStreamingSegments, streamingMarkdownSegments } from './ChatPanel.js';

const CORPUS = [
  'Intro paragraph with **bold** and `code`.\n',
  '\n',
  '# Heading\n',
  'Another paragraph\nwith a continuation line.\n',
  '\n',
  '- list item one\n',
  '- list item two\n',
  '1. ordered item\n',
  '```go\n',
  'func main() {\n',
  '\n', // blank line INSIDE the fence — must not become a cut point
  '\tfmt.Println("hi")\n',
  '}\n',
  '```\n',
  '| a | b |\n',
  '|---|---|\n',
  '| 1 | 2 |\n',
  'closing words\n',
].join('');

describe('streamingMarkdownSegments', () => {
  it('segments concatenate back to the normalized text', () => {
    const segments = streamingMarkdownSegments(CORPUS);
    expect(segments.join('')).toBe(CORPUS);
  });

  it('never cuts inside a fenced code block', () => {
    // Build enough blocks to force intermediate cuts, then a fence with
    // blank lines inside.
    const blocks = Array.from({ length: 40 }, (_, i) => `para ${i}\n\n`).join('');
    const text = `${blocks}\`\`\`txt\nline\n\nline after blank\n`; // fence still open
    const segments = streamingMarkdownSegments(text);
    expect(segments.join('')).toBe(text);
    // The open fence must be entirely inside the live (last) segment.
    const live = segments[segments.length - 1]!;
    expect(live.startsWith('```txt')).toBe(true);
  });

  it('every prefix split keeps each settled segment parse-equivalent', () => {
    // Stream the corpus in awkward 7-char chunks; at every step the
    // segments must concatenate to the streamed text.
    let text = '';
    for (let i = 0; i < CORPUS.length; i += 7) {
      text = CORPUS.slice(0, i + 7);
      expect(streamingMarkdownSegments(text).join('')).toBe(text);
    }
  });
});

describe('nextStreamingSegments', () => {
  it('settled segments stay identical while chunks stream in', () => {
    // The whole point of the split: every segment except the live last one
    // must keep its exact string value from chunk to chunk, so the memo'd
    // MarkdownBody rendering it can bail out.
    let prev: { text: string; segments: string[] } = { text: '', segments: [] };
    for (let i = 0; i < CORPUS.length; i += 5) {
      const text = CORPUS.slice(0, i + 5);
      const next = nextStreamingSegments(prev.text, prev.segments, text);
      expect(next.segments.join('')).toBe(text);
      const settled = prev.segments.slice(0, -1);
      expect(next.segments.slice(0, settled.length)).toEqual(settled);
      prev = next;
    }
  });

  it('falls back to a full split when the text does not extend the cache', () => {
    const a = nextStreamingSegments('', [], CORPUS);
    const b = nextStreamingSegments(a.text, a.segments, 'totally different');
    expect(b.segments.join('')).toBe('totally different');
  });
});

// --- DOM-level: a streamed bubble renders the same text as a one-shot one ---

const baseSession = {
  id: 'sess-md',
  agent_id: 'cursor-cli',
  native_id: 'acp-sess-md',
  cwd: '/tmp',
  status: 'thinking' as const,
  started_at: new Date(0).toISOString(),
};

beforeAll(() => {
  window.matchMedia ??= ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as never;
});

describe('MessageBubble streaming render', () => {
  it('streamed chunks render the same text content as the final message', () => {
    let chatHandler: ((env: unknown) => void) | null = null;
    useChat.setState({ attached: false, sessions: {}, activeId: null });
    useChat.getState().attach({
      handle: (_: string, handler: (env: unknown) => void) => {
        chatHandler = handler;
      },
      send: () => undefined,
    } as never);
    useChat.setState({
      sessions: {
        'sess-md': {
          session: { ...baseSession },
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
        },
      },
    });

    function Bubble() {
      const message = useChat((s) => s.sessions['sess-md']?.messages[0]);
      return message ? <MessageBubble message={message} /> : null;
    }
    const streamed = render(
      <MantineProvider>
        <Bubble />
      </MantineProvider>,
    );
    for (let i = 0; i < CORPUS.length; i += 3) {
      act(() => {
        chatHandler?.({
          type: 'event',
          method: 'update',
          payload: {
            session_id: 'sess-md',
            update: {
              sessionUpdate: 'agent_message_chunk',
              content: { type: 'text', text: CORPUS.slice(i, i + 3) },
            },
          },
        });
      });
    }
    const streamingText = streamed.container.textContent;

    const message = useChat.getState().sessions['sess-md']!.messages[0]!;
    expect(message.text).toBe(CORPUS);
    const oneShot = render(
      <MantineProvider>
        <MessageBubble message={{ ...message, streaming: false }} />
      </MantineProvider>,
    );
    expect(streamingText).toBe(oneShot.container.textContent);
  });
});
