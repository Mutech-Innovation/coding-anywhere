import {
  Alert,
  Box,
  Button,
  CopyButton,
  Group,
  Menu,
  Modal,
  Stack,
  Text,
  Tooltip,
} from '@mantine/core';
import {
  IconAlertTriangle,
  IconKey,
  IconRefresh,
  IconTrash,
} from '@tabler/icons-react';
import { useEffect, useState } from 'react';

import { RelayError } from '@coding-anywhere/protocol';

import { useConnection } from '../store/connection.js';

/**
 * Account-key menu items + the two modals used by the user-menu in the app
 * header. Owns:
 *   - "Show account key" → opens reveal modal with the cached plaintext.
 *   - "Rotate account key" → mints a new key on the relay, opens reveal modal.
 *   - "Revoke account key" → confirmation modal, drops everything on success.
 *
 * The reveal modal is shared with first-pair onboarding via the store's
 * pendingKeyReveal field, so freshly minted keys always show exactly once.
 */
export function AccountKeyMenu() {
  const credentials = useConnection((s) => s.credentials);
  const pendingKeyReveal = useConnection((s) => s.pendingKeyReveal);
  const dismissKeyReveal = useConnection((s) => s.dismissKeyReveal);
  const rotateAccountKey = useConnection((s) => s.rotateAccountKey);
  const revokeAccountKey = useConnection((s) => s.revokeAccountKey);

  const [revealValue, setRevealValue] = useState<string | null>(null);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (pendingKeyReveal) {
      setRevealValue(pendingKeyReveal);
    }
  }, [pendingKeyReveal]);

  function closeReveal() {
    setRevealValue(null);
    dismissKeyReveal();
  }

  function handleError(err: unknown, fallback: string) {
    if (err instanceof RelayError) {
      setError(`${err.code}: ${err.message}`);
    } else if (err instanceof Error) {
      setError(err.message);
    } else {
      setError(fallback);
    }
  }

  async function onShow() {
    setError(null);
    if (credentials?.bundle.account_key) {
      setRevealValue(credentials.bundle.account_key);
      return;
    }
    setError(
      '本地凭据里没有账户密钥（可能在之前的版本配的对）。请用"重置账户密钥"重新生成一个。',
    );
  }

  async function onRotate() {
    setError(null);
    setBusy(true);
    try {
      await rotateAccountKey();
      // reveal is triggered via pendingKeyReveal effect above
    } catch (err) {
      handleError(err, '轮换失败。');
    } finally {
      setBusy(false);
    }
  }

  async function onRevoke() {
    setError(null);
    setBusy(true);
    try {
      await revokeAccountKey();
      setRevokeOpen(false);
    } catch (err) {
      handleError(err, '撤销失败。');
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <Menu.Item leftSection={<IconKey size={14} />} onClick={onShow}>
        显示账户密钥
      </Menu.Item>
      <Menu.Item leftSection={<IconRefresh size={14} />} onClick={onRotate} disabled={busy}>
        重置账户密钥
      </Menu.Item>
      <Menu.Item
        leftSection={<IconTrash size={14} />}
        color="red"
        onClick={() => setRevokeOpen(true)}
      >
        撤销账户密钥（所有设备下线）
      </Menu.Item>

      <Modal
        opened={revealValue !== null}
        onClose={closeReveal}
        title={
          <Group gap={6}>
            <IconKey size={18} />
            <Text fw={700}>账户密钥</Text>
          </Group>
        }
        size="lg"
        centered
      >
        <Stack gap="md">
          <Alert color="yellow" variant="light" icon={<IconAlertTriangle size={16} />}>
            这串密钥相当于账户的主密码。任何拥有它的人都能登录你的账户。**只显示一次时请立刻保存到密码管理器**；之后想再看可以随时从这里"显示账户密钥"再调出。
          </Alert>
          <Box
            p="md"
            style={{
              background: 'var(--mantine-color-gray-light)',
              borderRadius: 8,
              fontFamily:
                '"JetBrains Mono", "IBM Plex Mono", ui-monospace, monospace',
              fontSize: 13,
              wordBreak: 'break-all',
              userSelect: 'all',
            }}
          >
            {revealValue ?? ''}
          </Box>
          <Group justify="flex-end">
            <CopyButton value={revealValue ?? ''} timeout={1500}>
              {({ copied, copy }) => (
                <Tooltip label={copied ? '已复制' : '点击复制'} withArrow>
                  <Button variant="default" onClick={copy} disabled={!revealValue}>
                    {copied ? '已复制' : '复制密钥'}
                  </Button>
                </Tooltip>
              )}
            </CopyButton>
            <Button color="ink.9" onClick={closeReveal}>
              我已保存
            </Button>
          </Group>
          {error ? (
            <Alert color="red" variant="light">
              {error}
            </Alert>
          ) : null}
        </Stack>
      </Modal>

      <Modal
        opened={revokeOpen}
        onClose={() => setRevokeOpen(false)}
        title={
          <Group gap={6}>
            <IconAlertTriangle size={18} color="var(--mantine-color-red-7)" />
            <Text fw={700}>撤销账户密钥</Text>
          </Group>
        }
        size="md"
        centered
      >
        <Stack gap="md">
          <Text size="sm">
            这会立即作废账户密钥，并使<b>所有</b>设备的会话失效（包括 daemon）。
            之后只能在开发机上重新走配对流程才能进入账户。请确认这是你的本意。
          </Text>
          {error ? (
            <Alert color="red" variant="light">
              {error}
            </Alert>
          ) : null}
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setRevokeOpen(false)} disabled={busy}>
              取消
            </Button>
            <Button color="red" onClick={onRevoke} loading={busy}>
              确认撤销
            </Button>
          </Group>
        </Stack>
      </Modal>
    </>
  );
}
