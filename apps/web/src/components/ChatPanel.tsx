import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Card,
  Collapse,
  Group,
  List,
  Loader,
  Menu,
  Modal,
  Paper,
  Popover,
  Progress,
  ScrollArea,
  SegmentedControl,
  Stack,
  Text,
  Textarea,
  TextInput,
  ThemeIcon,
  Tooltip,
  UnstyledButton,
} from '@mantine/core';
import type {
  AvailableCommand,
  ChatAttachment,
  ChatConfigOption,
  ChatSessionStatus,
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
  IconChevronLeft,
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
  IconShare3,
  IconShieldCheck,
  IconTerminal2,
  IconTool,
  IconTrash,
  IconUpload,
  IconWorld,
  IconX,
} from '@tabler/icons-react';
import { memo, type ReactNode, useEffect, useMemo, useRef, useState } from 'react';

import type { ChatMessage, ChatToolCall, SessionState } from '../store/chat.js';
import { useChat } from '../store/chat.js';
import { useConnection } from '../store/connection.js';
import { useFiles } from '../store/files.js';
import { MarkdownBody, StreamingMarkdownBody } from './Markdown.js';
import { groupSessionList, type GuestSessionGroup } from './session-groups.js';
import {
  groupTranscript,
  hasMeaningfulPayload,
  isActiveToolCall,
  type TranscriptEntry,
} from './transcript.js';

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

  const { regular, guestGroups } = useMemo(() => groupSessionList(sessions), [sessions]);

  return (
    <Group align="stretch" gap="md" h="100%" wrap="nowrap" style={{ minHeight: 0 }}>
      <Stack
        gap="xs"
        miw={280}
        maw={360}
        p="sm"
        style={{
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 8,
          flexShrink: 0,
        }}
      >
        <Group justify="space-between">
          <Text fw={600}>Sessions</Text>
          <Tooltip label="Start a new agent session">
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
        <Box style={{ maxHeight: '60vh', overflowY: 'auto', overflowX: 'hidden' }}>
          <Stack gap={4}>
            {regular.length === 0 && guestGroups.length === 0 ? (
              <Text size="sm" c="dimmed">
                No sessions yet. Click + to start one.
              </Text>
            ) : (
              <>
                {regular.map((s) => (
                  <SessionRow
                    key={s.session.id}
                    state={s}
                    active={activeId === s.session.id}
                    onSelect={() => setActive(s.session.id)}
                    onClose={() => void closeSession(s.session.id)}
                  />
                ))}
                {guestGroups.map((g) => (
                  <GuestShareGroup
                    key={g.shareId || '(unknown)'}
                    group={g}
                    activeId={activeId}
                    onSelect={setActive}
                    onClose={(id) => void closeSession(id)}
                  />
                ))}
              </>
            )}
          </Stack>
        </Box>
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
            <Text c="dimmed">Pick a session or start a new one to chat with your agent.</Text>
          </Stack>
        )}
      </Box>

      <CreateSessionModal opened={creatorOpen} onClose={() => setCreatorOpen(false)} />
    </Group>
  );
}

function SessionRow({
  state,
  active,
  onSelect,
  onClose,
}: {
  state: SessionState;
  active: boolean;
  onSelect: () => void;
  onClose: () => void;
}) {
  const s = state.session;
  return (
    <Group
      gap="xs"
      px="xs"
      py={6}
      wrap="nowrap"
      onClick={onSelect}
      style={{
        cursor: 'pointer',
        borderRadius: 6,
        background: active ? 'light-dark(#e9eefe, var(--mantine-color-brand-9))' : 'transparent',
        color: active ? 'light-dark(var(--mantine-color-brand-8), #ffffff)' : undefined,
      }}
    >
      <IconMessageCircle size={14} />
      <Stack gap={0} style={{ flex: 1, minWidth: 0 }}>
        <Text size="xs" fw={500} truncate>
          {s.title || s.cwd}
        </Text>
        <Text size="xs" c="dimmed" truncate>
          {s.agent_id}
        </Text>
      </Stack>
      <Badge size="xs" color={STATUS_COLOR[s.status] ?? 'gray'} variant="light">
        {s.status}
      </Badge>
      <Tooltip label="Close session">
        <ActionIcon
          size="sm"
          variant="subtle"
          color="gray"
          onClick={(e) => {
            e.stopPropagation();
            onClose();
          }}
        >
          <IconX size={14} />
        </ActionIcon>
      </Tooltip>
    </Group>
  );
}

/**
 * One share link's guest sessions as a collapsible group, so the owner can
 * monitor a link's chatter in one place and revoke the link from its header.
 * Rows reuse the normal session row; opening one shows the view-only chat.
 */
function GuestShareGroup({
  group,
  activeId,
  onSelect,
  onClose,
}: {
  group: GuestSessionGroup;
  activeId: string | null;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
}) {
  const [open, setOpen] = useState(true);
  const first = group.sessions[0]!.session;
  const label = group.shareId ? `分享链接 ·${group.shareId.slice(-6)}` : '分享链接（旧版）';
  return (
    <Stack gap={2}>
      <Group
        gap={6}
        px="xs"
        py={4}
        wrap="nowrap"
        onClick={() => setOpen((o) => !o)}
        style={{ cursor: 'pointer', borderRadius: 6 }}
      >
        {open ? <IconChevronDown size={12} /> : <IconChevronRight size={12} />}
        <IconShare3 size={14} />
        <Stack gap={0} style={{ flex: 1, minWidth: 0 }}>
          <Text size="xs" fw={600} truncate>
            {label}
          </Text>
          <Text size="xs" c="dimmed" truncate>
            {agentShortName(first.agent_id)} · {first.cwd}
          </Text>
        </Stack>
        <Badge size="xs" variant="light" color="grape">
          {group.sessions.length}
        </Badge>
        {group.shareId ? <RevokeShareButton shareId={group.shareId} /> : null}
      </Group>
      <Collapse in={open}>
        <Stack gap={4} pl="md">
          {group.sessions.map((s) => (
            <SessionRow
              key={s.session.id}
              state={s}
              active={activeId === s.session.id}
              onSelect={() => onSelect(s.session.id)}
              onClose={() => onClose(s.session.id)}
            />
          ))}
        </Stack>
      </Collapse>
    </Stack>
  );
}

/**
 * Popover-confirmed revoke of a share link: the relay marks it revoked and
 * disconnects its live guests. Existing sessions stay listed (close them per
 * row) — revoking only stops the link from being used again.
 */
function RevokeShareButton({ shareId }: { shareId: string }) {
  const revokeAskShare = useConnection((s) => s.revokeAskShare);
  const [opened, setOpened] = useState(false);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function onRevoke() {
    setError(null);
    setBusy(true);
    try {
      await revokeAskShare(shareId);
      setDone(true);
      setOpened(false);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  if (done) {
    return (
      <Tooltip label="链接已撤销（已建的会话保留，可逐个关闭）">
        <ThemeIcon size="sm" variant="light" color="gray">
          <IconCheck size={12} />
        </ThemeIcon>
      </Tooltip>
    );
  }
  return (
    <Popover opened={opened} onChange={setOpened} width={240} position="bottom-end" withArrow>
      <Popover.Target>
        <Tooltip label="撤销该分享链接">
          <ActionIcon
            size="sm"
            variant="subtle"
            color="red"
            onClick={(e) => {
              e.stopPropagation();
              setOpened((o) => !o);
            }}
          >
            <IconTrash size={14} />
          </ActionIcon>
        </Tooltip>
      </Popover.Target>
      <Popover.Dropdown onClick={(e) => e.stopPropagation()}>
        <Stack gap="xs">
          <Text size="xs">
            撤销后链接立即失效，在线访客会被断开；已创建的会话保留，可在列表里逐个关闭。
          </Text>
          {error ? (
            <Text size="xs" c="red">
              {error}
            </Text>
          ) : null}
          <Group justify="flex-end" gap="xs">
            <Button size="compact-xs" variant="default" onClick={() => setOpened(false)}>
              取消
            </Button>
            <Button size="compact-xs" color="red" loading={busy} onClick={() => void onRevoke()}>
              撤销
            </Button>
          </Group>
        </Stack>
      </Popover.Dropdown>
    </Popover>
  );
}

function CreateSessionModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const create = useChat((s) => s.createSession);
  const agents = useChat((s) => s.agents);
  const [agentId, setAgentId] = useState('devin');
  const [cwd, setCwd] = useState('');
  const [title, setTitle] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The daemon's chat.agents response is authoritative for what is actually
  // wired up + installed over there. Until it arrives, fall back to Devin.
  const availableAgents = useMemo(() => agents.filter((a) => a.available), [agents]);
  useEffect(() => {
    if (availableAgents.length === 0) return;
    if (!availableAgents.some((a) => a.id === agentId)) {
      setAgentId(availableAgents[0]!.id);
    }
  }, [availableAgents, agentId]);

  async function onSubmit() {
    setError(null);
    setBusy(true);
    try {
      await create({
        cwd: cwd.trim() || undefined,
        agentId,
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
    <Modal opened={opened} onClose={onClose} title="Start a session" centered size="lg">
      <Stack gap="md">
        {availableAgents.length > 1 ? (
          <SegmentedControl
            value={agentId}
            onChange={setAgentId}
            data={availableAgents.map((a) => ({ value: a.id, label: a.name }))}
          />
        ) : null}
        <TextInput
          label="Working directory"
          description="Required. Pick a project directory below — agents refuse to operate on broad roots like $HOME."
          placeholder="Select a directory"
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
        {agents.length > 0 && availableAgents.length === 0 ? (
          <Alert color="yellow" variant="light" radius="md">
            No agents are available on this daemon. Install and authenticate a CLI (devin or
            cursor-agent) on that machine.
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
          <Button onClick={onSubmit} loading={busy} disabled={!cwd.trim()}>
            Start
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}

export function DirectoryPicker({
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
        <Button size="xs" variant="subtle" onClick={() => onSelect('')} disabled={!selectedPath}>
          Clear
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
  const refreshToken = useFiles((s) => s.refreshToken);
  const cacheKey = `${workspaceId ?? ''}|${path}`;
  const cached = useFiles((s) => s.listings[cacheKey]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const isOpen = openDirs.has(path);
  const absolutePath = joinWorkspacePath(workspaceRoot, path);
  const active = selectedPath === absolutePath;

  // Re-fetch on open / workspace resolve / refresh (keeps the cached entries
  // visible meanwhile) so re-expanding a folder reflects filesystem changes.
  useEffect(() => {
    if (!isOpen || !workspaceId) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    list(path)
      .catch((e: Error) => {
        if (!cancelled) setError(e.message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [isOpen, workspaceId, refreshToken, list, path]);

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
        pr="xs"
        wrap="nowrap"
        onClick={() => onSelect(absolutePath)}
        style={{
          cursor: 'pointer',
          paddingLeft: 8 + depth * 16,
          background: active ? 'light-dark(#e9eefe, var(--mantine-color-brand-9))' : 'transparent',
          color: active ? 'light-dark(var(--mantine-color-brand-8), #ffffff)' : undefined,
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
  const setConfigOption = useChat((s) => s.setConfigOption);
  const resolvePermission = useChat((s) => s.resolvePermission);
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [attachments, setAttachments] = useState<ChatAttachment[]>([]);
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

  const transcriptItems = useMemo(() => {
    const entries: TranscriptEntry[] = [
      ...(session?.messages ?? []).map((message) => ({
        type: 'message' as const,
        order: message.order,
        message,
      })),
      ...Object.values(session?.toolCalls ?? {}).map((call) => ({
        type: 'tool' as const,
        order: call.order,
        call,
      })),
    ].sort((a, b) => a.order - b.order);
    return groupTranscript(entries);
  }, [session?.messages, session?.toolCalls]);

  if (!session) return null;

  const status = session.session.status;
  // Guest (share-link) sessions are view-only for the owner: the daemon
  // rejects owner prompt/set_mode/set_config_option on them, so the UI
  // hides the composer and mode picker; cancel stays for management.
  const isGuestSession = session.session.origin === 'guest';
  const pendingPermissions = Object.values(session.permissions).filter((p) => p.pending);
  const modelOption = session.configOptions.find((o) => o.id.toLowerCase() === 'model') ?? null;

  async function onSend() {
    if (!draft.trim() && attachments.length === 0) return;
    setError(null);
    setBusy(true);
    try {
      const text = draft;
      setDraft('');
      const currentAttachments = [...attachments];
      setAttachments([]);
      await sendPrompt(sessionId, text, currentAttachments);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Stack style={{ flex: 1, minHeight: 0 }} gap="xs">
      <Group justify="space-between" px="xs" wrap="nowrap">
        <Stack gap={0} style={{ minWidth: 0, flex: 1 }}>
          <Text fw={600} size="sm" truncate>
            {session.session.title || session.session.cwd}
          </Text>
          <Tooltip label={session.session.cwd} disabled={!session.session.cwd}>
            <Text size="xs" c="dimmed" truncate>
              {session.session.agent_id} · {session.session.cwd}
            </Text>
          </Tooltip>
        </Stack>
        <Group gap="xs">
          {isGuestSession ? (
            <Tooltip label="访客通过分享链接创建的会话 — 只读旁观（固定 ask 模式），可停止/关闭">
              <Badge color="grape" variant="light" leftSection={<IconShare3 size={10} />}>
                访客
              </Badge>
            </Tooltip>
          ) : null}
          {session.modes.length > 0 && !isGuestSession ? (
            <SegmentedControl
              size="xs"
              data={session.modes.map((m) => ({ label: m.name, value: m.id }))}
              value={session.currentModeId ?? session.modes[0]?.id}
              onChange={(value) => void setMode(sessionId, value)}
              transitionDuration={0}
            />
          ) : null}
          {status === 'awaiting_permission' ? (
            <Tooltip label={'智能体在等待授权 — 请在下方"需要授权"卡片里点允许或拒绝'}>
              <Badge color={STATUS_COLOR[status] ?? 'gray'} variant="light">
                {status}
              </Badge>
            </Tooltip>
          ) : (
            <Badge
              className={status === 'thinking' ? 'cw-pulse' : undefined}
              color={STATUS_COLOR[status] ?? 'gray'}
              variant="light"
            >
              {status}
            </Badge>
          )}
        </Group>
      </Group>

      {/* Header above spans the full chat panel. Below we keep the
          ScrollArea at full panel width (so its vertical scrollbar
          stays glued to the panel's right edge — moving the scrollbar
          into the middle of the panel was the chief visual complaint),
          while constraining individual sections (plan card, messages,
          permission cards, error alert, composer) to a centered
          "reading column" so the layout doesn't sprawl across
          ultra-wide displays. ReadingColumn is just a thin wrapper
          that does width:100% + maxWidth + margin:auto. */}
      {session.plan && session.plan.length > 0 ? (
        <ReadingColumn>
          <PlanCard entries={session.plan} />
        </ReadingColumn>
      ) : null}

      <ScrollArea
        viewportRef={scrollRef}
        style={{ flex: 1, minHeight: 0 }}
        type="auto"
        offsetScrollbars
      >
        <ReadingColumn>
          <Stack gap="sm" px="xs">
            {transcriptItems.map((item) =>
              item.type === 'message' ? (
                <MessageBubble key={item.key} message={item.message} />
              ) : item.type === 'tool' ? (
                <ToolCallCard key={item.key} call={item.call} />
              ) : (
                <ToolCallGroupCard key={item.key} calls={item.calls} />
              ),
            )}
          </Stack>
        </ReadingColumn>
      </ScrollArea>

      {pendingPermissions.length > 0 ? (
        <ReadingColumn>
          <Stack gap="xs" px="xs" mt="sm">
            {pendingPermissions.map((p) => (
              <PermissionCard
                key={p.requestId}
                params={p.params}
                onResolve={(decision) => void resolvePermission(sessionId, p.requestId, decision)}
              />
            ))}
          </Stack>
        </ReadingColumn>
      ) : null}

      {error ? (
        <ReadingColumn>
          <Alert color="red" variant="light" radius="md">
            {error}
          </Alert>
        </ReadingColumn>
      ) : null}

      <ReadingColumn>
        {isGuestSession ? (
          <Paper withBorder radius="md" p="xs">
            <Group justify="space-between" wrap="nowrap" gap="xs">
              <Text size="xs" c="dimmed">
                访客会话只读 — 你可以实时旁观；必要时停止当前回合，或在左侧列表关闭会话/撤销链接。
              </Text>
              {status === 'thinking' ? (
                <Button
                  size="compact-xs"
                  color="red"
                  variant="light"
                  leftSection={<IconPlayerStop size={12} />}
                  onClick={() => void cancel(sessionId)}
                >
                  停止
                </Button>
              ) : null}
            </Group>
          </Paper>
        ) : (
          <CommandInput
            commands={session.availableCommands}
            value={draft}
            onChange={setDraft}
            onSubmit={onSend}
            busy={busy}
            attachments={attachments}
            onAttachmentsChange={setAttachments}
            status={status}
            onCancel={() => void cancel(sessionId)}
            modelOption={modelOption}
            onModelChange={(value) =>
              modelOption ? void setConfigOption(sessionId, modelOption.id, value) : undefined
            }
            usage={session.usage}
            agentName={agentShortName(session.session.agent_id)}
          />
        )}
      </ReadingColumn>
    </Stack>
  );
}

// agentShortName renders an agent id as the short display name used in the
// composer placeholder ("问问 Cursor…"). Mirrors guestAgentName in GuestChat.
function agentShortName(agentId: string): string {
  switch (agentId) {
    case 'cursor-cli':
      return 'Cursor';
    case 'devin':
    case '':
      return 'Devin';
    default:
      return agentId;
  }
}

// composerPlaceholder names the session's agent (not always Devin!) and
// hints the first few slash commands the agent actually advertises.
function composerPlaceholder(agentName: string, commands: AvailableCommand[]): string {
  const hint = commands
    .slice(0, 3)
    .map((c) => `/${c.name}`)
    .join('、');
  return hint ? `问问 ${agentName}…（试试 ${hint}）` : `问问 ${agentName}…`;
}

// Centers its child within a max-width column. Used to give the chat
// transcript / composer / status cards a comfortable reading width on
// wide screens without affecting the ScrollArea's full-width scrollbar
// or the header's full-width layout.
function ReadingColumn({ children }: { children: ReactNode }) {
  return (
    <Box
      style={{
        width: '100%',
        maxWidth: 1000,
        marginLeft: 'auto',
        marginRight: 'auto',
      }}
    >
      {children}
    </Box>
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

function configOptionCurrentLabel(option: ChatConfigOption | null): string | null {
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

// --- Model picker ---------------------------------------------------------
//
// The agent ships ~80 model identifiers in a single flat `options` array
// (`Claude Opus 4.8 Max`, `GPT-5.5 Low Thinking`, `Gemini 3.5 Flash`, …).
// We expose them as a two-level picker: the first view lists model
// families and clicking a family drills into its variants. Families are
// derived automatically by stripping the trailing effort/speed tokens
// from each label, so a brand-new model line (e.g. a future "Claude Opus
// 4.9") groups itself without any hardcoded rule. The search box stays
// available across both views and switches to a flat result list (with
// full family-qualified names) whenever it has text.

// Effort / speed suffixes that distinguish variants *within* a family.
// The family name is everything up to the first trailing token from this
// set, so "Claude Opus 4.8 Low Fast" → family "Claude Opus 4.8".
const MODEL_VARIANT_TOKENS = new Set([
  'low',
  'medium',
  'high',
  'xhigh',
  'max',
  'minimal',
  'fast',
  'thinking',
  'reasoning',
]);

function familyForModel(label: string): string {
  const parts = label.trim().split(/\s+/);
  let end = parts.length;
  while (end > 1 && MODEL_VARIANT_TOKENS.has((parts[end - 1] ?? '').toLowerCase())) {
    end -= 1;
  }
  return parts.slice(0, end).join(' ') || label.trim();
}

interface ModelChoice {
  value: string;
  label: string;
  description: string | null;
}

function ModelPicker({
  option,
  onChange,
}: {
  option: ChatConfigOption;
  onChange: (value: string) => void;
}) {
  const [opened, setOpened] = useState(false);
  const [filter, setFilter] = useState('');
  const [selectedFamily, setSelectedFamily] = useState<string | null>(null);

  const choices = useMemo<ModelChoice[]>(
    () =>
      (option.options ?? []).flatMap((raw) => {
        const value = stringValue(raw.value);
        if (!value) return [];
        const label = stringValue(raw.label) ?? stringValue(raw.name) ?? value;
        return [
          {
            value,
            label,
            description: stringValue(raw.description),
          },
        ];
      }),
    [option.options],
  );

  // Bucket every model into its detected family, preserving the agent's
  // own ordering via first-appearance of each family.
  const families = useMemo(() => {
    const map = new Map<string, ModelChoice[]>();
    for (const c of choices) {
      const fam = familyForModel(c.label);
      const list = map.get(fam) ?? [];
      list.push(c);
      map.set(fam, list);
    }
    return [...map.entries()].map(([family, items]) => ({ family, items }));
  }, [choices]);

  // Flat search results across all families. Only used while the search
  // box has text; otherwise we fall back to the family / variant views.
  const searchResults = useMemo(() => {
    const lower = filter.trim().toLowerCase();
    if (!lower) return [];
    return choices.filter(
      (c) => c.label.toLowerCase().includes(lower) || c.value.toLowerCase().includes(lower),
    );
  }, [choices, filter]);

  const currentValue = stringValue(option.currentValue);
  const currentLabel = configOptionCurrentLabel(option) ?? '(unset)';

  const isSearching = filter.trim().length > 0;
  const drilled =
    !isSearching && selectedFamily
      ? (families.find((g) => g.family === selectedFamily) ?? null)
      : null;

  // Reset the drill-in + filter whenever the popover closes so reopening
  // always lands on the family list.
  useEffect(() => {
    if (!opened) {
      setFilter('');
      setSelectedFamily(null);
    }
  }, [opened]);

  const handlePick = (value: string) => {
    setOpened(false);
    if (value !== currentValue) onChange(value);
  };

  return (
    <Popover
      opened={opened}
      onChange={setOpened}
      position="bottom-end"
      shadow="md"
      withArrow
      offset={6}
    >
      <Popover.Target>
        <UnstyledButton onClick={() => setOpened((o) => !o)} aria-label="Switch model">
          <Tooltip label="Click to switch model" disabled={opened}>
            <Badge
              color="violet"
              variant="light"
              maw={260}
              style={{ textTransform: 'none', cursor: 'pointer' }}
              rightSection={<IconChevronDown size={12} />}
            >
              Model · {currentLabel}
            </Badge>
          </Tooltip>
        </UnstyledButton>
      </Popover.Target>
      <Popover.Dropdown p={0}>
        <Stack gap={0} w={340}>
          <Box p="xs" style={{ borderBottom: '1px solid var(--mantine-color-default-border)' }}>
            <TextInput
              size="xs"
              placeholder="搜索模型…"
              value={filter}
              onChange={(e) => setFilter(e.currentTarget.value)}
              autoFocus
              leftSection={<IconSearch size={12} />}
            />
          </Box>
          {drilled ? (
            <UnstyledButton
              onClick={() => setSelectedFamily(null)}
              px="sm"
              py={8}
              style={{ borderBottom: '1px solid var(--mantine-color-default-border)' }}
              aria-label="返回模型族列表"
            >
              <Group gap={6} wrap="nowrap">
                <IconChevronLeft size={14} />
                <Text
                  size="xs"
                  fw={600}
                  c="dimmed"
                  style={{ textTransform: 'uppercase', letterSpacing: 0.3 }}
                >
                  {drilled.family}
                </Text>
                <Text size="xs" c="dimmed">
                  · {drilled.items.length} 个变体
                </Text>
              </Group>
            </UnstyledButton>
          ) : null}
          <ScrollArea.Autosize mah={360} type="auto">
            {isSearching ? (
              searchResults.length === 0 ? (
                <Text size="xs" c="dimmed" p="md" ta="center">
                  没有匹配的模型
                </Text>
              ) : (
                <Stack gap={2} p={4}>
                  {searchResults.map((item) => (
                    <ModelRow
                      key={item.value}
                      label={item.label}
                      description={item.description}
                      active={item.value === currentValue}
                      onClick={() => handlePick(item.value)}
                    />
                  ))}
                </Stack>
              )
            ) : drilled ? (
              <Stack gap={2} p={4}>
                {drilled.items.map((item) => (
                  <ModelRow
                    key={item.value}
                    label={item.label}
                    description={item.description}
                    active={item.value === currentValue}
                    onClick={() => handlePick(item.value)}
                  />
                ))}
              </Stack>
            ) : families.length === 0 ? (
              <Text size="xs" c="dimmed" p="md" ta="center">
                没有可用的模型
              </Text>
            ) : (
              <Stack gap={2} p={4}>
                {families.map((group) => {
                  const containsCurrent = group.items.some((i) => i.value === currentValue);
                  return (
                    <UnstyledButton
                      key={group.family}
                      onClick={() => setSelectedFamily(group.family)}
                      p="xs"
                      w="100%"
                      style={{
                        borderRadius: 4,
                        background: containsCurrent
                          ? 'light-dark(#e9eefe, var(--mantine-color-brand-9))'
                          : 'transparent',
                      }}
                    >
                      <Group gap="xs" justify="space-between" wrap="nowrap">
                        <Stack gap={0} style={{ minWidth: 0, flex: 1 }}>
                          <Text size="sm" fw={containsCurrent ? 600 : 500} truncate>
                            {group.family}
                          </Text>
                          <Text size="xs" c="dimmed" truncate>
                            {group.items.length} 个变体
                          </Text>
                        </Stack>
                        <IconChevronRight size={14} stroke={1.5} />
                      </Group>
                    </UnstyledButton>
                  );
                })}
              </Stack>
            )}
          </ScrollArea.Autosize>
        </Stack>
      </Popover.Dropdown>
    </Popover>
  );
}

function ModelRow({
  label,
  description,
  active,
  onClick,
}: {
  label: string;
  description: string | null;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <UnstyledButton
      onClick={onClick}
      p="xs"
      w="100%"
      style={{
        borderRadius: 4,
        background: active ? 'light-dark(#e9eefe, var(--mantine-color-brand-9))' : 'transparent',
      }}
    >
      <Group gap="xs" justify="space-between" wrap="nowrap">
        <Stack gap={0} style={{ minWidth: 0, flex: 1 }}>
          <Text size="sm" fw={active ? 600 : 500} truncate>
            {label}
          </Text>
          {description ? (
            <Text size="xs" c="dimmed" truncate>
              {description}
            </Text>
          ) : null}
        </Stack>
        {active ? (
          <ThemeIcon size="xs" color="brand" variant="light">
            <IconCheck size={10} />
          </ThemeIcon>
        ) : null}
      </Group>
    </UnstyledButton>
  );
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

const ATTACHMENT_MAX_BYTES = 10 * 1024 * 1024;

function CommandInput({
  commands,
  value,
  onChange,
  onSubmit,
  busy,
  attachments,
  onAttachmentsChange,
  status,
  onCancel,
  modelOption,
  onModelChange,
  usage,
  agentName,
}: {
  commands: AvailableCommand[];
  value: string;
  onChange: (v: string) => void;
  onSubmit: () => void | Promise<void>;
  busy: boolean;
  attachments: ChatAttachment[];
  onAttachmentsChange: (attachments: ChatAttachment[]) => void;
  status: ChatSessionStatus;
  onCancel: () => void;
  modelOption: ChatConfigOption | null;
  onModelChange: (value: string) => void;
  usage: ChatUsage | null;
  agentName: string;
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

  async function handleFileSelect(files: FileList | null) {
    if (!files) return;
    const additions: ChatAttachment[] = [];
    for (const file of Array.from(files)) {
      if (file.size > ATTACHMENT_MAX_BYTES) {
        alert(`文件 ${file.name} 超过 10MB 限制`);
        continue;
      }
      const data = await fileToBase64(file);
      additions.push({
        type: file.type.startsWith('image/') ? 'image' : 'file',
        name: file.name,
        data,
        mime_type: file.type,
      });
    }
    if (additions.length) onAttachmentsChange([...attachments, ...additions]);
  }

  function removeAttachment(index: number) {
    onAttachmentsChange(attachments.filter((_, i) => i !== index));
  }

  async function handlePaste(e: React.ClipboardEvent<HTMLTextAreaElement>) {
    const items = e.clipboardData?.items;
    if (!items) return;
    const additions: ChatAttachment[] = [];
    for (const item of Array.from(items)) {
      if (!item.type.startsWith('image/')) continue;
      const file = item.getAsFile();
      if (!file) continue;
      e.preventDefault();
      if (file.size > ATTACHMENT_MAX_BYTES) {
        alert('粘贴的图片超过 10MB 限制');
        continue;
      }
      const data = await fileToBase64(file);
      additions.push({
        type: 'image',
        name: `pasted-image-${Date.now()}.${file.type.split('/')[1] ?? 'png'}`,
        data,
        mime_type: file.type,
      });
    }
    if (additions.length) onAttachmentsChange([...attachments, ...additions]);
  }

  // Track IME composition so Enter does not submit while the user is still
  // picking pinyin candidates / Japanese kana / Korean jamo etc. We watch
  // both React's composition events (state-based) AND the native isComposing
  // flag / keyCode 229 (event-based) because some IMEs fire keydown for
  // candidate selection BEFORE the React state has flushed.
  const composingRef = useRef(false);
  return (
    <Stack gap={4} px="xs">
      {attachments.length > 0 ? (
        <Group gap="xs" wrap="wrap">
          {attachments.map((att, idx) => (
            <AttachmentPreview key={idx} attachment={att} onRemove={() => removeAttachment(idx)} />
          ))}
        </Group>
      ) : null}

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
      <Box
        p="xs"
        style={{
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 12,
          background: 'light-dark(#ffffff, var(--mantine-color-dark-7))',
        }}
      >
        <Textarea
          ref={ref}
          placeholder={composerPlaceholder(agentName, commands)}
          autosize
          minRows={1}
          maxRows={6}
          value={value}
          variant="unstyled"
          styles={{ input: { padding: '4px 6px' } }}
          onChange={(e) => onChange(e.currentTarget.value)}
          onPaste={(e) => void handlePaste(e)}
          onCompositionStart={() => {
            composingRef.current = true;
          }}
          onCompositionEnd={() => {
            composingRef.current = false;
          }}
          onKeyDown={(e) => {
            // Skip Enter handling while an IME is composing — that Enter
            // belongs to the candidate picker, not us.
            if (composingRef.current || e.nativeEvent.isComposing || e.keyCode === 229) {
              return;
            }
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              void onSubmit();
            }
          }}
        />
        <Group gap="xs" mt={6} wrap="nowrap" align="center">
          {/* Left: upload + model + context */}
          <ActionIcon component="label" variant="subtle" size="md" aria-label="Attach files">
            <IconUpload size={16} />
            <input
              type="file"
              hidden
              multiple
              accept="image/*,.pdf,.doc,.docx,.txt,.md,.json"
              onChange={(e) => void handleFileSelect(e.target.files)}
            />
          </ActionIcon>
          {modelOption ? <ModelPicker option={modelOption} onChange={onModelChange} /> : null}
          {usage ? <UsagePill usage={usage} /> : null}
          <Box style={{ flex: 1 }} />
          {/* Right: send / stop morph. Idle → send; thinking or
              awaiting_permission → stop (cancels the in-flight prompt
              and clears the awaiting_permission banner). */}
          {status === 'thinking' || status === 'awaiting_permission' ? (
            <Tooltip label="停止">
              <ActionIcon
                size="lg"
                radius="xl"
                color="red"
                variant="filled"
                onClick={onCancel}
                aria-label="Stop"
              >
                <IconPlayerStop size={16} />
              </ActionIcon>
            </Tooltip>
          ) : (
            <Tooltip label="发送 (Enter)">
              <ActionIcon
                size="lg"
                radius="xl"
                color="brand"
                variant="filled"
                onClick={() => void onSubmit()}
                loading={busy}
                disabled={!value.trim() && attachments.length === 0}
                aria-label="Send"
              >
                <IconSend size={16} />
              </ActionIcon>
            </Tooltip>
          )}
        </Group>
      </Box>
    </Stack>
  );
}

function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result;
      if (typeof result !== 'string') {
        reject(new Error('failed to read file'));
        return;
      }
      // Strip the `data:<mime>;base64,` prefix; the wire format expects
      // raw base64.
      const comma = result.indexOf(',');
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.onerror = () => reject(reader.error ?? new Error('FileReader error'));
    reader.readAsDataURL(file);
  });
}

function AttachmentPreview({
  attachment,
  onRemove,
}: {
  attachment: ChatAttachment;
  onRemove: () => void;
}) {
  const isImage = attachment.type === 'image';
  const dataUrl =
    isImage && attachment.data
      ? `data:${attachment.mime_type ?? 'image/png'};base64,${attachment.data}`
      : null;
  return (
    <Paper withBorder radius="md" p={4} pr="xs">
      <Group gap="xs" wrap="nowrap">
        {dataUrl ? (
          <Box
            component="img"
            src={dataUrl}
            alt={attachment.name}
            style={{ width: 32, height: 32, objectFit: 'cover', borderRadius: 4 }}
          />
        ) : (
          <ThemeIcon size="md" variant="light">
            <IconFileText size={14} />
          </ThemeIcon>
        )}
        <Stack gap={0} style={{ minWidth: 0, maxWidth: 140 }}>
          <Text size="xs" truncate>
            {attachment.name}
          </Text>
          {attachment.mime_type ? (
            <Text size="xs" c="dimmed" truncate>
              {attachment.mime_type}
            </Text>
          ) : null}
        </Stack>
        <ActionIcon size="sm" variant="subtle" color="gray" onClick={onRemove}>
          <IconX size={12} />
        </ActionIcon>
      </Group>
    </Paper>
  );
}

function attachmentDataUrl(att: ChatAttachment): string | null {
  return att.type === 'image' && att.data
    ? `data:${att.mime_type ?? 'image/png'};base64,${att.data}`
    : null;
}

// Approximate decoded size from the base64 payload length.
function attachmentSize(att: ChatAttachment): string | null {
  if (!att.data) return null;
  const n = Math.floor((att.data.length * 3) / 4);
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

// Read-only attachment strip inside a transcript bubble: image attachments
// render as click-to-enlarge thumbnails, everything else as a name+size
// chip. Visually tuned for both the indigo user bubble and plain cards.
function MessageAttachments({
  attachments,
  onBrand,
}: {
  attachments: ChatAttachment[];
  onBrand: boolean;
}) {
  const [preview, setPreview] = useState<{ url: string; name: string } | null>(null);
  const chipStyle: React.CSSProperties = onBrand
    ? {
        background: 'rgba(255, 255, 255, 0.12)',
        border: '1px solid rgba(255, 255, 255, 0.28)',
        color: '#ffffff',
      }
    : {
        background: 'light-dark(#f5f6f8, var(--mantine-color-dark-5))',
        border: '1px solid light-dark(#e5e7eb, var(--mantine-color-dark-4))',
        color: 'var(--mantine-color-text)',
      };
  return (
    <Group gap={6} wrap="wrap">
      {attachments.map((att, idx) => {
        const url = attachmentDataUrl(att);
        if (url) {
          return (
            <UnstyledButton
              key={idx}
              onClick={() => setPreview({ url, name: att.name })}
              aria-label={`查看图片 ${att.name}`}
              title={att.name}
            >
              <Box
                component="img"
                src={url}
                alt={att.name}
                style={{
                  display: 'block',
                  maxWidth: 200,
                  maxHeight: 140,
                  borderRadius: 8,
                  border: onBrand
                    ? '1px solid rgba(255, 255, 255, 0.35)'
                    : '1px solid light-dark(#e5e7eb, var(--mantine-color-dark-4))',
                  cursor: 'zoom-in',
                }}
              />
            </UnstyledButton>
          );
        }
        const size = attachmentSize(att);
        return (
          <Group
            key={idx}
            gap={6}
            wrap="nowrap"
            px={8}
            py={4}
            style={{ ...chipStyle, borderRadius: 8, maxWidth: 240 }}
            title={att.name}
          >
            <IconFileText size={14} style={{ flexShrink: 0, opacity: 0.85 }} />
            <Text size="xs" truncate style={{ color: 'inherit' }}>
              {att.name}
            </Text>
            {size ? (
              <Text size="xs" style={{ color: 'inherit', opacity: 0.7, whiteSpace: 'nowrap' }}>
                {size}
              </Text>
            ) : null}
          </Group>
        );
      })}
      <Modal
        opened={preview !== null}
        onClose={() => setPreview(null)}
        title={preview?.name}
        size="auto"
        centered
      >
        {preview ? (
          <Box
            component="img"
            src={preview.url}
            alt={preview.name}
            style={{ display: 'block', maxWidth: '85vw', maxHeight: '75vh' }}
          />
        ) : null}
      </Modal>
    </Group>
  );
}

// Transcript components are memoized: streaming chunks land several times a
// second and each one replaces ONLY the live message (or one tool call) in
// the store — every other transcript item keeps its object identity, so memo
// turns a chunk's render cost from "re-render + re-parse the entire
// transcript" into "re-render the one streaming bubble". Long agent answers
// used to allocate fast enough to OOM the tab (guest page especially).
export const MessageBubble = memo(function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === 'user';
  const isThought = message.role === 'agent_thought';
  const isSystem = message.role === 'system';
  const text = message.text || (message.streaming ? '…' : '');

  // Defensive: some Devin models stream `content == "None"` when the
  // assistant turn ends with only tool calls (see closeLiveMessage in
  // store/chat.ts). The store drops these on close, but suppress here
  // too so a still-streaming bubble doesn't briefly flash "None".
  if (message.role === 'agent' && message.text === 'None') return null;

  if (isThought) return <ThoughtBubble message={message} />;

  // CodeWhale-style bubbles: indigo on user side, hairline white card on
  // agent side. System messages get a flatter, more muted look so they
  // recede into the timeline.
  const userStyle: React.CSSProperties = {
    background: 'var(--mantine-color-brand-8)',
    color: '#ffffff',
    border: '1px solid var(--mantine-color-brand-8)',
  };
  const agentStyle: React.CSSProperties = {
    background: 'light-dark(#ffffff, var(--mantine-color-dark-6))',
    color: 'var(--mantine-color-text)',
    border: '1px solid light-dark(#e5e7eb, var(--mantine-color-dark-4))',
  };
  const systemStyle: React.CSSProperties = {
    background: 'light-dark(#f5f6f8, var(--mantine-color-dark-5))',
    color: 'var(--mantine-color-dimmed)',
    border: '1px solid light-dark(#e5e7eb, var(--mantine-color-dark-4))',
  };

  const attachments = !isSystem && message.attachments?.length ? message.attachments : null;
  // Attachment-only prompts have no text — skip the empty markdown body.
  const showBody = Boolean(text) || !attachments;

  return (
    <Group justify={isUser ? 'flex-end' : 'flex-start'} align="flex-start">
      <Card
        className="cw-rise-in"
        radius="md"
        padding="sm"
        style={isUser ? userStyle : isSystem ? systemStyle : agentStyle}
        miw={isUser ? 100 : 200}
        maw="80%"
      >
        <Stack gap="xs">
          {attachments ? <MessageAttachments attachments={attachments} onBrand={isUser} /> : null}
          {isSystem ? (
            <Text size="xs" c="dimmed">
              {text}
            </Text>
          ) : !showBody ? null : message.streaming ? (
            <StreamingMarkdownBody text={text} c={isUser ? 'white' : undefined} />
          ) : (
            <MarkdownBody text={text} c={isUser ? 'white' : undefined} />
          )}
        </Stack>
      </Card>
    </Group>
  );
});

const ThoughtBubble = memo(function ThoughtBubble({ message }: { message: ChatMessage }) {
  const [expanded, setExpanded] = useState(false);
  const text = message.text || (message.streaming ? '…' : '');
  return (
    <Group justify="flex-start" align="flex-start">
      <Card
        className="cw-rise-in"
        radius="md"
        padding="sm"
        miw={220}
        maw="80%"
        style={{
          background: 'light-dark(#fafafa, var(--mantine-color-dark-7))',
          border: '1px dashed light-dark(#d1d6de, var(--mantine-color-dark-4))',
          color: 'var(--mantine-color-text)',
        }}
      >
        <Group gap="xs" justify="space-between" wrap="nowrap">
          <Group gap="xs" wrap="nowrap">
            <ThemeIcon size="sm" variant="light" color="gray">
              <IconBrain size={12} />
            </ThemeIcon>
            <Text size="xs" c="dimmed" fw={600}>
              思考过程
            </Text>
          </Group>
          <Button
            size="compact-xs"
            variant="subtle"
            color="gray"
            leftSection={expanded ? <IconChevronDown size={12} /> : <IconChevronRight size={12} />}
            onClick={() => setExpanded((x) => !x)}
          >
            {expanded ? '收起' : '展开'}
          </Button>
        </Group>
        <LazyCollapse opened={expanded}>
          {() => (
            <Box mt="xs" style={{ fontStyle: 'italic' }}>
              {message.streaming ? (
                <StreamingMarkdownBody text={text} size="xs" c="dimmed" />
              ) : (
                <MarkdownBody text={text} size="xs" c="dimmed" />
              )}
            </Box>
          )}
        </LazyCollapse>
      </Card>
    </Group>
  );
});

const TOOL_STATUS_COLOR: Record<string, string> = {
  pending: 'gray',
  in_progress: 'yellow',
  completed: 'green',
  failed: 'red',
};

/**
 * Animated Collapse that defers mounting its (potentially expensive) children
 * until the first open, then keeps them mounted so the close animation has
 * content to shrink over. Used for tool-call detail bodies, which stringify
 * raw JSON payloads we don't want to render for every collapsed row.
 */
function LazyCollapse({ opened, children }: { opened: boolean; children: () => ReactNode }) {
  const everOpened = useRef(false);
  if (opened) everOpened.current = true;
  return <Collapse in={opened}>{everOpened.current ? children() : null}</Collapse>;
}

export const ToolCallCard = memo(function ToolCallCard({ call }: { call: ChatToolCall }) {
  const [expanded, setExpanded] = useState(false);
  const title = call.title ?? humanizeToolFromInput(call) ?? '(tool)';
  const status = call.status ?? 'pending';
  const hasDetails =
    hasMeaningfulPayload(call.rawInput) ||
    hasMeaningfulPayload(call.rawOutput) ||
    (call.content && call.content.length > 0);
  // Hairline white card to blend into the timeline; terminal-style output
  // lives inside a dark mono block so the "this is shell output" cue is
  // preserved without dominating the whole card visually.
  return (
    <Card
      className="cw-rise-in"
      padding="sm"
      radius="md"
      maw="92%"
      style={{
        alignSelf: 'flex-start',
        background: 'light-dark(#ffffff, var(--mantine-color-dark-6))',
        border: '1px solid light-dark(#e5e7eb, var(--mantine-color-dark-4))',
        color: 'var(--mantine-color-text)',
      }}
    >
      <Group gap="xs" justify="space-between" wrap="nowrap">
        <Group gap="xs" wrap="nowrap" style={{ minWidth: 0, flex: 1 }}>
          <ThemeIcon
            size="sm"
            variant="light"
            color={TOOL_STATUS_COLOR[status] ?? 'gray'}
            style={{ transition: 'background-color 200ms ease, color 200ms ease' }}
          >
            {iconForKind(call.kind)}
          </ThemeIcon>
          <Text size="sm" fw={600} truncate>
            {title}
          </Text>
          <Badge size="xs" color={TOOL_STATUS_COLOR[status] ?? 'gray'} variant="light">
            {status}
          </Badge>
        </Group>
        {hasDetails ? (
          <Tooltip label={expanded ? '收起详情' : '展开详情'}>
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
        <ToolCallLocations locations={call.locations} mt={6} />
      ) : null}
      <LazyCollapse opened={expanded}>
        {() => (
          <Box mt="sm">
            <ToolCallDetails call={call} />
          </Box>
        )}
      </LazyCollapse>
    </Card>
  );
});

function ToolCallLocations({
  locations,
  mt,
}: {
  locations: NonNullable<ChatToolCall['locations']>;
  mt?: number;
}) {
  return (
    <Group gap={4} mt={mt} wrap="wrap">
      {locations.map((loc, i) => (
        <Badge
          key={i}
          size="xs"
          variant="light"
          color="brand"
          leftSection={<IconFileText size={10} />}
        >
          {shortenPath(loc.path)}
          {loc.line ? `:${loc.line}` : ''}
        </Badge>
      ))}
    </Group>
  );
}

/** Expanded body shared by ToolCallCard and ToolCallRow. */
function ToolCallDetails({
  call,
  withLocations = false,
}: {
  call: ChatToolCall;
  withLocations?: boolean;
}) {
  return (
    <Stack gap="xs">
      {withLocations && call.locations && call.locations.length > 0 ? (
        <ToolCallLocations locations={call.locations} />
      ) : null}
      {call.content?.map((c, i) => (
        <ToolContentBlockView key={i} block={c} />
      ))}
      {hasMeaningfulPayload(call.rawInput) ? (
        <Box>
          <Text size="xs" c="dimmed" mb={4}>
            input
          </Text>
          <RawJSONBlock value={safeStringify(call.rawInput)} />
        </Box>
      ) : call.rawInput != null ? (
        // cursor-agent sends `rawInput: {}` and never fills it — say so
        // instead of rendering a `{}` that looks like a UI bug.
        <Text size="xs" c="dimmed" fs="italic">
          该 agent 未上报调用参数
        </Text>
      ) : null}
      {hasMeaningfulPayload(call.rawOutput) ? (
        <Box>
          <Text size="xs" c="dimmed" mb={4}>
            output
          </Text>
          <RawJSONBlock value={safeStringify(call.rawOutput)} />
        </Box>
      ) : null}
    </Stack>
  );
}

/**
 * One collapsible card for a run of >= TOOL_GROUP_THRESHOLD consecutive tool
 * calls. Collapsed by default; while the run is still executing it surfaces
 * just the in-flight call(s) as live rows, and once everything finishes it
 * shrinks to the summary header. Expanding shows every call as a compact row.
 */
export const ToolCallGroupCard = memo(
  function ToolCallGroupCard({ calls }: { calls: ChatToolCall[] }) {
    return <ToolCallGroupBody calls={calls} />;
  },
  // groupTranscript rebuilds the calls array on every store change, but the
  // ChatToolCall objects inside keep their identity unless that call really
  // changed — compare element-wise so unrelated streaming chunks bail out.
  (prev, next) =>
    prev.calls.length === next.calls.length && prev.calls.every((c, i) => c === next.calls[i]),
);

function ToolCallGroupBody({ calls }: { calls: ChatToolCall[] }) {
  const [expanded, setExpanded] = useState(false);
  const active = calls.filter(isActiveToolCall);
  const failed = calls.filter((c) => c.status === 'failed').length;
  const done = calls.length - active.length;
  const running = active.length > 0;
  const color = running ? 'yellow' : failed > 0 ? 'red' : 'green';
  // Rows render through nested Collapses so completed live rows shrink away
  // smoothly while the run executes, and the whole block closes in one motion
  // when the run finishes (or the user toggles the header).
  const visibleIds = new Set((expanded ? calls : running ? active : []).map((c) => c.id));
  return (
    <Card
      className="cw-rise-in"
      padding="sm"
      radius="md"
      maw="92%"
      style={{
        alignSelf: 'flex-start',
        background: 'light-dark(#ffffff, var(--mantine-color-dark-6))',
        border: '1px solid light-dark(#e5e7eb, var(--mantine-color-dark-4))',
        color: 'var(--mantine-color-text)',
      }}
    >
      <UnstyledButton
        className="cw-row-hover"
        onClick={() => setExpanded((x) => !x)}
        w="100%"
        px={6}
        py={3}
        style={{ borderRadius: 6 }}
        aria-expanded={expanded}
      >
        <Group gap="xs" justify="space-between" wrap="nowrap">
          <Group gap="xs" wrap="nowrap" style={{ minWidth: 0, flex: 1 }}>
            <ThemeIcon
              size="sm"
              variant="light"
              color={color}
              style={{ transition: 'background-color 200ms ease, color 200ms ease' }}
            >
              <IconTool size={12} />
            </ThemeIcon>
            <Text size="sm" fw={600}>
              {calls.length} 个工具调用
            </Text>
            {running ? <Loader size={12} color="yellow" /> : null}
            <Badge size="xs" variant="light" color={color}>
              {running ? `${done}/${calls.length} 完成` : failed > 0 ? `${failed} 失败` : '已完成'}
            </Badge>
          </Group>
          <IconChevronRight
            size={14}
            style={{
              transform: expanded ? 'rotate(90deg)' : undefined,
              transition: 'transform 120ms',
              flexShrink: 0,
            }}
          />
        </Group>
      </UnstyledButton>
      <Collapse in={visibleIds.size > 0}>
        <Box mt={8}>
          {calls.map((call) => (
            <Collapse key={call.id} in={visibleIds.has(call.id)}>
              <ToolCallRow call={call} />
            </Collapse>
          ))}
        </Box>
      </Collapse>
    </Card>
  );
}

/** Compact single-line rendering of a tool call inside ToolCallGroupCard. */
const ToolCallRow = memo(function ToolCallRow({ call }: { call: ChatToolCall }) {
  const [expanded, setExpanded] = useState(false);
  const title = call.title ?? humanizeToolFromInput(call) ?? '(tool)';
  const status = call.status ?? 'pending';
  const hasDetails =
    hasMeaningfulPayload(call.rawInput) ||
    hasMeaningfulPayload(call.rawOutput) ||
    (call.content && call.content.length > 0) ||
    (call.locations && call.locations.length > 0);
  return (
    <Box className="cw-rise-in">
      <UnstyledButton
        className={hasDetails ? 'cw-row-hover' : undefined}
        onClick={hasDetails ? () => setExpanded((x) => !x) : undefined}
        w="100%"
        px={6}
        py={3}
        style={{ borderRadius: 4, cursor: hasDetails ? 'pointer' : 'default' }}
        aria-expanded={hasDetails ? expanded : undefined}
      >
        <Group gap="xs" wrap="nowrap">
          <ThemeIcon
            size="xs"
            variant="light"
            color={TOOL_STATUS_COLOR[status] ?? 'gray'}
            style={{ transition: 'background-color 200ms ease, color 200ms ease' }}
          >
            {iconForKind(call.kind)}
          </ThemeIcon>
          <Text size="xs" truncate style={{ flex: 1, minWidth: 0 }}>
            {title}
          </Text>
          {isActiveToolCall(call) ? <Loader size={10} color="yellow" /> : null}
          {status === 'failed' ? (
            <Badge size="xs" color="red" variant="light">
              failed
            </Badge>
          ) : null}
          {hasDetails ? (
            <IconChevronRight
              size={12}
              style={{
                transform: expanded ? 'rotate(90deg)' : undefined,
                transition: 'transform 120ms',
                flexShrink: 0,
              }}
            />
          ) : null}
        </Group>
      </UnstyledButton>
      <LazyCollapse opened={expanded}>
        {() => (
          <Box pl={28} pr={6} pb={4} pt={2}>
            <ToolCallDetails call={call} withLocations />
          </Box>
        )}
      </LazyCollapse>
    </Box>
  );
});

/**
 * Compact mono code block used for tool-call input/output JSON. Light theme
 * gets a near-white surface with a hairline; dark theme inherits Mantine's
 * surface.
 */
function RawJSONBlock({ value }: { value: string }) {
  return (
    <Box
      style={{
        background: 'light-dark(#f5f6f8, var(--mantine-color-dark-7))',
        border: '1px solid light-dark(#e5e7eb, var(--mantine-color-dark-4))',
        borderRadius: 6,
        padding: '8px 10px',
        fontFamily:
          '"JetBrains Mono", "IBM Plex Mono", ui-monospace, SFMono-Regular, Menlo, monospace',
        fontSize: 11,
        whiteSpace: 'pre-wrap',
        wordBreak: 'break-word',
        color: 'var(--mantine-color-text)',
        maxHeight: 280,
        overflow: 'auto',
      }}
    >
      {value}
    </Box>
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

/**
 * Renders an opaque/near-black mono block — used for shell-style output
 * inside ToolCallCard. Matches the "cw-code-block" CodeWhale install
 * snippet used on PairScreen, so the visual language stays consistent.
 */
function TerminalBlock({ children }: { children: React.ReactNode }) {
  return (
    <Box
      style={{
        background: '#0e0e10',
        color: '#e7ecf3',
        border: '1px solid #1f2330',
        borderRadius: 6,
        padding: '10px 12px',
        fontFamily:
          '"JetBrains Mono", "IBM Plex Mono", ui-monospace, SFMono-Regular, Menlo, monospace',
        fontSize: 12,
        lineHeight: 1.55,
        whiteSpace: 'pre-wrap',
        wordBreak: 'break-word',
        maxHeight: 360,
        overflow: 'auto',
      }}
    >
      {children}
    </Box>
  );
}

function ToolContentBlockView({ block }: { block: ToolCallContentBlock }) {
  if (block.type === 'content') {
    const inner = (block as { content?: { type?: string; text?: string } }).content;
    if (inner?.type === 'text' && typeof inner.text === 'string') {
      return <TerminalBlock>{inner.text}</TerminalBlock>;
    }
  }
  if (block.type === 'diff') {
    const d = block as { path: string; oldText?: string | null; newText: string };
    return (
      <Box>
        <Text size="xs" c="dimmed" mb={4}>
          diff · {d.path}
        </Text>
        <TerminalBlock>{d.newText}</TerminalBlock>
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
  return <TerminalBlock>{safeStringify(block)}</TerminalBlock>;
}

/**
 * ACP option kinds we render with distinct visual weight. Unknown kinds fall
 * back to a neutral grey button so future agents (or non-Devin agents) keep
 * working without UI changes.
 */
type AcpOptionKind = 'allow_once' | 'allow_always' | 'reject_once' | 'reject_always' | string;

interface AcpOption {
  optionId: string;
  name: string;
  kind?: AcpOptionKind;
}

function optionColor(kind?: string): string {
  if (kind === 'allow_once') return 'green';
  if (kind === 'allow_always') return 'teal';
  if (kind === 'reject_once' || kind === 'reject_always') return 'red';
  return 'gray';
}

function optionVariant(kind?: string): 'filled' | 'light' | 'default' {
  if (kind === 'allow_once') return 'filled';
  if (kind?.startsWith('reject')) return 'light';
  return 'default';
}

function optionIcon(kind?: string) {
  if (kind?.startsWith('allow')) return <IconCheck size={12} />;
  if (kind?.startsWith('reject')) return <IconX size={12} />;
  return null;
}

function parseAcpPermissionParams(params: Record<string, unknown>): {
  title: string | null;
  command: string | null;
  description: string | null;
  options: AcpOption[];
} {
  const toolCall = (params.toolCall ?? {}) as Record<string, unknown>;
  const rawInput = (toolCall.rawInput ?? {}) as Record<string, unknown>;
  const title = typeof toolCall.title === 'string' ? (toolCall.title as string) : null;
  const command = typeof rawInput.command === 'string' ? (rawInput.command as string) : null;
  const description =
    typeof rawInput.description === 'string' ? (rawInput.description as string) : null;
  const rawOptions = Array.isArray(params.options) ? (params.options as unknown[]) : [];
  const options: AcpOption[] = [];
  for (const o of rawOptions) {
    if (!o || typeof o !== 'object') continue;
    const obj = o as Record<string, unknown>;
    if (typeof obj.optionId !== 'string' || typeof obj.name !== 'string') continue;
    const opt: AcpOption = { optionId: obj.optionId, name: obj.name };
    if (typeof obj.kind === 'string') opt.kind = obj.kind;
    options.push(opt);
  }
  return { title, command, description, options };
}

function PermissionCard({
  params,
  onResolve,
}: {
  params: Record<string, unknown>;
  /** Decision body is forwarded verbatim to the agent via ACP. */
  onResolve: (decision: unknown) => void;
}) {
  const { title, command, description, options } = parseAcpPermissionParams(params);

  // Fallback options when the agent did not send any — same shape as ACP so
  // the response stays well-formed. Devin always provides options, but other
  // ACP agents might not.
  const renderedOptions: AcpOption[] =
    options.length > 0
      ? options
      : [
          { optionId: 'allow_once', name: '允许', kind: 'allow_once' },
          { optionId: 'reject_once', name: '拒绝', kind: 'reject_once' },
        ];

  function selectOption(opt: AcpOption) {
    onResolve({ outcome: { outcome: 'selected', optionId: opt.optionId } });
  }

  function cancel() {
    onResolve({ outcome: { outcome: 'cancelled' } });
  }

  return (
    <Card
      padding="sm"
      radius="md"
      style={{
        background: 'light-dark(#fffbeb, var(--mantine-color-yellow-9))',
        border: '1px solid light-dark(#fcd34d, var(--mantine-color-yellow-7))',
        color: 'var(--mantine-color-text)',
      }}
    >
      <Group gap="xs" wrap="nowrap">
        <IconShieldCheck size={16} color="var(--mantine-color-yellow-8)" />
        <Text size="sm" fw={700}>
          需要授权
        </Text>
        {title ? (
          <Text size="xs" c="dimmed" truncate>
            · {title}
          </Text>
        ) : null}
      </Group>
      {command ? (
        <Box mt="xs">
          <Text size="xs" c="dimmed" mb={4}>
            command
          </Text>
          <RawJSONBlock value={command} />
        </Box>
      ) : null}
      {!command && description ? (
        <Text size="xs" mt={6} c="dimmed">
          {description}
        </Text>
      ) : null}
      {!command && !description ? (
        <Box mt="xs">
          <RawJSONBlock value={safeStringify(params)} />
        </Box>
      ) : null}
      <Group gap="xs" mt="sm" wrap="wrap">
        {renderedOptions.map((opt) => (
          <Button
            key={opt.optionId}
            size="xs"
            color={optionColor(opt.kind)}
            variant={optionVariant(opt.kind)}
            leftSection={optionIcon(opt.kind)}
            onClick={() => selectOption(opt)}
          >
            {opt.name}
          </Button>
        ))}
        {renderedOptions.length > 0 ? (
          <Button size="xs" variant="subtle" color="gray" onClick={cancel}>
            取消
          </Button>
        ) : null}
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
