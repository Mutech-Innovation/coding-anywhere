import {
  ActionIcon,
  AppShell,
  Badge,
  Box,
  Center,
  Group,
  Loader,
  Menu,
  Stack,
  Tabs,
  Text,
  Title,
  Tooltip,
  UnstyledButton,
  useMantineColorScheme,
} from '@mantine/core';
import {
  IconBrandGithub,
  IconCode,
  IconDotsVertical,
  IconFiles,
  IconGitCompare,
  IconHome,
  IconLogout,
  IconMessageCircle,
  IconMoon,
  IconServer,
  IconSun,
  IconTerminal2,
} from '@tabler/icons-react';
import { useEffect, useState } from 'react';

import { AGENTS, listAgents } from '@coding-anywhere/agents';

import { AccountKeyMenu, AccountKeyModals } from './components/AccountKeyMenu.js';
import { BrandLogo } from './components/BrandLogo.js';
import { ChatPanel } from './components/ChatPanel.js';
import { ConnectionStatus } from './components/ConnectionStatus.js';
import { DiffPanel } from './components/DiffPanel.js';
import { FilesPanel } from './components/FilesPanel.js';
import { NotificationsMenu } from './components/NotificationsMenu.js';
import { PairScreen } from './components/PairScreen.js';
import { PortsPanel } from './components/PortsPanel.js';
import { ShareAskMenu, ShareAskModal } from './components/ShareAskMenu.js';
import { TerminalsPanel } from './components/TerminalsPanel.js';
import { WelcomeTab } from './components/WelcomeTab.js';
import { isDebugEnabled, startFlightRecorder } from './diagnostics.js';
import { useConnection } from './store/connection.js';

export function App() {
  const credentials = useConnection((s) => s.credentials);
  const hydrate = useConnection((s) => s.hydrate);
  const [hydrating, setHydrating] = useState(true);

  // Opt-in flight recorder for the owner UI: append ?debug to the URL or
  // localStorage.setItem('coding-anywhere/debug', '1'). Guest page has it
  // always on (see GuestApp).
  useEffect(() => {
    if (!isDebugEnabled()) return;
    return startFlightRecorder({ page: 'app' });
  }, []);

  useEffect(() => {
    let cancelled = false;
    hydrate().finally(() => {
      if (!cancelled) setHydrating(false);
    });
    return () => {
      cancelled = true;
    };
  }, [hydrate]);

  if (!credentials && hydrating) {
    return (
      <Center h="100vh">
        <Stack gap="xs" align="center">
          <Loader size="sm" />
          <Text c="dimmed" size="sm">
            正在恢复会话…
          </Text>
        </Stack>
      </Center>
    );
  }

  if (!credentials) {
    return <PairScreen />;
  }
  return <MainShell />;
}

function ColorSchemeToggle() {
  const { colorScheme, setColorScheme } = useMantineColorScheme();
  const isDark = colorScheme === 'dark';
  return (
    <Tooltip label={isDark ? '切换到浅色' : '切换到深色'}>
      <ActionIcon
        variant="subtle"
        color="ink"
        aria-label="Toggle color scheme"
        onClick={() => setColorScheme(isDark ? 'light' : 'dark')}
      >
        {isDark ? <IconSun size={18} /> : <IconMoon size={18} />}
      </ActionIcon>
    </Tooltip>
  );
}

function MainShell() {
  const credentials = useConnection((s) => s.credentials);
  const signOut = useConnection((s) => s.signOut);
  const [activeTab, setActiveTab] = useState<string | null>('welcome');
  const panelStyle = { height: '100%', minHeight: 0 };

  return (
    <AppShell padding={0} header={{ height: 56 }}>
      <AppShell.Header className="cw-hairline-b" withBorder={false}>
        <Group h="100%" px="lg" justify="space-between">
          <Group gap="md">
            <Group gap={10}>
              <BrandLogo height={40} />
              <Stack gap={0}>
                <Text fw={700} size="sm" lh={1.1}>
                  Coding Anywhere
                </Text>
                <Text size="10px" c="dimmed" lh={1} style={{ letterSpacing: 1 }}>
                  REMOTE DEV CONSOLE
                </Text>
              </Stack>
            </Group>
          </Group>
          <Group gap="xs">
            <ConnectionStatus />
            <NotificationsMenu />
            <Tooltip label="GitHub">
              <ActionIcon
                variant="subtle"
                color="ink"
                aria-label="GitHub"
                component="a"
                href="https://github.com/Mutech-Innovation/coding-anywhere"
                target="_blank"
                rel="noreferrer"
              >
                <IconBrandGithub size={18} />
              </ActionIcon>
            </Tooltip>
            <ColorSchemeToggle />
            <Menu position="bottom-end" withArrow>
              <Menu.Target>
                <ActionIcon variant="subtle" color="ink" aria-label="Device menu">
                  <IconDotsVertical size={18} />
                </ActionIcon>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Label>账户：{credentials?.bundle.account_id.slice(0, 12) ?? '—'}</Menu.Label>
                <ShareAskMenu />
                <Menu.Divider />
                <AccountKeyMenu />
                <Menu.Divider />
                <Menu.Item leftSection={<IconLogout size={14} />} onClick={signOut}>
                  退出并取消配对
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>
      {/*
        Mounted as a sibling to <AppShell.Header> so the Menu.Dropdown
        unmounting doesn't take the modals with it. See AccountKeyMenu.tsx
        for the full rationale.
      */}
      <AccountKeyModals />
      <ShareAskModal />
      <AppShell.Main
        style={{
          display: 'flex',
          flexDirection: 'column',
          height: '100dvh',
          minHeight: 0,
          overflow: 'hidden',
        }}
      >
        <Tabs
          value={activeTab}
          onChange={setActiveTab}
          keepMounted={false}
          className="cw-tabs"
          style={{
            display: 'flex',
            flex: 1,
            flexDirection: 'column',
            minHeight: 0,
          }}
        >
          <Box px="lg">
            <Tabs.List>
              <Tabs.Tab value="welcome" leftSection={<IconHome size={14} />}>
                首页
              </Tabs.Tab>
              <Tabs.Tab value="chat" leftSection={<IconMessageCircle size={14} />}>
                对话
              </Tabs.Tab>
              <Tabs.Tab value="terminals" leftSection={<IconTerminal2 size={14} />}>
                终端
              </Tabs.Tab>
              <Tabs.Tab value="files" leftSection={<IconFiles size={14} />}>
                文件
              </Tabs.Tab>
              <Tabs.Tab value="diff" leftSection={<IconGitCompare size={14} />}>
                Diff
              </Tabs.Tab>
              <Tabs.Tab value="ports" leftSection={<IconServer size={14} />}>
                端口
              </Tabs.Tab>
              <Tabs.Tab value="agents" leftSection={<IconCode size={14} />}>
                Agents
              </Tabs.Tab>
            </Tabs.List>
          </Box>
          <Box style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
            <Tabs.Panel value="welcome" style={panelStyle}>
              <WelcomeTab onNavigate={(v) => setActiveTab(v)} />
            </Tabs.Panel>
            <Box px="lg" pt="md" style={{ height: '100%', minHeight: 0 }}>
              <Tabs.Panel value="chat" style={panelStyle}>
                <ChatPanel />
              </Tabs.Panel>
              <Tabs.Panel value="terminals" style={panelStyle}>
                <TerminalsPanel />
              </Tabs.Panel>
              <Tabs.Panel value="files" style={panelStyle}>
                <FilesPanel />
              </Tabs.Panel>
              <Tabs.Panel value="diff" style={panelStyle}>
                <DiffPanel />
              </Tabs.Panel>
              <Tabs.Panel value="ports" style={panelStyle}>
                <PortsPanel />
              </Tabs.Panel>
              <Tabs.Panel value="agents" style={panelStyle}>
                <AgentsTab />
              </Tabs.Panel>
            </Box>
          </Box>
        </Tabs>
      </AppShell.Main>
    </AppShell>
  );
}

function AgentsTab() {
  const agents = listAgents();
  return (
    <Stack gap="md" py="lg">
      <Box>
        <Text className="cw-eyebrow">AGENTS · 智能体</Text>
        <Title order={2} mt={6}>
          连接你的 AI 编码伙伴
        </Title>
        <Text c="dimmed" mt={4} size="sm" maw={620}>
          一套统一的 ACP 通道，让任何 agent 都能通过 daemon 出站连接你的开发机。
        </Text>
      </Box>
      <Stack gap="sm">
        {agents.map((agent) => (
          <Group
            key={agent.id}
            justify="space-between"
            p="md"
            className="cw-hairline"
            style={{ borderRadius: 8 }}
          >
            <Group gap="md" align="flex-start">
              <UnstyledButton
                style={{
                  width: 36,
                  height: 36,
                  borderRadius: 6,
                  background: `${agent.accent}14`,
                  color: agent.accent,
                  display: 'grid',
                  placeItems: 'center',
                }}
                aria-hidden
              >
                <IconCode size={18} />
              </UnstyledButton>
              <Stack gap={2}>
                <Text fw={600}>{agent.name}</Text>
                <Text size="xs" c="dimmed">
                  {agent.tagline}
                </Text>
              </Stack>
            </Group>
            <Tooltip
              label={
                agent.id === AGENTS.devin.id
                  ? '优先级最高的集成，P3 阶段首发'
                  : '适配器将在 v2 落地'
              }
            >
              <Badge
                color={agent.id === AGENTS.devin.id ? 'brand' : 'gray'}
                variant={agent.available ? 'filled' : 'light'}
              >
                {agent.available ? '已可用' : '即将上线'}
              </Badge>
            </Tooltip>
          </Group>
        ))}
      </Stack>
    </Stack>
  );
}
