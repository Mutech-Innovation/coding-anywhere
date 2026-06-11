// Pins the mdast→Mantine renderer's fidelity and security posture: the
// constructs the old hand-rolled parser mangled (nested lists flattened,
// blockquotes shown literally) now render structurally, and untrusted chat
// content can never become markup, remote fetches, or script-scheme links
// (the guest share page renders attacker-adjacent input with this).
import { MantineProvider } from '@mantine/core';
import { render } from '@testing-library/react';
import { beforeAll, describe, expect, it } from 'vitest';

import { MarkdownBody } from './Markdown.js';

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

function renderMarkdown(text: string) {
  return render(
    <MantineProvider>
      <MarkdownBody text={text} />
    </MantineProvider>,
  );
}

describe('MarkdownBody', () => {
  it('renders nested lists as nested lists (old parser flattened them)', () => {
    const { container } = renderMarkdown('- parent\n  - child one\n  - child two\n- sibling\n');
    const nested = container.querySelectorAll('ul ul');
    expect(nested).toHaveLength(1);
    expect(nested[0]!.querySelectorAll('li')).toHaveLength(2);
  });

  it('renders blockquotes structurally (old parser showed the "> " literally)', () => {
    const { container } = renderMarkdown('> quoted words\n');
    expect(container.textContent).toContain('quoted words');
    expect(container.textContent).not.toContain('>');
  });

  it('renders GFM strikethrough and task-list checkboxes', () => {
    const { container } = renderMarkdown('~~gone~~\n\n- [x] done\n- [ ] todo\n');
    const struck = Array.from(container.querySelectorAll('span')).find(
      (el) => el.textContent === 'gone',
    );
    expect(struck?.style.textDecoration).toContain('line-through');
    // Task markers render as (svg) icons in the list markers.
    expect(container.querySelectorAll('li svg').length).toBe(2);
  });

  it('never injects raw HTML from chat content', () => {
    const { container } = renderMarkdown(
      'hi <img src=x onerror=alert(1)> there\n\n<script>boom()</script>\n',
    );
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('script')).toBeNull();
    expect(container.textContent).toContain('<script>boom()</script>');
  });

  it('refuses non-http(s)/mailto link schemes and image fetches', () => {
    const { container } = renderMarkdown(
      '[click](javascript:alert(1)) and ![pic](https://example.com/a.png)\n',
    );
    const anchors = Array.from(container.querySelectorAll('a'));
    expect(anchors.some((a) => a.getAttribute('href')?.startsWith('javascript:'))).toBe(false);
    expect(container.textContent).toContain('click');
    // The image renders as a link to the image, never an <img> fetch.
    expect(container.querySelector('img')).toBeNull();
    expect(anchors.some((a) => a.getAttribute('href') === 'https://example.com/a.png')).toBe(true);
  });

  it('resolves reference-style links against same-text definitions', () => {
    const { container } = renderMarkdown('See [docs][d].\n\n[d]: https://example.com/docs\n');
    const anchor = container.querySelector('a');
    expect(anchor?.getAttribute('href')).toBe('https://example.com/docs');
    // The definition line itself renders nothing.
    expect(container.textContent).not.toContain('example.com/docs');
  });
});
