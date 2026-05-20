import { Box } from '@mantine/core';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';
import { useEffect, useRef } from 'react';

import { useTerminals } from '../store/terminals.js';

interface Props {
  sessionId: string;
}

const ENCODER = new TextEncoder();

/**
 * Live xterm.js view bound to a daemon-side PTY session.
 *
 * Lifecycle:
 *   - On mount: instantiate Terminal, mount in DOM, fit to container.
 *   - Call terminal.attach to fetch the scrollback snapshot, write it.
 *   - Subscribe to terminal.data events for this session id, write into xterm.
 *   - xterm.onData (user keystrokes) → terminal.input via the store.
 *   - ResizeObserver → fit + terminal.resize via the store.
 *   - On unmount: dispose xterm, detach listeners.
 */
export function TerminalView({ sessionId }: Props) {
  const ref = useRef<HTMLDivElement | null>(null);
  const onData = useTerminals((s) => s.onData);
  const sendInput = useTerminals((s) => s.sendInput);
  const resize = useTerminals((s) => s.resize);
  const attachData = useTerminals((s) => s.attachData);
  const onExit = useTerminals((s) => s.onExit);

  useEffect(() => {
    const host = ref.current;
    if (!host) return;

    const term = new Terminal({
      convertEol: false,
      cursorBlink: true,
      fontFamily: 'ui-monospace, SFMono-Regular, "JetBrains Mono", Menlo, Consolas, monospace',
      fontSize: 13,
      lineHeight: 1.2,
      theme: {
        background: '#0b0f17',
        foreground: '#e7ecf3',
        cursor: '#e7ecf3',
        selectionBackground: '#306eff44',
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(host);
    fit.fit();

    let detachData: (() => void) | undefined;
    let detachExit: (() => void) | undefined;
    let disposed = false;
    let resizeObserver: ResizeObserver | undefined;
    let pendingResize: number | null = null;

    (async () => {
      // 1. Pull scrollback snapshot.
      try {
        const { snapshot } = await attachData(sessionId);
        if (snapshot.length > 0) term.write(snapshot);
      } catch (err) {
        term.writeln(`\r\n[attach failed: ${(err as Error).message}]`);
      }
      if (disposed) return;

      // 2. Live data stream.
      detachData = onData(sessionId, (chunk) => {
        if (!disposed) term.write(chunk);
      });
      detachExit = onExit(sessionId, (exit) => {
        if (disposed) return;
        const msg = exit.err
          ? `\r\n[session exited code=${exit.code} err=${exit.err}]`
          : `\r\n[session exited code=${exit.code}]`;
        term.writeln(msg);
      });

      // 3. Wire keystrokes back to the daemon.
      term.onData((data) => {
        sendInput(sessionId, ENCODER.encode(data)).catch(() => {
          // ignore; the user will see errors via the badge / disconnect flow.
        });
      });

      // 4. Push resize on viewport change.
      const sendResize = () => {
        const cols = term.cols;
        const rows = term.rows;
        if (cols > 0 && rows > 0) {
          resize(sessionId, cols, rows).catch(() => {
            // ignore
          });
        }
      };
      term.onResize(sendResize);

      resizeObserver = new ResizeObserver(() => {
        if (pendingResize !== null) cancelAnimationFrame(pendingResize);
        pendingResize = requestAnimationFrame(() => {
          pendingResize = null;
          if (disposed) return;
          try {
            fit.fit();
          } catch {
            // host is hidden / unmounting
          }
        });
      });
      resizeObserver.observe(host);
      // Initial size push.
      sendResize();
    })();

    return () => {
      disposed = true;
      detachData?.();
      detachExit?.();
      if (pendingResize !== null) cancelAnimationFrame(pendingResize);
      resizeObserver?.disconnect();
      try {
        term.dispose();
      } catch {
        // ignore
      }
    };
  }, [sessionId, onData, sendInput, resize, attachData, onExit]);

  return (
    <Box
      ref={ref}
      style={{
        flex: 1,
        minHeight: 0,
        background: '#0b0f17',
        padding: 8,
        borderRadius: 6,
      }}
    />
  );
}
