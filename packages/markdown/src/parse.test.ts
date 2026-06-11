// Pins what the remark/mdast core buys over the old hand-rolled line parser:
// the constructs LLMs emit constantly that the old parser flattened or
// rendered literally (nested lists, blockquotes, GFM extras), plus the fence
// info-string handling whose strict-regex version once looped it into an OOM.
import type { Blockquote, Code, List, Paragraph, Table } from 'mdast';
import { describe, expect, it } from 'vitest';

import { parseMarkdown } from './parse.js';

describe('parseMarkdown', () => {
  it('returns mdast with positions and normalizes CRLF', () => {
    const root = parseMarkdown('# h\r\n\r\npara\r\n');
    expect(root.children.map((n) => n.type)).toEqual(['heading', 'paragraph']);
    expect(root.children[0]!.position!.start.offset).toBe(0);
    // Offsets refer to the normalized ('\n') text — the segmentation slices
    // the normalized string with them.
    expect(root.children[1]!.position!.start.offset).toBe('# h\n\n'.length);
  });

  it('keeps nested lists nested (the old flat parser lost indentation)', () => {
    const root = parseMarkdown('- parent\n  - child one\n  - child two\n- sibling\n');
    const list = root.children[0] as List;
    expect(list.children).toHaveLength(2);
    const nested = list.children[0]!.children.find((n) => n.type === 'list') as List;
    expect(nested.children).toHaveLength(2);
  });

  it('parses blockquotes (previously shown as literal "> " paragraphs)', () => {
    const root = parseMarkdown('> quoted\n> and more\n');
    const quote = root.children[0] as Blockquote;
    expect(quote.type).toBe('blockquote');
    expect(quote.children[0]!.type).toBe('paragraph');
  });

  it('parses GFM strikethrough, task lists and table alignment', () => {
    const root = parseMarkdown(
      '~~gone~~\n\n- [x] done\n- [ ] todo\n\n| a | b |\n|:--|--:|\n| 1 | 2 |\n',
    );
    const para = root.children[0] as Paragraph;
    expect(para.children[0]!.type).toBe('delete');
    const tasks = root.children[1] as List;
    expect(tasks.children.map((item) => item.checked)).toEqual([true, false]);
    const table = root.children[2] as Table;
    expect(table.align).toEqual(['left', 'right']);
  });

  it('accepts arbitrary fence info strings per CommonMark', () => {
    // ```c++ / ``` js / ```ts title=… all defeated the old strict opener
    // regex; the unconsumed line spun the parser forever (renderer OOM).
    const root = parseMarkdown(
      '```c++\nint main() {}\n```\n\n```ts title=app.ts\nexport {};\n```\n',
    );
    const [a, b] = root.children as [Code, Code];
    expect([a.lang, b.lang]).toEqual(['c++', 'ts']);
    expect(b.meta).toBe('title=app.ts');
  });

  it('resolves nothing implicitly: reference definitions stay separate nodes', () => {
    // The renderer resolves linkReference → definition itself; pin the shape.
    const root = parseMarkdown('See [docs][d].\n\n[d]: https://example.com\n');
    expect(root.children.map((n) => n.type)).toEqual(['paragraph', 'definition']);
  });
});
