// React Native renderer for chat markdown: walks the SAME shared mdast tree from
// @coding-anywhere/markdown that the web renderer (apps/web/src/components/Markdown.tsx)
// walks — only the component mapping differs (RN Text/View instead of Mantine).
// The parse + streaming-segmentation + OOM-prevention invariant live in the shared
// package; do not fork them here.
//
// Security posture (identical to web — the content is untrusted): NEVER emit raw
// HTML (html nodes render as literal text), NEVER fetch remote images (image nodes
// render as a tappable link to the URL), and only http(s)/mailto URLs become links.
import {
  nextStreamingSegments,
  parseMarkdown,
  type Definition,
  type List as MdList,
  type ListItem as MdListItem,
  type PhrasingContent,
  type Root,
  type RootContent,
  type Table as MdTable,
} from '@coding-anywhere/markdown';
import { colors } from '@coding-anywhere/tokens';
import { useColorScheme } from 'nativewind';
import { Fragment, memo, type ReactNode, useMemo, useRef } from 'react';
import { Linking, ScrollView, Text, View } from 'react-native';

type MarkdownSize = 'xs' | 'sm';

type Palette = {
  text: string;
  muted: string;
  link: string;
  border: string;
  codeBg: string;
};

// Threaded through the walk so every node renders with the bubble's size/colors and
// reference-style links resolve against the tree's definitions ([docs][d] … [d]: …).
type RenderCtx = {
  size: MarkdownSize;
  color: string;
  palette: Palette;
  defs: Map<string, Definition>;
};

const FONT: Record<MarkdownSize, number> = { xs: 12, sm: 13 };

function usePalette(override?: string): Palette {
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  return {
    text: override ?? t.text,
    muted: t.textMuted,
    link: t.accent,
    border: t.border,
    codeBg: scheme === 'dark' ? colors.dark.surfaceMuted : colors.ink[100],
  };
}

// memo matters twice over: transcript bubbles re-render on every streaming chunk of
// their own text, and StreamingMarkdownBody renders the stable prefix through this
// component with segment strings that only change value when a new block boundary
// settles — so the (potentially huge) prefix bails out of re-parsing/re-rendering.
export const MarkdownBody = memo(function MarkdownBody({
  text,
  size = 'sm',
  color,
}: {
  text: string;
  size?: MarkdownSize;
  color?: string;
}) {
  const palette = usePalette(color);
  const root = useMemo(() => parseMarkdown(text), [text]);
  const ctx = useMemo<RenderCtx>(
    () => ({ size, color: palette.text, palette, defs: collectDefinitions(root) }),
    [root, size, palette],
  );
  if (root.children.length === 0) return null;
  return (
    <View style={{ gap: 6 }}>
      {root.children.map((node, i) => (
        <BlockNode key={i} node={node} ctx={ctx} />
      ))}
    </View>
  );
});

// Markdown body for a still-streaming message. See the web file / the markdown
// package README for the O(live tail) invariant: split at block boundaries so every
// settled segment is value-stable and its memo'd MarkdownBody bails out; only the
// live last block re-parses per chunk.
export function StreamingMarkdownBody({
  text,
  size,
  color,
}: {
  text: string;
  size?: MarkdownSize;
  color?: string;
}) {
  const cache = useRef<{ text: string; segments: string[] }>({ text: '', segments: [] });
  const segments = useMemo(() => {
    const prev = cache.current;
    cache.current = nextStreamingSegments(prev.text, prev.segments, text);
    return cache.current.segments;
  }, [text]);
  if (segments.length <= 1)
    return <MarkdownBody text={segments[0] ?? ''} size={size} color={color} />;
  return (
    <View style={{ gap: 6 }}>
      {segments.map((segment, i) => (
        <MarkdownBody key={i} text={segment} size={size} color={color} />
      ))}
    </View>
  );
}

// First definition wins per CommonMark; definitions can sit inside blockquotes/lists.
function collectDefinitions(root: Root): Map<string, Definition> {
  const defs = new Map<string, Definition>();
  const walk = (nodes: RootContent[]) => {
    for (const node of nodes) {
      if (node.type === 'definition') {
        if (!defs.has(node.identifier)) defs.set(node.identifier, node);
      } else if ('children' in node) {
        walk(node.children as RootContent[]);
      }
    }
  };
  walk(root.children);
  return defs;
}

// Chat content is untrusted: only protocols that can't execute script get a link.
function safeHref(url: string): string | null {
  return /^(https?:|mailto:)/i.test(url) ? url : null;
}

function openHref(url: string) {
  void Linking.openURL(url).catch(() => {});
}

function BlockNode({ node, ctx }: { node: RootContent; ctx: RenderCtx }): ReactNode {
  const { size, color, palette } = ctx;
  switch (node.type) {
    case 'paragraph':
      return (
        <Text style={{ fontSize: FONT[size], color, lineHeight: FONT[size] * 1.5 }}>
          {renderInline(node.children, ctx)}
        </Text>
      );
    case 'heading': {
      const fs = node.depth <= 1 ? 18 : node.depth === 2 ? 16 : 14;
      return (
        <Text style={{ fontSize: fs, fontWeight: '700', color }}>
          {renderInline(node.children, ctx)}
        </Text>
      );
    }
    case 'code':
      return (
        <View
          style={{
            backgroundColor: palette.codeBg,
            borderRadius: 8,
            borderWidth: 1,
            borderColor: palette.border,
            padding: 10,
          }}
        >
          {node.lang ? (
            <Text style={{ fontSize: 11, color: palette.muted, marginBottom: 4 }}>{node.lang}</Text>
          ) : null}
          <Text style={{ fontFamily: 'Menlo', fontSize: 12, color }}>{node.value}</Text>
        </View>
      );
    case 'blockquote':
      return (
        <View
          style={{
            borderLeftWidth: 3,
            borderLeftColor: palette.border,
            paddingLeft: 10,
            gap: 6,
          }}
        >
          {node.children.map((child, i) => (
            <BlockNode key={i} node={child} ctx={ctx} />
          ))}
        </View>
      );
    case 'list':
      return <ListBlock node={node} ctx={ctx} />;
    case 'table':
      return <TableBlock node={node} ctx={ctx} />;
    case 'thematicBreak':
      return <View style={{ height: 1, backgroundColor: palette.border, marginVertical: 2 }} />;
    case 'html':
      // Literal text on purpose — never parse or inject markup from chat.
      return <Text style={{ fontSize: FONT[size], color }}>{node.value}</Text>;
    case 'definition':
      return null;
    case 'footnoteDefinition':
      return (
        <View style={{ gap: 4 }}>
          <Text style={{ fontSize: 11, color: palette.muted }}>
            [{node.label ?? node.identifier}]:
          </Text>
          {node.children.map((child, i) => (
            <BlockNode key={i} node={child} ctx={{ ...ctx, size: 'xs' }} />
          ))}
        </View>
      );
    default:
      if ('children' in node) {
        return (
          <Text style={{ fontSize: FONT[size], color }}>
            {renderInline(node.children as PhrasingContent[], ctx)}
          </Text>
        );
      }
      if ('value' in node) {
        return (
          <Text style={{ fontSize: FONT[size], color }}>{(node as { value: string }).value}</Text>
        );
      }
      return null;
  }
}

function ListBlock({ node, ctx, depth = 0 }: { node: MdList; ctx: RenderCtx; depth?: number }) {
  let n = node.start ?? 1;
  return (
    <View style={{ gap: 3, paddingLeft: depth > 0 ? 14 : 0 }}>
      {node.children.map((item, i) => {
        const marker = node.ordered ? `${n++}.` : '•';
        return <ListItemNode key={i} item={item} ctx={ctx} marker={marker} depth={depth} />;
      })}
    </View>
  );
}

function ListItemNode({
  item,
  ctx,
  marker,
  depth,
}: {
  item: MdListItem;
  ctx: RenderCtx;
  marker: string;
  depth: number;
}) {
  // GFM task-list items get a checkbox glyph as their marker.
  const glyph = typeof item.checked === 'boolean' ? (item.checked ? '☑' : '☐') : marker;
  return (
    <View style={{ flexDirection: 'row', gap: 6 }}>
      <Text style={{ fontSize: FONT[ctx.size], color: ctx.palette.muted, minWidth: 16 }}>
        {glyph}
      </Text>
      <View style={{ flex: 1, gap: 4 }}>
        {item.children.map((child, i) => {
          if (i === 0 && child.type === 'paragraph') {
            return (
              <Text
                key={i}
                style={{
                  fontSize: FONT[ctx.size],
                  color: ctx.color,
                  lineHeight: FONT[ctx.size] * 1.5,
                }}
              >
                {renderInline(child.children, ctx)}
              </Text>
            );
          }
          if (child.type === 'list') {
            return <ListBlock key={i} node={child} ctx={ctx} depth={depth + 1} />;
          }
          return <BlockNode key={i} node={child} ctx={ctx} />;
        })}
      </View>
    </View>
  );
}

function TableBlock({ node, ctx }: { node: MdTable; ctx: RenderCtx }) {
  const [header, ...rows] = node.children;
  if (!header) return null;
  const align = node.align ?? [];
  const cols = header.children.length;
  const colWidth = 130;
  const cell = (content: ReactNode, j: number, head: boolean) => (
    <View
      key={j}
      style={{
        width: colWidth,
        padding: 6,
        borderWidth: 1,
        borderColor: ctx.palette.border,
      }}
    >
      <Text
        style={{
          fontSize: FONT[ctx.size],
          color: ctx.color,
          fontWeight: head ? '700' : '400',
          textAlign: align[j] ?? 'left',
        }}
      >
        {content}
      </Text>
    </View>
  );
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false}>
      <View>
        <View style={{ flexDirection: 'row' }}>
          {header.children.map((c, j) => cell(renderInline(c.children, ctx), j, true))}
        </View>
        {rows.map((row, r) => (
          <View key={r} style={{ flexDirection: 'row' }}>
            {Array.from({ length: cols }).map((_, j) => {
              const c = row.children[j];
              return cell(c ? renderInline(c.children, ctx) : '', j, false);
            })}
          </View>
        ))}
      </View>
    </ScrollView>
  );
}

function renderInline(children: PhrasingContent[], ctx: RenderCtx): ReactNode[] {
  return children.map((node, i) => renderInlineNode(node, ctx, i));
}

function renderInlineNode(node: PhrasingContent, ctx: RenderCtx, key: number): ReactNode {
  switch (node.type) {
    case 'text':
      return node.value;
    case 'strong':
      return (
        <Text key={key} style={{ fontWeight: '700' }}>
          {renderInline(node.children, ctx)}
        </Text>
      );
    case 'emphasis':
      return (
        <Text key={key} style={{ fontStyle: 'italic' }}>
          {renderInline(node.children, ctx)}
        </Text>
      );
    case 'delete':
      return (
        <Text key={key} style={{ textDecorationLine: 'line-through' }}>
          {renderInline(node.children, ctx)}
        </Text>
      );
    case 'inlineCode':
      return (
        <Text key={key} style={{ fontFamily: 'Menlo', fontSize: FONT[ctx.size] - 1 }}>
          {node.value}
        </Text>
      );
    case 'break':
      return '\n';
    case 'link': {
      const href = safeHref(node.url);
      if (!href) return <Fragment key={key}>{renderInline(node.children, ctx)}</Fragment>;
      return (
        <Text key={key} style={{ color: ctx.palette.link }} onPress={() => openHref(href)}>
          {renderInline(node.children, ctx)}
        </Text>
      );
    }
    case 'image': {
      // No <Image>: chat content must not trigger remote fetches. Render the alt
      // text as a tappable link to the image URL instead.
      const href = safeHref(node.url);
      const label = node.alt || node.url;
      if (!href) return label;
      return (
        <Text key={key} style={{ color: ctx.palette.link }} onPress={() => openHref(href)}>
          {label}
        </Text>
      );
    }
    case 'linkReference': {
      const def = ctx.defs.get(node.identifier);
      const href = def ? safeHref(def.url) : null;
      if (!href) return <Fragment key={key}>[{renderInline(node.children, ctx)}]</Fragment>;
      return (
        <Text key={key} style={{ color: ctx.palette.link }} onPress={() => openHref(href)}>
          {renderInline(node.children, ctx)}
        </Text>
      );
    }
    case 'imageReference': {
      const def = ctx.defs.get(node.identifier);
      const href = def ? safeHref(def.url) : null;
      const label = node.alt || node.identifier;
      if (!href) return label;
      return (
        <Text key={key} style={{ color: ctx.palette.link }} onPress={() => openHref(href)}>
          {label}
        </Text>
      );
    }
    case 'footnoteReference':
      return (
        <Text key={key} style={{ fontSize: 11, color: ctx.palette.muted }}>
          [{node.label ?? node.identifier}]
        </Text>
      );
    case 'html':
      // Literal text on purpose.
      return node.value;
    default:
      return null;
  }
}
