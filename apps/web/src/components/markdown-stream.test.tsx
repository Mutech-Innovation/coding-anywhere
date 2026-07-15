// DOM-level pin of the streaming-markdown render path: a bubble streamed
// chunk by chunk must end up rendering the same text as a one-shot render.
// The pure segmentation invariants (settled-segment stability, fence safety,
// the ```c++ termination regression) live with the shared core in
// packages/markdown/src/segment.test.ts.
import { MantineProvider } from '@mantine/core';
import { act, render } from '@testing-library/react';
import { beforeAll, describe, expect, it } from 'vitest';

import { useChat } from '../store/chat.js';
import { MessageBubble } from './ChatPanel.js';

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
  '\n',
  // Fence whose info string defeats the old strict opener regex — used to
  // spin parseMarkdownLines forever (renderer OOM, "Render process gone").
  '```c++\n',
  'int main() { return 0; }\n',
  '```\n',
  '| a | b |\n',
  '|---|---|\n',
  '| 1 | 2 |\n',
  'closing words\n',
].join('');

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
          historyLoaded: true,
          historyLoading: false,
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
