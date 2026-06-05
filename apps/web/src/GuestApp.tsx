import { AppShell, Badge, Box, Center, Group, Loader, Stack, Text } from '@mantine/core';
import { RelayWSClient } from '@coding-anywhere/protocol';
import { useEffect, useRef, useState } from 'react';

import { BrandLogo } from './components/BrandLogo.js';
import { GuestChat } from './components/GuestChat.js';
import { useChat } from './store/chat.js';
import { useConnection } from './store/connection.js';

type Phase = 'connecting' | 'ready' | 'error' | 'ended';

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
    const client = new RelayWSClient({ relayURL: relay, accessToken: token });
    useConnection.setState({ client, status: 'connecting' });
    useChat.getState().attach(client);

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
