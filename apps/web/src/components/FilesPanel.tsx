import {
  ActionIcon,
  Alert,
  Badge,
  Box,
  Button,
  Center,
  Group,
  Menu,
  Modal,
  ScrollArea,
  SegmentedControl,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import {
  IconChevronDown,
  IconChevronRight,
  IconCode,
  IconDots,
  IconDownload,
  IconEye,
  IconFile,
  IconFileText,
  IconFolder,
  IconLink,
  IconPencil,
  IconRefresh,
  IconTrash,
  IconUpload,
} from '@tabler/icons-react';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';

import type { FilesEntry, FilesReadResponse } from '@coding-anywhere/protocol';

import { useConnection } from '../store/connection.js';
import { joinPath, useFiles } from '../store/files.js';

import { CodeViewer } from './CodeViewer.js';
import { MarkdownBody } from './Markdown.js';

// Actions the recursive tree needs, provided once at the panel root so the
// nodes don't have to thread a growing list of props through the recursion.
interface TreeCtx {
  openDirs: Set<string>;
  toggleDir: (path: string) => void;
  activePath: string | null;
  select: (path: string) => void;
  dragPath: string | null;
  setDragPath: (path: string | null) => void;
  onUpload: (dirPath: string) => void;
  onDownload: (entry: FilesEntry) => void;
  onRename: (entry: FilesEntry) => void;
  onDelete: (entry: FilesEntry) => void;
  onMove: (fromPath: string, toDir: string) => void;
}

const TreeContext = createContext<TreeCtx | null>(null);
function useTree(): TreeCtx {
  const ctx = useContext(TreeContext);
  if (!ctx) throw new Error('TreeContext missing');
  return ctx;
}

export function FilesPanel() {
  const status = useConnection((s) => s.status);
  const workspaces = useFiles((s) => s.workspaces);
  const workspaceId = useFiles((s) => s.workspaceId);
  const loadWorkspaces = useFiles((s) => s.loadWorkspaces);
  const refresh = useFiles((s) => s.refresh);
  const activePath = useFiles((s) => s.activePath);
  const setActivePath = useFiles((s) => s.setActivePath);
  const uploadFile = useFiles((s) => s.uploadFile);
  const downloadFile = useFiles((s) => s.downloadFile);
  const deleteEntry = useFiles((s) => s.deleteEntry);
  const moveEntry = useFiles((s) => s.moveEntry);

  const [error, setError] = useState<string | null>(null);
  const [openDirs, setOpenDirs] = useState<Set<string>>(() => new Set(['']));
  const [dragPath, setDragPath] = useState<string | null>(null);

  // Modal + upload state.
  const [renameTarget, setRenameTarget] = useState<FilesEntry | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<FilesEntry | null>(null);
  const [busy, setBusy] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const uploadDirRef = useRef<string>('');

  useEffect(() => {
    if (status !== 'connected') return;
    loadWorkspaces().catch((e: Error) => setError(e.message));
  }, [status, loadWorkspaces]);

  const ws = workspaces.find((w) => w.id === workspaceId);

  const toggleDir = useCallback((path: string) => {
    setOpenDirs((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }, []);

  const openDir = useCallback((path: string) => {
    setOpenDirs((prev) => (prev.has(path) ? prev : new Set(prev).add(path)));
  }, []);

  // --- file operations ---

  const onUpload = useCallback((dirPath: string) => {
    uploadDirRef.current = dirPath;
    fileInputRef.current?.click();
  }, []);

  const handleFilesPicked = useCallback(
    async (fileList: FileList | null) => {
      const dir = uploadDirRef.current;
      const files = fileList ? Array.from(fileList) : [];
      const first = files[0];
      if (!first) return;
      try {
        for (const f of files) await uploadFile(dir, f);
        refresh();
        openDir(dir);
        notifications.show({
          color: 'teal',
          message: files.length === 1 ? `已上传 ${first.name}` : `已上传 ${files.length} 个文件`,
        });
      } catch (e) {
        notifications.show({ color: 'red', title: '上传失败', message: (e as Error).message });
      } finally {
        // Reset so picking the same file again still fires onChange.
        if (fileInputRef.current) fileInputRef.current.value = '';
      }
    },
    [uploadFile, refresh, openDir],
  );

  const onDownload = useCallback(
    async (entry: FilesEntry) => {
      try {
        const { bytes, contentType } = await downloadFile(entry.path);
        triggerDownload(entry.name, bytes, contentType);
      } catch (e) {
        notifications.show({ color: 'red', title: '下载失败', message: (e as Error).message });
      }
    },
    [downloadFile],
  );

  const onRename = useCallback((entry: FilesEntry) => {
    setRenameTarget(entry);
    setRenameValue(entry.name);
  }, []);

  const submitRename = useCallback(async () => {
    if (!renameTarget) return;
    const name = renameValue.trim();
    if (!name || name === renameTarget.name) {
      setRenameTarget(null);
      return;
    }
    const to = joinPath(dirOf(renameTarget.path), name);
    setBusy(true);
    try {
      await moveEntry(renameTarget.path, to);
      // Keep the active file open under its new path; if a directory was
      // renamed out from under the active file, drop the now-stale selection.
      if (activePath === renameTarget.path) setActivePath(to);
      else if (activePath && activePath.startsWith(renameTarget.path + '/')) setActivePath(null);
      refresh();
      setRenameTarget(null);
    } catch (e) {
      notifications.show({ color: 'red', title: '重命名失败', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  }, [renameTarget, renameValue, moveEntry, activePath, setActivePath, refresh]);

  const onDelete = useCallback((entry: FilesEntry) => setDeleteTarget(entry), []);

  const confirmDelete = useCallback(async () => {
    if (!deleteTarget) return;
    setBusy(true);
    try {
      await deleteEntry(deleteTarget.path);
      if (
        activePath === deleteTarget.path ||
        (activePath && activePath.startsWith(deleteTarget.path + '/'))
      ) {
        setActivePath(null);
      }
      refresh();
      setDeleteTarget(null);
    } catch (e) {
      notifications.show({ color: 'red', title: '删除失败', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  }, [deleteTarget, deleteEntry, activePath, setActivePath, refresh]);

  const onMove = useCallback(
    async (fromPath: string, toDir: string) => {
      if (!canDrop(fromPath, toDir)) return;
      const to = joinPath(toDir, baseName(fromPath));
      try {
        await moveEntry(fromPath, to);
        if (activePath === fromPath) setActivePath(to);
        else if (activePath && activePath.startsWith(fromPath + '/')) setActivePath(null);
        refresh();
        openDir(toDir);
      } catch (e) {
        notifications.show({ color: 'red', title: '移动失败', message: (e as Error).message });
      }
    },
    [moveEntry, activePath, setActivePath, refresh, openDir],
  );

  const reload = useCallback(() => {
    setOpenDirs(new Set(['']));
    setActivePath(null);
    refresh();
    loadWorkspaces().catch((e: Error) => setError(e.message));
  }, [setActivePath, refresh, loadWorkspaces]);

  const ctx: TreeCtx = {
    openDirs,
    toggleDir,
    activePath,
    select: setActivePath,
    dragPath,
    setDragPath,
    onUpload,
    onDownload,
    onRename,
    onDelete,
    onMove,
  };

  return (
    <TreeContext.Provider value={ctx}>
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
            <Stack gap={0} style={{ minWidth: 0 }}>
              <Text fw={600}>{ws?.name ?? 'Workspace'}</Text>
              {ws ? (
                <Tooltip label={ws.root}>
                  <Text size="xs" c="dimmed" truncate>
                    {ws.root}
                  </Text>
                </Tooltip>
              ) : null}
            </Stack>
            <Group gap={4}>
              {ws ? (
                <Tooltip label="上传到根目录">
                  <ActionIcon variant="subtle" onClick={() => onUpload('')}>
                    <IconUpload size={14} />
                  </ActionIcon>
                </Tooltip>
              ) : null}
              <Tooltip label="Reload">
                <ActionIcon variant="subtle" onClick={reload}>
                  <IconRefresh size={14} />
                </ActionIcon>
              </Tooltip>
            </Group>
          </Group>
          {error ? (
            <Alert color="red" variant="light" radius="md">
              {error}
            </Alert>
          ) : null}
          {status !== 'connected' ? (
            <Text size="sm" c="dimmed">
              Waiting for relay connection…
            </Text>
          ) : !ws ? (
            <Text size="sm" c="dimmed">
              Loading workspace…
            </Text>
          ) : (
            // Only mount the tree once the workspace id is known: the listing
            // cache is keyed by workspace id, so rendering earlier would fetch
            // (and cache) under a placeholder key and leave the tree blank.
            <ScrollArea.Autosize mah="calc(100vh - 200px)">
              <DirNode path="" label={ws.name} depth={0} />
            </ScrollArea.Autosize>
          )}
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
          {activePath ? <FileViewer path={activePath} /> : <EmptyViewer />}
        </Box>
      </Group>

      {/* Hidden picker shared by every "上传到此处" menu item. */}
      <input
        ref={fileInputRef}
        type="file"
        hidden
        multiple
        onChange={(e) => void handleFilesPicked(e.target.files)}
      />

      <Modal
        opened={renameTarget !== null}
        onClose={() => (busy ? undefined : setRenameTarget(null))}
        title="重命名"
        centered
        size="sm"
      >
        <Stack gap="md">
          <TextInput
            label="新名称"
            value={renameValue}
            data-autofocus
            onChange={(e) => setRenameValue(e.currentTarget.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void submitRename();
            }}
          />
          <Group justify="flex-end" gap="xs">
            <Button variant="default" onClick={() => setRenameTarget(null)} disabled={busy}>
              取消
            </Button>
            <Button onClick={() => void submitRename()} loading={busy}>
              确定
            </Button>
          </Group>
        </Stack>
      </Modal>

      <Modal
        opened={deleteTarget !== null}
        onClose={() => (busy ? undefined : setDeleteTarget(null))}
        title="确认删除"
        centered
        size="sm"
      >
        <Stack gap="md">
          <Text size="sm">
            确定要删除{' '}
            <Text span fw={600}>
              {deleteTarget?.name}
            </Text>{' '}
            吗？
            {deleteTarget?.kind === 'directory' ? '该文件夹及其内容将被一并删除。' : ''}{' '}
            此操作不可撤销。
          </Text>
          <Group justify="flex-end" gap="xs">
            <Button variant="default" onClick={() => setDeleteTarget(null)} disabled={busy}>
              取消
            </Button>
            <Button color="red" onClick={() => void confirmDelete()} loading={busy}>
              删除
            </Button>
          </Group>
        </Stack>
      </Modal>
    </TreeContext.Provider>
  );
}

function EmptyViewer() {
  return (
    <Stack gap="xs" align="center" justify="center" style={{ flex: 1 }}>
      <IconFile size={32} stroke={1.4} color="var(--mantine-color-dimmed)" />
      <Text c="dimmed">Pick a file from the tree to preview it.</Text>
    </Stack>
  );
}

const TREE_INDENT = 18;
const TREE_TOGGLE_WIDTH = 18;
const TREE_ICON_SIZE = 16;

function DirNode({ path, label, depth }: { path: string; label: string; depth: number }) {
  const t = useTree();
  const list = useFiles((s) => s.list);
  const workspaceId = useFiles((s) => s.workspaceId);
  const refreshToken = useFiles((s) => s.refreshToken);
  const cacheKey = `${workspaceId ?? ''}|${path}`;
  const cached = useFiles((s) => s.listings[cacheKey]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [dropActive, setDropActive] = useState(false);
  const isOpen = t.openDirs.has(path);
  const isRoot = path === '';

  // Re-fetch whenever the directory is opened, the workspace resolves, or a
  // refresh is requested. Cached entries (if any) keep showing while the
  // re-fetch runs, so re-expanding a folder picks up filesystem changes
  // without a blank flash. Depending on `workspaceId` (not just `cached`)
  // also fixes the first-open race where the listing was fetched before the
  // workspace id was known and cached under the wrong key.
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

  const entry: FilesEntry = { name: label, path, kind: 'directory', size: 0, mod_time: 0 };
  const canAccept = canDrop(t.dragPath, path);

  return (
    <Stack gap={0}>
      <Group
        className="cw-tree-row"
        data-drop={dropActive ? 'true' : undefined}
        gap={0}
        py={3}
        px="xs"
        wrap="nowrap"
        draggable={!isRoot}
        onClick={() => t.toggleDir(path)}
        onContextMenu={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setMenu({ x: e.clientX, y: e.clientY });
        }}
        onDragStart={(e) => {
          if (isRoot) return;
          e.stopPropagation();
          e.dataTransfer.setData('text/plain', path);
          e.dataTransfer.effectAllowed = 'move';
          t.setDragPath(path);
        }}
        onDragEnd={() => t.setDragPath(null)}
        onDragOver={(e) => {
          // Stop here so only the folder directly under the cursor is a drop
          // target — without this the event bubbles to ancestor folders and
          // they'd light up (and accept the drop) too.
          e.stopPropagation();
          if (!canAccept) return;
          e.preventDefault();
          e.dataTransfer.dropEffect = 'move';
          if (!dropActive) setDropActive(true);
        }}
        onDragLeave={() => {
          if (dropActive) setDropActive(false);
        }}
        onDrop={(e) => {
          // Match onDragOver: the innermost folder owns the drop, so don't let
          // it bubble and trigger a second move on an ancestor folder.
          e.stopPropagation();
          e.preventDefault();
          setDropActive(false);
          const from = t.dragPath ?? e.dataTransfer.getData('text/plain');
          if (from) t.onMove(from, path);
        }}
        style={{ cursor: 'pointer' }}
      >
        <Box style={{ width: depth * TREE_INDENT, flexShrink: 0 }} />
        <Box
          style={{
            width: TREE_TOGGLE_WIDTH,
            marginRight: 4,
            display: 'flex',
            justifyContent: 'center',
            flexShrink: 0,
          }}
        >
          {isOpen ? <IconChevronDown size={12} /> : <IconChevronRight size={12} />}
        </Box>
        <IconFolder size={TREE_ICON_SIZE} style={{ marginRight: 6, flexShrink: 0 }} />
        <Text size="xs" fw={depth === 0 ? 600 : 500} truncate style={{ flex: 1, minWidth: 0 }}>
          {label}
        </Text>
        <RowMenu entry={entry} isDir isRoot={isRoot} menu={menu} setMenu={setMenu} />
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
          {(cached ?? []).map((child) =>
            child.kind === 'directory' ? (
              <DirNode key={child.path} path={child.path} label={child.name} depth={depth + 1} />
            ) : (
              <FileLeaf key={child.path} entry={child} depth={depth + 1} />
            ),
          )}
        </Stack>
      ) : null}
    </Stack>
  );
}

function FileLeaf({ entry, depth }: { entry: FilesEntry; depth: number }) {
  const t = useTree();
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const active = t.activePath === entry.path;
  const Icon =
    entry.kind === 'symlink' ? IconLink : isLikelyText(entry.name) ? IconFileText : IconFile;
  return (
    <Group
      className="cw-tree-row"
      gap={0}
      py={3}
      px="xs"
      wrap="nowrap"
      draggable
      onClick={() => t.select(entry.path)}
      onContextMenu={(e) => {
        e.preventDefault();
        e.stopPropagation();
        setMenu({ x: e.clientX, y: e.clientY });
      }}
      onDragStart={(e) => {
        e.stopPropagation();
        e.dataTransfer.setData('text/plain', entry.path);
        e.dataTransfer.effectAllowed = 'move';
        t.setDragPath(entry.path);
      }}
      onDragEnd={() => t.setDragPath(null)}
      style={{
        cursor: 'pointer',
        background: active ? 'light-dark(#e9eefe, var(--mantine-color-brand-9))' : 'transparent',
        color: active ? 'light-dark(var(--mantine-color-brand-8), #ffffff)' : undefined,
        borderRadius: 4,
      }}
    >
      <Box style={{ width: depth * TREE_INDENT, flexShrink: 0 }} />
      <Box style={{ width: TREE_TOGGLE_WIDTH, marginRight: 4, flexShrink: 0 }} />
      <Icon size={TREE_ICON_SIZE} style={{ marginRight: 6, flexShrink: 0 }} />
      <Text size="xs" truncate style={{ flex: 1, minWidth: 0 }}>
        {entry.name}
      </Text>
      <RowMenu entry={entry} isDir={false} isRoot={false} menu={menu} setMenu={setMenu} />
    </Group>
  );
}

function RowMenu({
  entry,
  isDir,
  isRoot,
  menu,
  setMenu,
}: {
  entry: FilesEntry;
  isDir: boolean;
  isRoot: boolean;
  menu: { x: number; y: number } | null;
  setMenu: (v: { x: number; y: number } | null) => void;
}) {
  const t = useTree();
  return (
    <>
      <ActionIcon
        className="cw-tree-row__menu"
        data-open={menu ? 'true' : undefined}
        variant="subtle"
        color="gray"
        size="sm"
        aria-label="文件操作"
        onClick={(e) => {
          e.stopPropagation();
          if (menu) {
            setMenu(null);
            return;
          }
          // Keyboard activation reports a (0,0) pointer — fall back to the
          // button's own position so the menu doesn't fly to the corner.
          const r = e.currentTarget.getBoundingClientRect();
          setMenu(e.detail === 0 ? { x: r.left, y: r.bottom } : { x: e.clientX, y: e.clientY });
        }}
      >
        <IconDots size={15} />
      </ActionIcon>
      <Menu
        opened={menu !== null}
        onChange={(o) => {
          if (!o) setMenu(null);
        }}
        position="bottom-start"
        offset={2}
        withinPortal
        shadow="md"
        width={170}
      >
        <Menu.Target>
          {/* Invisible anchor pinned to the click point so the dropdown opens
              at the cursor instead of at the right-aligned ⋮ button. */}
          <div
            aria-hidden
            style={{
              position: 'fixed',
              left: menu?.x ?? -9999,
              top: menu?.y ?? -9999,
              width: 0,
              height: 0,
              pointerEvents: 'none',
            }}
          />
        </Menu.Target>
        <Menu.Dropdown onClick={(e) => e.stopPropagation()}>
          {isDir ? (
            <Menu.Item
              leftSection={<IconUpload size={14} />}
              onClick={() => t.onUpload(entry.path)}
            >
              上传到此处
            </Menu.Item>
          ) : (
            <Menu.Item leftSection={<IconDownload size={14} />} onClick={() => t.onDownload(entry)}>
              下载
            </Menu.Item>
          )}
          {!isRoot ? (
            <>
              <Menu.Item leftSection={<IconPencil size={14} />} onClick={() => t.onRename(entry)}>
                重命名
              </Menu.Item>
              <Menu.Item
                color="red"
                leftSection={<IconTrash size={14} />}
                onClick={() => t.onDelete(entry)}
              >
                删除
              </Menu.Item>
            </>
          ) : null}
        </Menu.Dropdown>
      </Menu>
    </>
  );
}

function FileViewer({ path }: { path: string }) {
  const readFile = useFiles((s) => s.readFile);
  const [text, setText] = useState<string | null>(null);
  const [meta, setMeta] = useState<FilesReadResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [mdView, setMdView] = useState<'rendered' | 'raw'>('rendered');

  useEffect(() => {
    setText(null);
    setError(null);
    setMdView('rendered');
    readFile(path)
      .then((r) => {
        setText(r.text ?? '');
        setMeta(r.res);
      })
      .catch((e: Error) => setError(e.message));
  }, [readFile, path]);

  const filename = useMemo(() => path.split('/').pop() ?? path, [path]);
  const isMarkdown = useMemo(() => /\.(md|markdown|mdx)$/i.test(filename), [filename]);
  const canRender = isMarkdown && text != null && !meta?.is_binary;

  return (
    <Stack style={{ flex: 1, minHeight: 0 }} gap="xs">
      <Group justify="space-between" px="xs" wrap="nowrap">
        <Stack gap={0} style={{ minWidth: 0 }}>
          <Text fw={600} size="sm" truncate>
            {filename}
          </Text>
          <Tooltip label={path}>
            <Text size="xs" c="dimmed" truncate>
              {path}
            </Text>
          </Tooltip>
        </Stack>
        <Group gap="xs" wrap="nowrap">
          {meta ? (
            <Badge size="xs" variant="light">
              {humanSize(meta.size)}
            </Badge>
          ) : null}
          {meta?.is_binary ? (
            <Badge size="xs" color="yellow" variant="light">
              binary
            </Badge>
          ) : null}
          {meta?.truncated ? (
            <Badge size="xs" color="orange" variant="light">
              truncated
            </Badge>
          ) : null}
          {canRender ? (
            <SegmentedControl
              size="xs"
              value={mdView}
              onChange={(v) => setMdView(v as 'rendered' | 'raw')}
              data={[
                {
                  value: 'rendered',
                  label: (
                    <Tooltip label="渲染" withArrow>
                      <Center>
                        <IconEye size={14} />
                      </Center>
                    </Tooltip>
                  ),
                },
                {
                  value: 'raw',
                  label: (
                    <Tooltip label="源码" withArrow>
                      <Center>
                        <IconCode size={14} />
                      </Center>
                    </Tooltip>
                  ),
                },
              ]}
            />
          ) : null}
        </Group>
      </Group>
      {error ? (
        <Alert color="red" variant="light" radius="md">
          {error}
        </Alert>
      ) : meta?.is_binary ? (
        <Stack align="center" justify="center" style={{ flex: 1 }} gap="xs">
          <IconFile size={32} stroke={1.4} />
          <Text c="dimmed">{meta.content_type ?? 'binary file'} — preview not shown.</Text>
        </Stack>
      ) : text == null ? (
        <Text c="dimmed" px="xs">
          Loading…
        </Text>
      ) : canRender && mdView === 'rendered' ? (
        <MarkdownDocument text={text} />
      ) : (
        <CodeViewer text={text} filename={filename} />
      )}
    </Stack>
  );
}

// Rendered-markdown reader: comfortable padding + a capped measure so long
// lines stay readable, scrolling within the viewer pane.
function MarkdownDocument({ text }: { text: string }) {
  return (
    <ScrollArea type="auto" style={{ flex: 1, minHeight: 0 }}>
      <Box className="cw-md-doc" px="md" py="sm" style={{ maxWidth: 820, width: '100%' }}>
        <MarkdownBody text={text} size="sm" />
      </Box>
    </ScrollArea>
  );
}

// Trigger a browser "save as" for in-memory bytes. Copy into a fresh
// ArrayBuffer so the Blob part is unambiguously an ArrayBuffer (a plain
// Uint8Array may be typed over a SharedArrayBuffer, which BlobPart rejects).
function triggerDownload(name: string, bytes: Uint8Array, contentType?: string) {
  const buffer = new ArrayBuffer(bytes.byteLength);
  new Uint8Array(buffer).set(bytes);
  const blob = new Blob([buffer], { type: contentType || 'application/octet-stream' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// Parent directory of a workspace-relative path ("" for top-level entries).
function dirOf(path: string): string {
  const i = path.lastIndexOf('/');
  return i === -1 ? '' : path.slice(0, i);
}

function baseName(path: string): string {
  return path.split('/').pop() ?? path;
}

// Whether `from` can be moved into directory `toDir`: not itself, not its own
// current folder (a no-op), and not into its own subtree (for directories).
function canDrop(from: string | null, toDir: string): boolean {
  if (!from) return false;
  if (from === toDir) return false;
  if (dirOf(from) === toDir) return false;
  if (toDir === from || toDir.startsWith(from + '/')) return false;
  return true;
}

function isLikelyText(name: string): boolean {
  return /\.(md|txt|ts|tsx|js|jsx|json|go|py|rs|toml|yaml|yml|css|html|sh|conf|env|gitignore|prettierrc|prettierignore)$/i.test(
    name,
  );
}

function humanSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
