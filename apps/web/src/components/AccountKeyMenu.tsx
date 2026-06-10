import { Alert, Box, Button, Group, Menu, Modal, Stack, Text } from '@mantine/core';
import { IconAlertTriangle, IconKey, IconRefresh, IconTrash } from '@tabler/icons-react';
import { useState } from 'react';

import { RelayError } from '@coding-anywhere/protocol';

import { useConnection } from '../store/connection.js';
import { CopyTextButton } from './CopyTextButton.js';

/**
 * Menu items for the device dropdown ("show / rotate / revoke account key").
 *
 * IMPORTANT: this component MUST be rendered inside a `<Menu.Dropdown>`.
 * Click handlers only mutate zustand store state — they do NOT own any
 * React-local UI state — because Mantine's Menu unmounts its Dropdown
 * subtree on item click (closeOnItemClick=true). Any component-local
 * `useState` declared here would be wiped before a downstream modal could
 * render, which is exactly the "click-and-flash" bug we hit before.
 *
 * The matching modals live in {@link AccountKeyModals}, which must be
 * mounted as a sibling to the Menu (i.e. outside Menu.Dropdown) so their
 * lifetime is independent of the dropdown.
 */
export function AccountKeyMenu() {
  const requestRevealKey = useConnection((s) => s.requestRevealKey);
  const setRevokeKeyOpen = useConnection((s) => s.setRevokeKeyOpen);
  const rotateAccountKey = useConnection((s) => s.rotateAccountKey);
  const setKeyActionError = useConnection((s) => s.setKeyActionError);

  async function onRotate() {
    setKeyActionError(null);
    try {
      await rotateAccountKey();
      // The new key flows through pendingKeyReveal → AccountKeyModals.
    } catch (err) {
      setKeyActionError(formatKeyError(err, '轮换失败。'));
    }
  }

  return (
    <>
      <Menu.Item leftSection={<IconKey size={14} />} onClick={() => requestRevealKey()}>
        显示账户密钥
      </Menu.Item>
      <Menu.Item leftSection={<IconRefresh size={14} />} onClick={() => void onRotate()}>
        重置账户密钥
      </Menu.Item>
      <Menu.Item
        leftSection={<IconTrash size={14} />}
        color="red"
        onClick={() => setRevokeKeyOpen(true)}
      >
        撤销账户密钥（所有设备下线）
      </Menu.Item>
    </>
  );
}

/**
 * Modals that pair with {@link AccountKeyMenu}. Render this ONCE,
 * outside the Menu (typically inside AppShell.Main or next to the
 * header), so the modal stays mounted after the dropdown closes.
 *
 * All state is in the zustand store; this component is effectively a
 * view layer over `pendingKeyReveal` / `revokeKeyOpen` / `keyActionError`.
 */
export function AccountKeyModals() {
  const pendingKeyReveal = useConnection((s) => s.pendingKeyReveal);
  const dismissKeyReveal = useConnection((s) => s.dismissKeyReveal);
  const revokeKeyOpen = useConnection((s) => s.revokeKeyOpen);
  const setRevokeKeyOpen = useConnection((s) => s.setRevokeKeyOpen);
  const revokeAccountKey = useConnection((s) => s.revokeAccountKey);
  const keyActionError = useConnection((s) => s.keyActionError);
  const setKeyActionError = useConnection((s) => s.setKeyActionError);

  const [busy, setBusy] = useState(false);

  async function onRevoke() {
    setKeyActionError(null);
    setBusy(true);
    try {
      await revokeAccountKey();
    } catch (err) {
      setKeyActionError(formatKeyError(err, '撤销失败。'));
    } finally {
      setBusy(false);
    }
  }

  function closeRevoke() {
    if (busy) return;
    setRevokeKeyOpen(false);
  }

  return (
    <>
      <Modal
        opened={pendingKeyReveal !== null}
        onClose={dismissKeyReveal}
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
            这串密钥相当于账户的主密码。任何拥有它的人都能登录你的账户。请把它保存到密码管理器；之后想再看可以随时从这里"显示账户密钥"再调出。
          </Alert>
          <Box
            p="md"
            style={{
              background: 'var(--mantine-color-gray-light)',
              borderRadius: 8,
              fontFamily: '"JetBrains Mono", "IBM Plex Mono", ui-monospace, monospace',
              fontSize: 13,
              wordBreak: 'break-all',
              userSelect: 'all',
            }}
          >
            {pendingKeyReveal ?? ''}
          </Box>
          {keyActionError ? (
            <Alert color="red" variant="light">
              {keyActionError}
            </Alert>
          ) : null}
          <Group justify="flex-end">
            <CopyTextButton
              value={pendingKeyReveal ?? ''}
              label="复制密钥"
              disabled={!pendingKeyReveal}
            />
            <Button color="ink.9" onClick={dismissKeyReveal}>
              我已保存
            </Button>
          </Group>
        </Stack>
      </Modal>

      <Modal
        opened={revokeKeyOpen}
        onClose={closeRevoke}
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
          {keyActionError ? (
            <Alert color="red" variant="light">
              {keyActionError}
            </Alert>
          ) : null}
          <Group justify="flex-end">
            <Button variant="default" onClick={closeRevoke} disabled={busy}>
              取消
            </Button>
            <Button color="red" onClick={() => void onRevoke()} loading={busy}>
              确认撤销
            </Button>
          </Group>
        </Stack>
      </Modal>

      {/*
        Inline banner for menu-time errors that occur while the reveal modal
        is closed (e.g. "本地凭据里没有账户密钥" from requestRevealKey, or
        a rotateAccountKey failure). Without this the error would be silent.
      */}
      {keyActionError && pendingKeyReveal === null && !revokeKeyOpen ? (
        <KeyActionErrorBanner message={keyActionError} onDismiss={() => setKeyActionError(null)} />
      ) : null}
    </>
  );
}

function KeyActionErrorBanner({ message, onDismiss }: { message: string; onDismiss: () => void }) {
  return (
    <Box
      style={{
        position: 'fixed',
        right: 16,
        bottom: 16,
        zIndex: 1000,
        maxWidth: 360,
      }}
    >
      <Alert
        color="red"
        variant="filled"
        withCloseButton
        onClose={onDismiss}
        icon={<IconAlertTriangle size={16} />}
      >
        {message}
      </Alert>
    </Box>
  );
}

function formatKeyError(err: unknown, fallback: string): string {
  if (err instanceof RelayError) return `${err.code}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return fallback;
}
