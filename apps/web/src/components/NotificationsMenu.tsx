import { ActionIcon, Badge, Group, Menu, Stack, Text, Tooltip } from '@mantine/core';
import { IconBell, IconBellMinus, IconBellOff, IconBellPlus } from '@tabler/icons-react';
import { useEffect } from 'react';

import { useNotifications } from '../store/notifications.js';

const STATE_LABEL: Record<string, string> = {
  unsupported: 'Push not supported by this browser',
  idle: 'Notifications off',
  subscribing: 'Subscribing…',
  subscribed: 'Notifications on',
  error: 'Notifications error',
};

const STATE_COLOR: Record<string, string> = {
  unsupported: 'gray',
  idle: 'gray',
  subscribing: 'yellow',
  subscribed: 'green',
  error: 'red',
};

export function NotificationsMenu() {
  const init = useNotifications((s) => s.init);
  const state = useNotifications((s) => s.state);
  const error = useNotifications((s) => s.error);
  const subscribe = useNotifications((s) => s.subscribe);
  const unsubscribe = useNotifications((s) => s.unsubscribe);
  const sendTest = useNotifications((s) => s.sendTest);

  useEffect(() => {
    init();
  }, [init]);

  const Icon =
    state === 'subscribed'
      ? IconBell
      : state === 'unsupported'
        ? IconBellOff
        : state === 'subscribing'
          ? IconBellMinus
          : IconBellPlus;

  return (
    <Menu position="bottom-end" withArrow>
      <Menu.Target>
        <Tooltip label={STATE_LABEL[state] ?? state}>
          <ActionIcon variant="subtle" aria-label="Notifications">
            <Icon size={18} />
          </ActionIcon>
        </Tooltip>
      </Menu.Target>
      <Menu.Dropdown miw={240}>
        <Stack gap={4} px="sm" py="xs">
          <Group gap="xs">
            <Badge color={STATE_COLOR[state] ?? 'gray'} variant="light" size="xs">
              {state}
            </Badge>
            <Text size="xs" c="dimmed">
              {STATE_LABEL[state] ?? state}
            </Text>
          </Group>
          {error ? (
            <Text size="xs" c="red">
              {error}
            </Text>
          ) : null}
        </Stack>
        <Menu.Divider />
        {state === 'subscribed' ? (
          <>
            <Menu.Item leftSection={<IconBellMinus size={14} />} onClick={() => sendTest()}>
              Send test notification
            </Menu.Item>
            <Menu.Item leftSection={<IconBellOff size={14} />} onClick={() => unsubscribe()}>
              Disable notifications
            </Menu.Item>
          </>
        ) : state === 'unsupported' ? (
          <Menu.Item disabled>Browser does not support Web Push.</Menu.Item>
        ) : (
          <Menu.Item leftSection={<IconBellPlus size={14} />} onClick={() => subscribe()}>
            Enable notifications
          </Menu.Item>
        )}
      </Menu.Dropdown>
    </Menu>
  );
}
