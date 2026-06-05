import {
  Alert,
  Button,
  CopyButton,
  Group,
  Menu,
  Modal,
  NumberInput,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { IconShare3 } from '@tabler/icons-react';
import { useState } from 'react';

import { RelayError } from '@coding-anywhere/protocol';

import { useConnection } from '../store/connection.js';

/**
 * Menu item that opens the "share a read-only ask link" modal. Like
 * AccountKeyMenu, it only flips a store flag (the Menu.Dropdown unmounts on
 * click) — the modal itself lives in {@link ShareAskModal}.
 */
export function ShareAskMenu() {
  const setShareAskOpen = useConnection((s) => s.setShareAskOpen);
  return (
    <Menu.Item leftSection={<IconShare3 size={14} />} onClick={() => setShareAskOpen(true)}>
      分享只读（ask）链接
    </Menu.Item>
  );
}

/**
 * Modal that mints a guest "ask"-mode share link. Render once, as a sibling to
 * the header menu (see App.tsx), so it survives the dropdown closing. TTL,
 * prompt limit, and session count are all fixed at generation time.
 */
export function ShareAskModal() {
  const opened = useConnection((s) => s.shareAskOpen);
  const setOpen = useConnection((s) => s.setShareAskOpen);
  const createAskShare = useConnection((s) => s.createAskShare);

  const [ttlMinutes, setTtlMinutes] = useState<number | string>(30);
  const [promptLimit, setPromptLimit] = useState<number | string>(20);
  const [maxSessions, setMaxSessions] = useState<number | string>(1);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [link, setLink] = useState<string | null>(null);
  const [expiresAt, setExpiresAt] = useState<number | null>(null);

  function reset() {
    setLink(null);
    setError(null);
    setExpiresAt(null);
  }

  function close() {
    if (busy) return;
    setOpen(false);
    // Defer reset so the modal content doesn't flash while closing.
    setTimeout(reset, 200);
  }

  async function onGenerate() {
    setError(null);
    setBusy(true);
    try {
      const res = await createAskShare({
        ttlSeconds: Math.max(1, Number(ttlMinutes) || 0) * 60,
        promptLimit: Math.max(0, Number(promptLimit) || 0),
        maxSessions: Math.max(1, Number(maxSessions) || 1),
      });
      setLink(res.url);
      setExpiresAt(res.expiresAt);
    } catch (err) {
      setError(formatShareError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      opened={opened}
      onClose={close}
      centered
      size="lg"
      title={
        <Group gap={6}>
          <IconShare3 size={18} />
          <Text fw={700}>分享只读（ask）链接</Text>
        </Group>
      }
    >
      <Stack gap="md">
        <Alert color="yellow" variant="light">
          拿到链接的人可以在 <b>ask（只读）</b>模式下与你的 Devin 对话。Devin 不能改文件、不能跑命令
          （权限请求会自动拒绝），也看不到你其它的会话。链接到期或额度用尽后自动失效。
        </Alert>

        {link ? (
          <Stack gap="sm">
            <TextInput
              label="分享链接"
              value={link}
              readOnly
              onFocus={(e) => e.currentTarget.select()}
            />
            {expiresAt ? (
              <Text size="xs" c="dimmed">
                到期时间：{new Date(expiresAt).toLocaleString()}
              </Text>
            ) : null}
            <Group justify="flex-end">
              <CopyButton value={link} timeout={1500}>
                {({ copied, copy }) => (
                  <Tooltip label={copied ? '已复制' : '点击复制'} withArrow>
                    <Button variant="default" onClick={copy}>
                      {copied ? '已复制' : '复制链接'}
                    </Button>
                  </Tooltip>
                )}
              </CopyButton>
              <Button variant="subtle" onClick={reset}>
                再建一个
              </Button>
            </Group>
          </Stack>
        ) : (
          <Stack gap="sm">
            <NumberInput
              label="有效期（分钟）"
              min={1}
              value={ttlMinutes}
              onChange={setTtlMinutes}
            />
            <NumberInput
              label="提问次数上限（0 表示不限）"
              min={0}
              value={promptLimit}
              onChange={setPromptLimit}
            />
            <NumberInput label="会话数上限" min={1} value={maxSessions} onChange={setMaxSessions} />
            {error ? (
              <Alert color="red" variant="light">
                {error}
              </Alert>
            ) : null}
            <Group justify="flex-end">
              <Button variant="default" onClick={close} disabled={busy}>
                取消
              </Button>
              <Button onClick={() => void onGenerate()} loading={busy}>
                生成链接
              </Button>
            </Group>
          </Stack>
        )}
      </Stack>
    </Modal>
  );
}

function formatShareError(err: unknown): string {
  if (err instanceof RelayError) return `${err.code}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return '生成失败，请重试。';
}
