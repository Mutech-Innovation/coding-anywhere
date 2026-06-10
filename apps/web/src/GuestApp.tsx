import { AppShell, Badge, Box, Center, Group, Loader, Stack, Text } from '@mantine/core';
import { RelayWSClient } from '@coding-anywhere/protocol';
import { useEffect, useRef, useState } from 'react';

import { BrandLogo } from './components/BrandLogo.js';
import { GuestChat } from './components/GuestChat.js';
import { useChat } from './store/chat.js';
import { useConnection } from './store/connection.js';

type Phase = 'connecting' | 'ready' | 'error' | 'ended';

/** Application close code the relay uses when it rejects a guest connection
 * (mirrors `guestCloseStatus` in apps/relay/internal/server/ws.go). */
const GUEST_REJECTED_CLOSE_CODE = 4401;

/** Give up on the spinner after this many failed connection attempts. */
const MAX_CONNECT_ATTEMPTS = 5;

/** Map the relay's short close reasons onto guest-facing copy. */
function guestRejectMessage(reason: string): string {
  switch (reason) {
    case 'share expired':
      return '链接已过期，请让分享者重新生成。';
    case 'share revoked':
      return '链接已被分享者撤销。';
    case 'unknown share':
      return '链接无效（分享不存在，可能 relay 已重置）。';
    case 'token expired or invalid':
      return '链接已过期或无效，请让分享者重新生成。';
    default:
      return `连接被拒绝（${reason || 'unauthorized'}）。`;
  }
}

/** Decode the `exp` claim (unix seconds) of a JWT without verifying it. */
function tokenExpiryMs(token: string): number | null {
  const parts = token.split('.');
  if (parts.length !== 3) return null;
  try {
    const payload = JSON.parse(atob(parts[1]!.replace(/-/g, '+').replace(/_/g, '/'))) as {
      exp?: number;
    };
    return typeof payload.exp === 'number' ? payload.exp * 1000 : null;
  } catch {
    return null;
  }
}

/**
 * GuestApp is the standalone, read-only "ask"-mode share page served at
 * `/guest?token=...&relay=...`. It runs entirely on a guest token: no pairing,
 * no stored credentials, no token refresh. It reuses the chat store but mounts
 * a stripped chat-only UI (see GuestChat).
 */
export function GuestApp() {
  const params = new URLSearchParams(window.location.search);
  const token = params.get('token') ?? '';
  const relay = params.get('relay') ?? '';

  const [phase, setPhase] = useState<Phase>('connecting');
  const [error, setError] = useState<string | null>(null);
  const activeId = useChat((s) => s.activeId);
  const startedRef = useRef(false);

  useEffect(() => {
    if (!token || !relay) {
      setError('链接缺少 token 或 relay 参数，无法连接。');
      setPhase('error');
      return;
    }
    // Guest tokens can't be refreshed; if this one is already past its exp we
    // know the outcome without dialing — fail fast with a clear message.
    const exp = tokenExpiryMs(token);
    if (exp !== null && exp <= Date.now()) {
      setError(`链接已于 ${new Date(exp).toLocaleString()} 过期，请让分享者重新生成。`);
      setPhase('error');
      return;
    }
    const client = new RelayWSClient({ relayURL: relay, accessToken: token });
    useConnection.setState({ client, status: 'connecting' });
    useChat.getState().attach(client);

    // Before the first successful session, every disconnect is a failed
    // attempt: the relay refuses rejected guests post-upgrade with close code
    // 4401 + a reason ("share expired", ...), and network/old-relay failures
    // show up as generic closes. Give up after a few of those instead of
    // spinning "正在连接" forever.
    let failedAttempts = 0;
    const fail = (message: string) => {
      setError(message);
      setPhase('error');
      client.stop();
    };

    let creating = false;
    const off = client.onState((s) => {
      useConnection.setState({ status: s });
      if (s === 'connected' && !startedRef.current && !creating) {
        creating = true;
        useChat
          .getState()
          .createSession({})
          .then(() => {
            startedRef.current = true;
            setPhase('ready');
          })
          .catch((e: unknown) => {
            setError(e instanceof Error ? e.message : '创建会话失败');
            setPhase('error');
          })
          .finally(() => {
            creating = false;
          });
      }
      if (s === 'disconnected' && !startedRef.current) {
        const close = client.getLastClose();
        if (close?.code === GUEST_REJECTED_CLOSE_CODE) {
          fail(guestRejectMessage(close.reason));
          return;
        }
        failedAttempts += 1;
        if (failedAttempts >= MAX_CONNECT_ATTEMPTS) {
          fail(
            '无法连接到 relay。链接可能已过期或被撤销，也可能 relay 暂时不可用 —— 请让分享者重新生成链接。',
          );
        }
      }
      // A drop after the session was established means the link expired or the
      // network died. The guest session can't be resumed (ownership resets on
      // reconnect), so surface an end state instead of silently reconnecting.
      if (s === 'disconnected' && startedRef.current) {
        setPhase('ended');
      }
    });
    client.start();

    return () => {
      off();
      useChat.getState().detach();
      client.stop();
      useConnection.setState({ client: null, status: 'disconnected' });
    };
    // token/relay come from the URL and never change for a mounted page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <AppShell padding={0} header={{ height: 56 }}>
      <AppShell.Header className="cw-hairline-b" withBorder={false}>
        <Group h="100%" px="lg" justify="space-between">
          <Group gap={10}>
            <BrandLogo height={36} />
            <Stack gap={0}>
              <Text fw={700} size="sm" lh={1.1}>
                Coding Anywhere
              </Text>
              <Text size="10px" c="dimmed" lh={1} style={{ letterSpacing: 1 }}>
                只读分享 · ASK
              </Text>
            </Stack>
          </Group>
          <Badge variant="light" color="gray">
            只读（ask）模式
          </Badge>
        </Group>
      </AppShell.Header>
      <AppShell.Main
        style={{
          display: 'flex',
          flexDirection: 'column',
          height: '100dvh',
          minHeight: 0,
          overflow: 'hidden',
        }}
      >
        {phase === 'error' ? (
          <Center flex={1}>
            <Stack align="center" gap="xs" maw={420} px="lg">
              <Text fw={600}>无法打开分享</Text>
              <Text c="dimmed" size="sm" ta="center">
                {error}
              </Text>
            </Stack>
          </Center>
        ) : phase === 'connecting' || !activeId ? (
          <Center flex={1}>
            <Stack align="center" gap="xs">
              <Loader size="sm" />
              <Text c="dimmed" size="sm">
                正在连接…
              </Text>
            </Stack>
          </Center>
        ) : (
          <Box style={{ flex: 1, minHeight: 0 }}>
            {phase === 'ended' ? (
              <Box className="cw-hairline-b" p="xs">
                <Text c="dimmed" size="xs" ta="center">
                  连接已结束（链接可能已过期）。刷新页面以重试。
                </Text>
              </Box>
            ) : null}
            <GuestChat sessionId={activeId} disabled={phase === 'ended'} />
          </Box>
        )}
      </AppShell.Main>
    </AppShell>
  );
}
