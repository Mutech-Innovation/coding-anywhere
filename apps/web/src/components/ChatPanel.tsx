import {
  ActionIcon,
  Alert,
  Anchor,
  Badge,
  Box,
  Button,
  Card,
  Code,
  Group,
  List,
  Menu,
  Modal,
  Paper,
  Progress,
  ScrollArea,
  SegmentedControl,
  Stack,
  Text,
  Textarea,
  TextInput,
  ThemeIcon,
  Tooltip,
} from '@mantine/core';
import type {
  AvailableCommand,
  ChatConfigOption,
  FilesEntry,
  PlanEntry,
  ChatUsage,
  ToolCallContentBlock,
} from '@coding-anywhere/protocol';
import {
  IconArrowRight,
  IconBolt,
  IconBrain,
  IconChevronDown,
  IconChevronRight,
  IconCheck,
  IconCircle,
  IconCircleDashed,
  IconCirclePlus,
  IconCommand,
  IconEdit,
  IconFileSearch,
  IconFileText,
  IconFolder,
  IconMessageCircle,
  IconPlayerStop,
  IconRoute,
  IconSearch,
  IconSend,
  IconShieldCheck,
  IconTerminal2,
  IconTool,
  IconTrash,
  IconWorld,
  IconX,
} from '@tabler/icons-react';
import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react';

import type { ChatMessage, ChatToolCall } from '../store/chat.js';
import { useChat } from '../store/chat.js';
import { useConnection } from '../store/connection.js';
import { useFiles } from '../store/files.js';

const STATUS_COLOR: Record<string, string> = {
  idle: 'gray',
  thinking: 'yellow',
  awaiting_permission: 'red',
  closed: 'dark',
};

export function ChatPanel() {
  const status = useConnection((s) => s.status);
  const client = useConnection((s) => s.client);
  const attach = useChat((s) => s.attach);
  const sessions = useChat((s) => s.sessions);
  const activeId = useChat((s) => s.activeId);
  const setActive = useChat((s) => s.setActive);
  const refresh = useChat((s) => s.refreshSessions);
  const loadAgents = useChat((s) => s.loadAgents);
  const closeSession = useChat((s) => s.closeSession);

  const [creatorOpen, setCreatorOpen] = useState(false);

  useEffect(() => {
    if (!client) return;
    attach(client);
  }, [client, attach]);

  useEffect(() => {
    if (status !== 'connected') return;
    void refresh().catch(() => {});
    void loadAgents().catch(() => {});
  }, [status, refresh, loadAgents]);

  const list = useMemo(
    () =>
      Object.values(sessions).sort(
        (a, b) =>
          new Date(b.session.started_at).getTime() - new Date(a.session.started_at).getTime(),
      ),
    [sessions],
  );

  return (
    <Group align="stretch" gap="md" h="100%" wrap="nowrap" style={{ minHeight: 0 }}>
      <Stack
        gap="xs"
        miw={260}
        maw={320}
        p="sm"
        style={{
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 8,
          flexShrink: 0,
        }}
      >
        <Group justify="space-between">
          <Text fw={600}>Sessions</Text>
          <Tooltip label="Start a new session with Devin">
            <ActionIcon
              variant="filled"
              color="brand"
              onClick={() => setCreatorOpen(true)}
              disabled={status !== 'connected'}
            >
              <IconCirclePlus size={16} />
            </ActionIcon>
          </Tooltip>
        </Group>
        {status !== 'connected' ? (
          <Text size="sm" c="dimmed">
            Waiting for relay connection…
          </Text>
        ) : null}
        <ScrollArea.Autosize mah="60vh">
          <Stack gap={4}>
            {list.length === 0 ? (
              <Text size="sm" c="dimmed">
                No sessions yet. Click + to start one.
              </Text>
            ) : (
              list.map((s) => (
                <Group
                  key={s.session.id}
                  gap="xs"
                  px="xs"
                  py={6}
                  wrap="nowrap"
                  onClick={() => setActive(s.session.id)}
                  style={{
                    cursor: 'pointer',
                    borderRadius: 6,
                    background:
                      activeId === s.session.id ? 'var(--mantine-color-brand-9)' : 'transparent',
                  }}
                >
                  <IconMessageCircle size={14} />
                  <Stack gap={0} style={{ flex: 1, minWidth: 0 }}>
                    <Text size="xs" fw={500} truncate>
                      {s.session.title || s.session.cwd}
                    </Text>
                    <Text size="xs" c="dimmed" truncate>
                      {s.session.agent_id} · {s.session.cwd}
                    </Text>
                  </Stack>
                  <Badge size="xs" color={STATUS_COLOR[s.session.status] ?? 'gray'} variant="light">
                    {s.session.status}
                  </Badge>
                  <Tooltip label="Close session">
                    <ActionIcon
                      size="sm"
                      variant="subtle"
                      color="gray"
                      onClick={(e) => {
                        e.stopPropagation();
                        void closeSession(s.session.id);
                      }}
                    >
                      <IconX size={14} />
                    </ActionIcon>
                  </Tooltip>
                </Group>
              ))
            )}
          </Stack>
        </ScrollArea.Autosize>
      </Stack>

      <Box
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          minWidth: 0,
          minHeight: 0,
          padding: 8,
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 8,
        }}
      >
        {activeId ? (
          <ChatView sessionId={activeId} />
        ) : (
          <Stack gap="xs" align="center" justify="center" style={{ flex: 1 }}>
            <IconMessageCircle size={32} stroke={1.4} color="var(--mantine-color-dimmed)" />
            <Text c="dimmed">Pick a session or start a new one to chat with Devin.</Text>
          </Stack>
        )}
      </Box>

      <CreateSessionModal opened={creatorOpen} onClose={() => setCreatorOpen(false)} />
    </Group>
  );
}

function CreateSessionModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const create = useChat((s) => s.createSession);
  const agents = useChat((s) => s.agents);
  const [cwd, setCwd] = useState('');
  const [title, setTitle] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const devin = agents.find((a) => a.id === 'devin');
  const devinAvailable = devin?.available ?? true;

  async function onSubmit() {
    setError(null);
    setBusy(true);
    try {
      await create({
        cwd: cwd.trim() || undefined,
        agentId: 'devin',
        title: title.trim() || undefined,
      });
      onClose();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title="Start a Devin session" centered size="lg">
      <Stack gap="md">
        <TextInput
          label="Working directory"
          description="Optional. Choose from the tree or leave blank to use the daemon's default workspace."
          placeholder="Select a directory or use daemon default"
          value={cwd}
          onChange={(e) => setCwd(e.currentTarget.value)}
        />
        <DirectoryPicker opened={opened} selectedPath={cwd} onSelect={setCwd} />
        <TextInput
          label="Title"
          placeholder="(optional)"
          value={title}
          onChange={(e) => setTitle(e.currentTarget.value)}
        />
        {!devinAvailable ? (
          <Alert color="yellow" variant="light" radius="md">
            Devin reports as unavailable on this daemon. The CLI may not be installed or
            authenticated.
          </Alert>
        ) : null}
        {error ? (
          <Alert color="red" variant="light" radius="md">
            {error}
          </Alert>
        ) : null}
        <Group justify="flex-end">
          <Button variant="subtle" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={onSubmit} loading={busy}>
            Start
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}

function DirectoryPicker({
  opened,
  selectedPath,
  onSelect,
}: {
  opened: boolean;
  selectedPath: string;
  onSelect: (path: string) => void;
}) {
  const status = useConnection((s) => s.status);
  const workspaces = useFiles((s) => s.workspaces);
  const workspaceId = useFiles((s) => s.workspaceId);
  const loadWorkspaces = useFiles((s) => s.loadWorkspaces);
  const [openDirs, setOpenDirs] = useState<Set<string>>(() => new Set(['']));
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!opened || status !== 'connected') return;
    setError(null);
    loadWorkspaces().catch((e: Error) => setError(e.message));
  }, [opened, status, loadWorkspaces]);

  const workspace = workspaces.find((w) => w.id === workspaceId) ?? workspaces[0];

  if (status !== 'connected') {
    return (
      <Text size="sm" c="dimmed">
        Directory picker is available after the relay connects.
      </Text>
    );
  }

  if (error) {
    return (
      <Alert color="red" variant="light" radius="md">
        {error}
      </Alert>
    );
  }

  if (!workspace) {
    return (
      <Text size="sm" c="dimmed">
        Loading workspace…
      </Text>
    );
  }

  return (
    <Paper withBorder radius="md" p="sm">
      <Group justify="space-between" align="flex-start" gap="sm">
        <Stack gap={0} style={{ minWidth: 0 }}>
          <Text size="sm" fw={500}>
            Choose a directory
          </Text>
          <Tooltip label={workspace.root}>
            <Text size="xs" c="dimmed" truncate>
              {workspace.root}
            </Text>
          </Tooltip>
        </Stack>
        <Button size="xs" variant="subtle" onClick={() => onSelect('')}>
          Use default
        </Button>
      </Group>
      <ScrollArea.Autosize mah={260} mt="xs" type="auto">
        <DirectoryNode
          path=""
          label={workspace.name || workspace.root}
          workspaceRoot={workspace.root}
          depth={0}
          openDirs={openDirs}
          setOpenDirs={setOpenDirs}
          selectedPath={selectedPath}
          onSelect={onSelect}
        />
      </ScrollArea.Autosize>
    </Paper>
  );
}

function DirectoryNode({
  path,
  label,
  workspaceRoot,
  depth,
  openDirs,
  setOpenDirs,
  selectedPath,
  onSelect,
}: {
  path: string;
  label: string;
  workspaceRoot: string;
  depth: number;
  openDirs: Set<string>;
  setOpenDirs: (s: Set<string>) => void;
  selectedPath: string;
  onSelect: (path: string) => void;
}) {
  const list = useFiles((s) => s.list);
  const workspaceId = useFiles((s) => s.workspaceId);
  const cacheKey = `${workspaceId ?? ''}|${path}`;
  const cached = useFiles((s) => s.listings[cacheKey]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const isOpen = openDirs.has(path);
  const absolutePath = joinWorkspacePath(workspaceRoot, path);
  const active = selectedPath === absolutePath;

  useEffect(() => {
    if (!isOpen || cached) return;
    setLoading(true);
    setError(null);
    list(path)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false));
  }, [isOpen, cached, list, path]);

  function toggle() {
    const next = new Set(openDirs);
    if (next.has(path)) next.delete(path);
    else next.add(path);
    setOpenDirs(next);
  }

  const directories = (cached ?? []).filter(
    (entry): entry is FilesEntry => entry.kind === 'directory',
  );

  return (
    <Stack gap={0}>
      <Group
        gap={4}
        py={3}
        px="xs"
        wrap="nowrap"
        onClick={() => onSelect(absolutePath)}
        style={{
          cursor: 'pointer',
          paddingLeft: depth * 12 + 4,
          background: active ? 'var(--mantine-color-brand-9)' : 'transparent',
          borderRadius: 4,
        }}
      >
        <ActionIcon
          size="xs"
          variant="subtle"
          aria-label={isOpen ? 'Collapse directory' : 'Expand directory'}
          onClick={(e) => {
            e.stopPropagation();
            toggle();
          }}
        >
          {isOpen ? <IconChevronDown size={12} /> : <IconChevronRight size={12} />}
        </ActionIcon>
        <IconFolder size={14} />
        <Text size="xs" fw={depth === 0 ? 600 : 500} truncate>
          {label}
        </Text>
      </Group>
      {isOpen ? (
        <Stack gap={0}>
          {error ? (
            <Text size="xs" c="red" px="md">
              {error}
            </Text>
          ) : null}
          {loading && !cached ? (
            <Text size="xs" c="dimmed" px="md">
              Loading…
            </Text>
          ) : null}
          {cached && directories.length === 0 ? (
            <Text size="xs" c="dimmed" px="md">
              No subdirectories
            </Text>
          ) : null}
          {directories.map((entry) => (
            <DirectoryNode
              key={entry.path}
              path={entry.path}
              label={entry.name}
              workspaceRoot={workspaceRoot}
              depth={depth + 1}
              openDirs={openDirs}
              setOpenDirs={setOpenDirs}
              selectedPath={selectedPath}
              onSelect={onSelect}
            />
          ))}
        </Stack>
      ) : null}
    </Stack>
  );
}

function joinWorkspacePath(root: string, path: string): string {
  if (!path) return root;
  const base = root.replace(/\/+$/, '');
  return `${base}/${path}`;
}

function ChatView({ sessionId }: { sessionId: string }) {
  const session = useChat((s) => s.sessions[sessionId]);
  const sendPrompt = useChat((s) => s.sendPrompt);
  const cancel = useChat((s) => s.cancel);
  const setMode = useChat((s) => s.setMode);
  const resolvePermission = useChat((s) => s.resolvePermission);
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // Auto-scroll on new content.
  useEffect(() => {
    if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [
    session?.messages.length,
    session?.session.status,
    session?.transcriptOrder,
    session?.plan?.length,
  ]);

  const transcriptItems = useMemo(
    () =>
      [
        ...(session?.messages ?? []).map((message) => ({
          type: 'message' as const,
          id: message.id,
          order: message.order,
          message,
        })),
        ...Object.values(session?.toolCalls ?? {}).map((call) => ({
          type: 'tool' as const,
          id: call.id,
          order: call.order,
          call,
        })),
      ].sort((a, b) => a.order - b.order),
    [session?.messages, session?.toolCalls],
  );

  if (!session) return null;

  const status = session.session.status;
  const pendingPermissions = Object.values(session.permissions).filter((p) => p.pending);
  const modelLabel = getConfigOptionDisplay(session.configOptions, 'model');

  async function onSend() {
    if (!draft.trim()) return;
    setError(null);
    setBusy(true);
    try {
      const text = draft;
      setDraft('');
      await sendPrompt(sessionId, text);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Stack style={{ flex: 1, minHeight: 0 }} gap="xs">
      <Group justify="space-between" px="xs">
        <Stack gap={0}>
          <Text fw={600} size="sm" truncate>
            {session.session.title || session.session.cwd}
          </Text>
          <Text size="xs" c="dimmed" truncate>
            {session.session.agent_id} · {session.session.cwd}
          </Text>
        </Stack>
        <Group gap="xs">
          {session.modes.length > 0 ? (
            <SegmentedControl
              size="xs"
              data={session.modes.map((m) => ({ label: m.name, value: m.id }))}
              value={session.currentModeId ?? session.modes[0]?.id}
              onChange={(value) => void setMode(sessionId, value)}
              transitionDuration={0}
            />
          ) : null}
          {modelLabel ? (
            <Tooltip label="Current model">
              <Badge color="violet" variant="light" maw={220} style={{ textTransform: 'none' }}>
                Model · {modelLabel}
              </Badge>
            </Tooltip>
          ) : null}
          {session.usage ? <UsagePill usage={session.usage} /> : null}
          <Badge color={STATUS_COLOR[status] ?? 'gray'} variant="light">
            {status}
          </Badge>
          {status === 'thinking' || status === 'awaiting_permission' ? (
            <Tooltip label="Cancel">
              <ActionIcon variant="subtle" color="red" onClick={() => void cancel(sessionId)}>
                <IconPlayerStop size={14} />
              </ActionIcon>
            </Tooltip>
          ) : null}
        </Group>
      </Group>

      {session.plan && session.plan.length > 0 ? <PlanCard entries={session.plan} /> : null}

      <ScrollArea
        viewportRef={scrollRef}
        style={{ flex: 1, minHeight: 0 }}
        type="auto"
        offsetScrollbars
      >
        <Stack gap="sm" px="xs">
          {pendingPermissions.map((p) => (
            <PermissionCard
              key={p.requestId}
              params={p.params}
              onAllow={() => void resolvePermission(sessionId, p.requestId, { decision: 'allow' })}
              onDeny={() => void resolvePermission(sessionId, p.requestId, { decision: 'deny' })}
            />
          ))}
          {transcriptItems.map((item) =>
            item.type === 'message' ? (
              <MessageBubble key={`message-${item.id}`} message={item.message} />
            ) : (
              <ToolCallCard key={`tool-${item.id}`} call={item.call} />
            ),
          )}
        </Stack>
      </ScrollArea>

      {error ? (
        <Alert color="red" variant="light" radius="md">
          {error}
        </Alert>
      ) : null}

      <CommandInput
        commands={session.availableCommands}
        value={draft}
        onChange={setDraft}
        onSubmit={onSend}
        busy={busy}
      />
    </Stack>
  );
}

function PlanCard({ entries }: { entries: PlanEntry[] }) {
  const total = entries.length || 1;
  const done = entries.filter((e) => e.status === 'completed').length;
  const inProgress = entries.find((e) => e.status === 'in_progress');
  return (
    <Paper p="xs" radius="md" withBorder>
      <Group justify="space-between" mb={6}>
        <Group gap="xs">
          <ThemeIcon size="sm" variant="light" color="brand">
            <IconRoute size={14} />
          </ThemeIcon>
          <Text fw={600} size="sm">
            Plan
          </Text>
          <Text size="xs" c="dimmed">
            {done}/{entries.length} done
          </Text>
        </Group>
        {inProgress ? (
          <Text size="xs" c="dimmed" lineClamp={1}>
            {inProgress.content}
          </Text>
        ) : null}
      </Group>
      <Progress value={(done / total) * 100} size="xs" radius="xs" mb={6} color="brand" />
      <List size="xs" spacing={4} icon={<IconCircle size={10} />} center>
        {entries.map((e, i) => (
          <List.Item
            key={i}
            icon={planIcon(e.status)}
            style={{
              opacity: e.status === 'completed' ? 0.6 : 1,
              textDecoration: e.status === 'completed' ? 'line-through' : undefined,
              fontWeight: e.status === 'in_progress' ? 600 : 400,
            }}
          >
            {e.content}
            {e.priority === 'high' ? (
              <Badge size="xs" color="red" variant="light" ml={6}>
                high
              </Badge>
            ) : null}
          </List.Item>
        ))}
      </List>
    </Paper>
  );
}

function planIcon(status: PlanEntry['status']) {
  switch (status) {
    case 'completed':
      return (
        <ThemeIcon size="xs" color="green" variant="light">
          <IconCheck size={10} />
        </ThemeIcon>
      );
    case 'in_progress':
      return (
        <ThemeIcon size="xs" color="yellow" variant="light">
          <IconCircleDashed size={10} />
        </ThemeIcon>
      );
    default:
      return (
        <ThemeIcon size="xs" color="gray" variant="light">
          <IconCircle size={10} />
        </ThemeIcon>
      );
  }
}

function UsagePill({ usage }: { usage: ChatUsage }) {
  const used = Number.isFinite(usage.used) ? Math.max(0, usage.used) : 0;
  const size = Number.isFinite(usage.size) ? Math.max(0, usage.size) : 0;
  const remaining = Math.max(0, size - used);
  const percent = size > 0 ? Math.min(100, Math.max(0, (used / size) * 100)) : 0;
  const cost = usage.cost ? formatCost(usage.cost) : null;
  const tooltip = [
    `Context: ${formatTokens(used)} / ${formatTokens(size)} tokens`,
    `${formatTokens(remaining)} remaining`,
    cost ? `Cost: ${cost}` : null,
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <Tooltip label={tooltip}>
      <Paper withBorder radius="xl" px="xs" py={3} miw={128}>
        <Group gap={6} wrap="nowrap" justify="space-between">
          <Text size="xs" c="dimmed">
            Context
          </Text>
          <Text size="xs" fw={600}>
            {Math.round(percent)}%
          </Text>
        </Group>
        <Progress value={percent} size={4} mt={3} radius="xl" color={usageColor(percent)} />
      </Paper>
    </Tooltip>
  );
}

function getConfigOptionDisplay(options: ChatConfigOption[], id: string): string | null {
  const option = options.find((o) => o.id.toLowerCase() === id);
  if (!option) return null;
  const value = option.currentValue;
  const choice = option.options?.find((o) => o.value === value);
  return stringValue(choice?.label) ?? stringValue(choice?.name) ?? stringValue(value);
}

function stringValue(value: unknown): string | null {
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return null;
}

function usageColor(percent: number): string {
  if (percent >= 90) return 'red';
  if (percent >= 75) return 'yellow';
  return 'green';
}

function formatTokens(n: number): string {
  if (!Number.isFinite(n)) return '—';
  if (n >= 1_000_000) return `${trimFixed(n / 1_000_000, n >= 10_000_000 ? 0 : 1)}M`;
  if (n >= 1_000) return `${trimFixed(n / 1_000, n >= 100_000 ? 0 : 1)}k`;
  return `${Math.round(n)}`;
}

function trimFixed(n: number, digits: number): string {
  return n.toFixed(digits).replace(/\.0$/, '');
}

function formatCost(cost: NonNullable<ChatUsage['cost']>): string {
  const digits = Math.abs(cost.amount) < 1 ? 4 : 2;
  return `${cost.currency} ${cost.amount.toFixed(digits)}`;
}

function CommandInput({
  commands,
  value,
  onChange,
  onSubmit,
  busy,
}: {
  commands: AvailableCommand[];
  value: string;
  onChange: (v: string) => void;
  onSubmit: () => void | Promise<void>;
  busy: boolean;
}) {
  // Show the typeahead only when the user has typed `/` as the very first
  // non-whitespace character and the cursor sits in the command word.
  const trimmed = value.trimStart();
  const slashOpen = trimmed.startsWith('/') && !trimmed.includes(' ');
  const filter = slashOpen ? trimmed.slice(1).toLowerCase() : '';
  const matches = useMemo(
    () =>
      slashOpen ? commands.filter((c) => c.name.toLowerCase().startsWith(filter)).slice(0, 8) : [],
    [commands, slashOpen, filter],
  );
  const ref = useRef<HTMLTextAreaElement | null>(null);
  return (
    <Stack gap={4} px="xs">
      {matches.length > 0 ? (
        <Paper withBorder radius="md" p="xs">
          <Stack gap={2}>
            {matches.map((c) => (
              <Group
                key={c.name}
                gap="xs"
                py={4}
                px={6}
                style={{ cursor: 'pointer', borderRadius: 4 }}
                onClick={() => {
                  onChange(`/${c.name}${c.input ? ' ' : ''}`);
                  ref.current?.focus();
                }}
              >
                <ThemeIcon size="xs" variant="light">
                  <IconCommand size={10} />
                </ThemeIcon>
                <Text size="xs" fw={600}>
                  /{c.name}
                </Text>
                <Text size="xs" c="dimmed" truncate>
                  {c.description}
                  {c.input?.hint ? (
                    <Text component="span" c="dimmed">{` — ${c.input.hint}`}</Text>
                  ) : null}
                </Text>
              </Group>
            ))}
          </Stack>
        </Paper>
      ) : null}
      <Group gap="xs" align="flex-end">
        <Textarea
          ref={ref}
          placeholder="Ask Devin... (try /plan, /clear, /fork)"
          autosize
          minRows={1}
          maxRows={6}
          value={value}
          style={{ flex: 1 }}
          onChange={(e) => onChange(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              void onSubmit();
            }
          }}
        />
        <Button leftSection={<IconSend size={14} />} onClick={() => void onSubmit()} loading={busy}>
          Send
        </Button>
      </Group>
    </Stack>
  );
}

function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === 'user';
  const isThought = message.role === 'agent_thought';
  const isSystem = message.role === 'system';
  const text = message.text || (message.streaming ? '…' : '');

  if (isThought) return <ThoughtBubble message={message} />;

  return (
    <Group justify={isUser ? 'flex-end' : 'flex-start'} align="flex-start">
      <Card
        radius="md"
        padding="sm"
        bg={isUser ? 'brand.7' : isSystem ? 'dark.6' : 'dark.7'}
        c={isUser ? 'white' : undefined}
        miw={isUser ? 100 : 200}
        maw="80%"
      >
        {isSystem ? (
          <Text size="xs" c="dimmed">
            {text}
          </Text>
        ) : (
          <MarkdownBody text={text} c={isUser ? 'white' : undefined} />
        )}
      </Card>
    </Group>
  );
}

function ThoughtBubble({ message }: { message: ChatMessage }) {
  const [expanded, setExpanded] = useState(false);
  const text = message.text || (message.streaming ? '…' : '');
  return (
    <Group justify="flex-start" align="flex-start">
      <Card
        radius="md"
        padding="sm"
        bg="dark.7"
        miw={220}
        maw="80%"
        withBorder
        style={{ opacity: 0.72 }}
      >
        <Group gap="xs" justify="space-between" wrap="nowrap">
          <Group gap="xs" wrap="nowrap">
            <ThemeIcon size="sm" variant="light" color="gray">
              <IconBrain size={12} />
            </ThemeIcon>
            <Text size="xs" c="dimmed" fw={600}>
              Thought process
            </Text>
          </Group>
          <Button
            size="compact-xs"
            variant="subtle"
            color="gray"
            leftSection={expanded ? <IconChevronDown size={12} /> : <IconChevronRight size={12} />}
            onClick={() => setExpanded((x) => !x)}
          >
            {expanded ? 'Hide' : 'Show'}
          </Button>
        </Group>
        {expanded ? (
          <Box mt="xs" style={{ fontStyle: 'italic' }}>
            <MarkdownBody text={text} size="xs" c="dimmed" />
          </Box>
        ) : null}
      </Card>
    </Group>
  );
}

type MarkdownBlock =
  | { type: 'paragraph'; text: string }
  | { type: 'heading'; level: number; text: string }
  | { type: 'list'; ordered: boolean; items: string[] }
  | { type: 'code'; language?: string; text: string };

function MarkdownBody({ text, size = 'sm', c }: { text: string; size?: 'xs' | 'sm'; c?: string }) {
  const blocks = parseMarkdownBlocks(text);
  if (blocks.length === 0) return null;
  return (
    <Stack gap={6}>
      {blocks.map((block, i) => {
        if (block.type === 'code') {
          return (
            <Box key={i}>
              {block.language ? (
                <Text size="xs" c="dimmed" mb={2}>
                  {block.language}
                </Text>
              ) : null}
              <Code block style={{ whiteSpace: 'pre-wrap', fontSize: 12 }}>
                {block.text}
              </Code>
            </Box>
          );
        }
        if (block.type === 'heading') {
          return (
            <Text key={i} size={block.level <= 2 ? 'md' : 'sm'} fw={700} c={c}>
              {renderInlineMarkdown(block.text)}
            </Text>
          );
        }
        if (block.type === 'list') {
          return (
            <List
              key={i}
              type={block.ordered ? 'ordered' : 'unordered'}
              size={size}
              spacing={3}
              c={c}
            >
              {block.items.map((item, j) => (
                <List.Item key={j}>{renderInlineMarkdown(item)}</List.Item>
              ))}
            </List>
          );
        }
        return (
          <Text key={i} size={size} c={c} style={{ whiteSpace: 'pre-wrap' }}>
            {renderInlineMarkdown(block.text)}
          </Text>
        );
      })}
    </Stack>
  );
}

function parseMarkdownBlocks(text: string): MarkdownBlock[] {
  const lines = text.replace(/\r\n/g, '\n').split('\n');
  const blocks: MarkdownBlock[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i] ?? '';
    if (!line.trim()) {
      i++;
      continue;
    }
    const fence = line.match(/^```([\w-]*)\s*$/);
    if (fence) {
      const language = fence[1] || undefined;
      const body: string[] = [];
      i++;
      while (i < lines.length && !(lines[i] ?? '').match(/^```\s*$/)) {
        body.push(lines[i] ?? '');
        i++;
      }
      if (i < lines.length) i++;
      blocks.push({ type: 'code', language, text: body.join('\n') });
      continue;
    }
    const heading = line.match(/^(#{1,4})\s+(.+)$/);
    if (heading) {
      blocks.push({ type: 'heading', level: heading[1]!.length, text: heading[2]! });
      i++;
      continue;
    }
    const unordered = line.match(/^\s*[-*]\s+(.+)$/);
    const ordered = line.match(/^\s*\d+\.\s+(.+)$/);
    if (unordered || ordered) {
      const isOrdered = Boolean(ordered);
      const items: string[] = [];
      while (i < lines.length) {
        const candidate = lines[i] ?? '';
        const match = isOrdered
          ? candidate.match(/^\s*\d+\.\s+(.+)$/)
          : candidate.match(/^\s*[-*]\s+(.+)$/);
        if (!match) break;
        items.push(match[1]!);
        i++;
      }
      blocks.push({ type: 'list', ordered: isOrdered, items });
      continue;
    }
    const paragraph: string[] = [];
    while (i < lines.length) {
      const candidate = lines[i] ?? '';
      if (!candidate.trim()) break;
      if (
        candidate.match(/^```/) ||
        candidate.match(/^(#{1,4})\s+(.+)$/) ||
        candidate.match(/^\s*[-*]\s+(.+)$/) ||
        candidate.match(/^\s*\d+\.\s+(.+)$/)
      ) {
        break;
      }
      paragraph.push(candidate);
      i++;
    }
    blocks.push({ type: 'paragraph', text: paragraph.join('\n') });
  }
  return blocks;
}

function renderInlineMarkdown(text: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  const pattern = /(`[^`]+`|\*\*[^*]+\*\*|\*[^*\n]+\*|\[[^\]\n]+\]\(https?:\/\/[^)\s]+\))/g;
  let last = 0;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(text))) {
    if (match.index > last) nodes.push(text.slice(last, match.index));
    const token = match[0];
    const key = `${match.index}-${token}`;
    if (token.startsWith('`')) {
      nodes.push(
        <Code key={key} fz="0.85em">
          {token.slice(1, -1)}
        </Code>,
      );
    } else if (token.startsWith('**')) {
      nodes.push(
        <Text key={key} span fw={700} inherit>
          {token.slice(2, -2)}
        </Text>,
      );
    } else if (token.startsWith('*')) {
      nodes.push(
        <Text key={key} span fs="italic" inherit>
          {token.slice(1, -1)}
        </Text>,
      );
    } else {
      const link = token.match(/^\[([^\]\n]+)\]\((https?:\/\/[^)\s]+)\)$/);
      if (link) {
        nodes.push(
          <Anchor key={key} href={link[2]} target="_blank" rel="noreferrer">
            {link[1]}
          </Anchor>,
        );
      } else {
        nodes.push(token);
      }
    }
    last = match.index + token.length;
  }
  if (last < text.length) nodes.push(text.slice(last));
  return nodes;
}

const TOOL_STATUS_COLOR: Record<string, string> = {
  pending: 'gray',
  in_progress: 'yellow',
  completed: 'green',
  failed: 'red',
};

function ToolCallCard({ call }: { call: ChatToolCall }) {
  const [expanded, setExpanded] = useState(false);
  const title = call.title ?? humanizeToolFromInput(call) ?? '(tool)';
  const status = call.status ?? 'pending';
  const hasDetails =
    call.rawInput != null || call.rawOutput != null || (call.content && call.content.length > 0);
  return (
    <Card
      padding="sm"
      radius="md"
      withBorder
      maw="92%"
      style={{
        alignSelf: 'flex-start',
        background: 'var(--mantine-color-dark-7)',
      }}
    >
      <Group gap="xs" justify="space-between" wrap="nowrap">
        <Group gap="xs" wrap="nowrap" style={{ minWidth: 0, flex: 1 }}>
          <ThemeIcon size="sm" variant="light" color={TOOL_STATUS_COLOR[status] ?? 'gray'}>
            {iconForKind(call.kind)}
          </ThemeIcon>
          <Text size="sm" fw={500} truncate>
            {title}
          </Text>
          <Badge size="xs" color={TOOL_STATUS_COLOR[status] ?? 'gray'} variant="light">
            {status}
          </Badge>
        </Group>
        {hasDetails ? (
          <Tooltip label={expanded ? 'Hide details' : 'Show details'}>
            <ActionIcon variant="subtle" size="sm" onClick={() => setExpanded((x) => !x)}>
              <IconArrowRight
                size={12}
                style={{
                  transform: expanded ? 'rotate(90deg)' : undefined,
                  transition: 'transform 120ms',
                }}
              />
            </ActionIcon>
          </Tooltip>
        ) : null}
      </Group>
      {call.locations && call.locations.length > 0 ? (
        <Group gap={4} mt={6} wrap="wrap">
          {call.locations.map((loc, i) => (
            <Badge
              key={i}
              size="xs"
              variant="light"
              color="blue"
              leftSection={<IconFileText size={10} />}
            >
              {shortenPath(loc.path)}
              {loc.line ? `:${loc.line}` : ''}
            </Badge>
          ))}
        </Group>
      ) : null}
      {expanded ? (
        <Stack gap="xs" mt="sm">
          {call.content?.map((c, i) => (
            <ToolContentBlockView key={i} block={c} />
          ))}
          {call.rawInput != null ? (
            <Box>
              <Text size="xs" c="dimmed">
                input
              </Text>
              <Code block style={{ whiteSpace: 'pre-wrap', fontSize: 11 }}>
                {safeStringify(call.rawInput)}
              </Code>
            </Box>
          ) : null}
          {call.rawOutput != null ? (
            <Box>
              <Text size="xs" c="dimmed">
                output
              </Text>
              <Code block style={{ whiteSpace: 'pre-wrap', fontSize: 11 }}>
                {safeStringify(call.rawOutput)}
              </Code>
            </Box>
          ) : null}
        </Stack>
      ) : null}
    </Card>
  );
}

function iconForKind(kind: ChatToolCall['kind']) {
  switch (kind) {
    case 'read':
      return <IconFileSearch size={12} />;
    case 'edit':
      return <IconEdit size={12} />;
    case 'delete':
      return <IconTrash size={12} />;
    case 'search':
      return <IconSearch size={12} />;
    case 'execute':
      return <IconTerminal2 size={12} />;
    case 'fetch':
      return <IconWorld size={12} />;
    case 'think':
      return <IconBrain size={12} />;
    case 'switch_mode':
      return <IconBolt size={12} />;
    case 'move':
      return <IconArrowRight size={12} />;
    default:
      return <IconTool size={12} />;
  }
}

/** Best-effort title from rawInput when the agent didn't send one. */
function humanizeToolFromInput(call: ChatToolCall): string | null {
  if (!call.rawInput || typeof call.rawInput !== 'object') return null;
  const input = call.rawInput as Record<string, unknown>;
  if (typeof input.command === 'string') return `$ ${input.command}`;
  if (typeof input.path === 'string') return input.path as string;
  if (typeof input.file_path === 'string') return input.file_path as string;
  if (typeof input.url === 'string') return input.url as string;
  if (typeof input.query === 'string') return `“${input.query}”`;
  return null;
}

function shortenPath(p: string): string {
  if (p.length <= 40) return p;
  const idx = p.lastIndexOf('/');
  if (idx < 0) return p.slice(-40);
  return '…/' + p.slice(idx + 1);
}

function ToolContentBlockView({ block }: { block: ToolCallContentBlock }) {
  if (block.type === 'content') {
    const inner = (block as { content?: { type?: string; text?: string } }).content;
    if (inner?.type === 'text' && typeof inner.text === 'string') {
      return (
        <Text size="xs" ff="monospace" style={{ whiteSpace: 'pre-wrap' }}>
          {inner.text}
        </Text>
      );
    }
  }
  if (block.type === 'diff') {
    const d = block as { path: string; oldText?: string | null; newText: string };
    return (
      <Box>
        <Text size="xs" c="dimmed">
          diff · {d.path}
        </Text>
        <Code block style={{ whiteSpace: 'pre-wrap', fontSize: 11 }}>
          {d.newText}
        </Code>
      </Box>
    );
  }
  if (block.type === 'terminal') {
    const t = block as { terminalId: string };
    return (
      <Group gap="xs">
        <IconTerminal2 size={12} />
        <Text size="xs" c="dimmed">
          terminal {t.terminalId}
        </Text>
      </Group>
    );
  }
  return (
    <Code block style={{ whiteSpace: 'pre-wrap', fontSize: 11 }}>
      {safeStringify(block)}
    </Code>
  );
}

function PermissionCard({
  params,
  onAllow,
  onDeny,
}: {
  params: Record<string, unknown>;
  onAllow: () => void;
  onDeny: () => void;
}) {
  return (
    <Card padding="sm" radius="md" withBorder bg="yellow.9">
      <Group gap="xs">
        <IconShieldCheck size={14} />
        <Text size="sm" fw={600}>
          Permission requested
        </Text>
      </Group>
      <Text size="xs" ff="monospace" mt="xs" style={{ whiteSpace: 'pre-wrap' }}>
        {safeStringify(params)}
      </Text>
      <Group gap="xs" mt="xs">
        <Button
          size="xs"
          color="green"
          variant="filled"
          leftSection={<IconCheck size={12} />}
          onClick={onAllow}
        >
          Allow
        </Button>
        <Button
          size="xs"
          color="red"
          variant="light"
          leftSection={<IconX size={12} />}
          onClick={onDeny}
        >
          Deny
        </Button>
      </Group>
    </Card>
  );
}

function safeStringify(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}
