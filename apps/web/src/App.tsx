import {
  ActionIcon,
  AppShell,
  Badge,
  Box,
  Group,
  Menu,
  Stack,
  Tabs,
  Text,
  Title,
  Tooltip,
} from '@mantine/core';
import {
  IconCode,
  IconDotsVertical,
  IconFiles,
  IconGitCompare,
  IconHome,
  IconLogout,
  IconMessageCircle,
  IconServer,
  IconTerminal2,
} from '@tabler/icons-react';
import { useEffect, useState } from 'react';

import { AGENTS, listAgents } from '@coding-anywhere/agents';

import { ChatPanel } from './components/ChatPanel.js';
import { ConnectionStatus } from './components/ConnectionStatus.js';
import { DiffPanel } from './components/DiffPanel.js';
import { FilesPanel } from './components/FilesPanel.js';
import { PairScreen } from './components/PairScreen.js';
import { PortsPanel } from './components/PortsPanel.js';
import { TerminalsPanel } from './components/TerminalsPanel.js';
import { useConnection } from './store/connection.js';

export function App() {
  const credentials = useConnection((s) => s.credentials);
  const hydrate = useConnection((s) => s.hydrate);

  // On first mount, attempt to load saved credentials and reconnect.
  useEffect(() => {
    hydrate();
  }, [hydrate]);

  if (!credentials) {
    return <PairScreen />;
  }
  return <MainShell />;
}

function MainShell() {
  const credentials = useConnection((s) => s.credentials);
  const signOut = useConnection((s) => s.signOut);
  const [activeTab, setActiveTab] = useState<string | null>('chat');

  return (
    <AppShell padding="md" header={{ height: 56 }}>
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between">
          <Group gap="xs">
            <IconTerminal2 size={20} stroke={1.6} />
            <Title order={4} fw={600}>
              Coding Anywhere
            </Title>
            <Badge size="xs" variant="light">
              P6 wired
            </Badge>
          </Group>
          <Group gap="xs">
            <ConnectionStatus />
            <Menu position="bottom-end" withArrow>
              <Menu.Target>
                <ActionIcon variant="subtle" aria-label="Device menu">
                  <IconDotsVertical size={18} />
                </ActionIcon>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Label>
                  Account: {credentials?.bundle.account_id.slice(0, 12) ?? '—'}
                </Menu.Label>
                <Menu.Item leftSection={<IconLogout size={14} />} onClick={signOut}>
                  Sign out / unpair
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>
      <AppShell.Main>
        <Tabs value={activeTab} onChange={setActiveTab} keepMounted={false}>
          <Tabs.List>
            <Tabs.Tab value="chat" leftSection={<IconMessageCircle size={14} />}>
              Chat
            </Tabs.Tab>
            <Tabs.Tab value="terminals" leftSection={<IconTerminal2 size={14} />}>
              Terminals
            </Tabs.Tab>
            <Tabs.Tab value="files" leftSection={<IconFiles size={14} />}>
              Files
            </Tabs.Tab>
            <Tabs.Tab value="diff" leftSection={<IconGitCompare size={14} />}>
              Diff
            </Tabs.Tab>
            <Tabs.Tab value="ports" leftSection={<IconServer size={14} />}>
              Ports
            </Tabs.Tab>
            <Tabs.Tab value="agents" leftSection={<IconCode size={14} />}>
              Agents
            </Tabs.Tab>
            <Tabs.Tab value="welcome" leftSection={<IconHome size={14} />}>
              Welcome
            </Tabs.Tab>
          </Tabs.List>
          <Box pt="md">
            <Tabs.Panel value="chat">
              <ChatPanel />
            </Tabs.Panel>
            <Tabs.Panel value="terminals">
              <TerminalsPanel />
            </Tabs.Panel>
            <Tabs.Panel value="files">
              <FilesPanel />
            </Tabs.Panel>
            <Tabs.Panel value="diff">
              <DiffPanel />
            </Tabs.Panel>
            <Tabs.Panel value="ports">
              <PortsPanel />
            </Tabs.Panel>
            <Tabs.Panel value="agents">
              <AgentsTab />
            </Tabs.Panel>
            <Tabs.Panel value="welcome">
              <WelcomeTab />
            </Tabs.Panel>
          </Box>
        </Tabs>
      </AppShell.Main>
    </AppShell>
  );
}

function WelcomeTab() {
  return (
    <Stack gap="lg">
      <Box>
        <Title order={2}>Welcome</Title>
        <Text c="dimmed" mt="xs">
          You are paired. Open a Terminal in the Terminals tab to drive your dev machine remotely.
          Sessions, structured chat UIs, files, and ports come online in the next phases (P3–P6).
        </Text>
      </Box>
    </Stack>
  );
}

function AgentsTab() {
  const agents = listAgents();
  return (
    <Stack gap="xs">
      {agents.map((agent) => (
        <Group
          key={agent.id}
          justify="space-between"
          p="sm"
          style={{
            border: '1px solid var(--mantine-color-default-border)',
            borderRadius: 8,
          }}
        >
          <Group gap="sm">
            <IconCode size={18} color={agent.accent} />
            <Stack gap={0}>
              <Text fw={500}>{agent.name}</Text>
              <Text size="xs" c="dimmed">
                {agent.tagline}
              </Text>
            </Stack>
          </Group>
          <Tooltip
            label={
              agent.id === AGENTS.devin.id
                ? 'Highest-priority integration; first to ship in P3'
                : 'Adapter lands in v2'
            }
          >
            <Badge
              color={agent.id === AGENTS.devin.id ? 'brand' : 'gray'}
              variant={agent.available ? 'filled' : 'light'}
            >
              {agent.available ? 'Available' : 'Coming soon'}
            </Badge>
          </Tooltip>
        </Group>
      ))}
    </Stack>
  );
}
