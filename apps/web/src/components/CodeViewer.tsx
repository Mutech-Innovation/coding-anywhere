import { javascript } from '@codemirror/lang-javascript';
import { json } from '@codemirror/lang-json';
import { markdown } from '@codemirror/lang-markdown';
import { EditorState, type Extension } from '@codemirror/state';
import { EditorView, lineNumbers } from '@codemirror/view';
import { Box } from '@mantine/core';
import { useEffect, useRef } from 'react';

interface Props {
  text: string;
  filename: string;
}

/** Pick an extension list based on file extension. */
function languageFor(filename: string): Extension[] {
  const ext = filename.split('.').pop()?.toLowerCase() ?? '';
  switch (ext) {
    case 'ts':
    case 'tsx':
      return [javascript({ jsx: true, typescript: true })];
    case 'js':
    case 'jsx':
    case 'mjs':
    case 'cjs':
      return [javascript({ jsx: ext === 'jsx' })];
    case 'json':
    case 'jsonc':
      return [json()];
    case 'md':
    case 'markdown':
    case 'mdx':
      return [markdown()];
    default:
      return [];
  }
}

const READ_ONLY_THEME = EditorView.theme(
  {
    '&': { fontSize: '13px', height: '100%' },
    '.cm-scroller': {
      fontFamily: 'ui-monospace, SFMono-Regular, "JetBrains Mono", Menlo, Consolas, monospace',
      lineHeight: '1.45',
    },
    '.cm-content': { padding: '8px 0' },
    '&.cm-focused .cm-cursor': { display: 'none' },
    '&.cm-editor': { backgroundColor: '#0b0f17' },
    '.cm-gutters': {
      backgroundColor: '#0b0f17',
      color: '#3d4a60',
      borderRight: '1px solid #19202c',
    },
    '.cm-activeLine': { backgroundColor: 'transparent' },
    '.cm-activeLineGutter': { backgroundColor: 'transparent' },
  },
  { dark: true },
);

/**
 * Read-only CodeMirror 6 viewer. Renders inside a flex parent that supplies
 * its own height; we fill that.
 */
export function CodeViewer({ text, filename }: Props) {
  const ref = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);

  // Build the editor on mount; replace its document on text/filename change.
  useEffect(() => {
    const host = ref.current;
    if (!host) return;
    const state = EditorState.create({
      doc: text,
      extensions: [
        EditorView.editable.of(false),
        EditorState.readOnly.of(true),
        lineNumbers(),
        READ_ONLY_THEME,
        ...languageFor(filename),
      ],
    });
    viewRef.current = new EditorView({ state, parent: host });
    return () => {
      viewRef.current?.destroy();
      viewRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filename]);

  // Update doc text without rebuilding the editor (keeps scroll, language).
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    const cur = view.state.doc.toString();
    if (cur === text) return;
    view.dispatch({
      changes: { from: 0, to: cur.length, insert: text },
    });
  }, [text]);

  return <Box ref={ref} style={{ flex: 1, minHeight: 0, overflow: 'auto' }} />;
}
