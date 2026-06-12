// Pins attachment rendering inside transcript bubbles: image attachments
// must show as data-URL thumbnails (click → lightbox), non-image files as
// name+size chips, and an attachment-only prompt must not render an empty
// markdown body. Attachments used to be store-only state the bubble ignored.
import { MantineProvider } from '@mantine/core';
import { fireEvent, render } from '@testing-library/react';
import { beforeAll, describe, expect, it } from 'vitest';

import type { ChatMessage } from '../store/chat.js';
import { MessageBubble } from './ChatPanel.js';

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

function renderBubble(message: ChatMessage) {
  return render(
    <MantineProvider env="test">
      <MessageBubble message={message} />
    </MantineProvider>,
  );
}

function userMessage(overrides: Partial<ChatMessage>): ChatMessage {
  return { id: 'm1', role: 'user', text: '', createdAt: 0, order: 1, ...overrides };
}

describe('MessageBubble attachments', () => {
  it('renders image attachments as thumbnails and files as name+size chips', () => {
    const { container } = renderBubble(
      userMessage({
        text: '看看这个',
        attachments: [
          { type: 'image', name: 'shot.png', data: 'aGVsbG8=', mime_type: 'image/png' },
          { type: 'file', name: 'notes.txt', data: 'aGVsbG8=', mime_type: 'text/plain' },
        ],
      }),
    );
    const img = container.querySelector('img');
    expect(img?.getAttribute('src')).toBe('data:image/png;base64,aGVsbG8=');
    expect(img?.getAttribute('alt')).toBe('shot.png');
    expect(container.textContent).toContain('notes.txt');
    expect(container.textContent).toContain('6 B'); // decoded size of aGVsbG8=
    expect(container.textContent).toContain('看看这个');
  });

  it('opens a lightbox with the full image when the thumbnail is clicked', () => {
    const { container, baseElement } = renderBubble(
      userMessage({
        attachments: [
          { type: 'image', name: 'shot.png', data: 'aGVsbG8=', mime_type: 'image/png' },
        ],
      }),
    );
    fireEvent.click(container.querySelector('button')!);
    // Modal portals to the document body; thumbnail + lightbox copies.
    expect(baseElement.querySelectorAll('img[src="data:image/png;base64,aGVsbG8="]')).toHaveLength(
      2,
    );
  });

  it('renders an attachment-only prompt without an empty markdown body', () => {
    const { container } = renderBubble(
      userMessage({
        attachments: [
          { type: 'image', name: 'shot.png', data: 'aGVsbG8=', mime_type: 'image/png' },
        ],
      }),
    );
    expect(container.querySelector('img')).not.toBeNull();
    // No markdown paragraph should render for the empty text.
    expect(container.querySelector('p')).toBeNull();
  });
});
