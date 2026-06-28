import { type FilesEntry } from '@coding-anywhere/protocol';
import { File, Paths } from 'expo-file-system';
import * as Sharing from 'expo-sharing';
import { SymbolView } from 'expo-symbols';
import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { MarkdownBody } from '../../components/Markdown';
import { useConnection } from '../../store/connection';
import { useFiles } from '../../store/files';

function parentPath(p: string): string {
  const i = p.lastIndexOf('/');
  return i <= 0 ? '' : p.slice(0, i);
}

function isMarkdown(name: string): boolean {
  return /\.(md|markdown|mdx)$/i.test(name);
}

type Viewing = { path: string; name: string; text: string | null; binary: boolean };

export default function FilesTab() {
  const status = useConnection((s) => s.status);
  const workspaces = useFiles((s) => s.workspaces);
  const workspaceId = useFiles((s) => s.workspaceId);
  const loadWorkspaces = useFiles((s) => s.loadWorkspaces);
  const list = useFiles((s) => s.list);
  const readFile = useFiles((s) => s.readFile);
  const downloadFile = useFiles((s) => s.downloadFile);

  const [dir, setDir] = useState('');
  const [entries, setEntries] = useState<FilesEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [viewing, setViewing] = useState<Viewing | null>(null);
  const [downloading, setDownloading] = useState(false);

  useEffect(() => {
    if (status === 'connected' && workspaces.length === 0) {
      loadWorkspaces().catch((e: Error) => setError(e.message));
    }
  }, [status, workspaces.length, loadWorkspaces]);

  const loadDir = useCallback(
    async (path: string) => {
      if (status !== 'connected') return;
      setLoading(true);
      setError(null);
      try {
        setEntries(await list(path));
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setLoading(false);
      }
    },
    [status, list],
  );

  useEffect(() => {
    if (status === 'connected' && workspaceId) void loadDir(dir);
  }, [status, workspaceId, dir, loadDir]);

  async function openFile(entry: FilesEntry) {
    setError(null);
    try {
      const { text, res } = await readFile(entry.path);
      setViewing({ path: entry.path, name: entry.name, text, binary: !!res.is_binary });
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function onDownload(v: Viewing) {
    setDownloading(true);
    try {
      const { bytes } = await downloadFile(v.path);
      const f = new File(Paths.cache, v.name);
      try {
        if (f.exists) f.delete();
      } catch {
        /* ignore */
      }
      f.create();
      f.write(bytes);
      if (await Sharing.isAvailableAsync()) await Sharing.shareAsync(f.uri);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setDownloading(false);
    }
  }

  // ---- Viewer ----
  if (viewing) {
    return (
      <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark" edges={['top']}>
        <View className="flex-row items-center gap-sm border-b border-border px-md py-sm dark:border-border-dark">
          <Pressable onPress={() => setViewing(null)} className="p-1 active:opacity-60">
            <SymbolView name="chevron.left" size={20} tintColor="#3d54ee" />
          </Pressable>
          <Text
            className="flex-1 text-md font-semibold text-text dark:text-text-dark"
            numberOfLines={1}
          >
            {viewing.name}
          </Text>
          <Pressable
            onPress={() => void onDownload(viewing)}
            disabled={downloading}
            className="p-1 active:opacity-60"
          >
            {downloading ? (
              <ActivityIndicator size="small" />
            ) : (
              <SymbolView name="square.and.arrow.up" size={20} tintColor="#3d54ee" />
            )}
          </Pressable>
        </View>
        <ScrollView contentContainerClassName="p-md" horizontal={false}>
          {viewing.binary ? (
            <Text className="text-sm text-text-muted dark:text-text-muted-dark">
              二进制文件，无法预览。点右上角导出。
            </Text>
          ) : isMarkdown(viewing.name) ? (
            <MarkdownBody text={viewing.text ?? ''} />
          ) : (
            <ScrollView horizontal>
              <Text
                selectable
                className="text-text dark:text-text-dark"
                style={{ fontFamily: 'Menlo', fontSize: 12 }}
              >
                {viewing.text || '（空文件）'}
              </Text>
            </ScrollView>
          )}
        </ScrollView>
      </SafeAreaView>
    );
  }

  // ---- Browser ----
  const sorted = [...entries].sort((a, b) => {
    const ad = a.kind === 'directory' ? 0 : 1;
    const bd = b.kind === 'directory' ? 0 : 1;
    return ad !== bd ? ad - bd : a.name.localeCompare(b.name);
  });
  const ws = workspaces.find((w) => w.id === workspaceId);

  return (
    <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark" edges={['top']}>
      <View className="px-xl pb-sm pt-lg">
        <Text className="text-xxl font-bold text-text dark:text-text-dark">文件</Text>
        <Text className="text-xs text-text-muted dark:text-text-muted-dark" numberOfLines={1}>
          {ws ? ws.name : ''} /{dir}
        </Text>
      </View>

      {status !== 'connected' ? (
        <Text className="px-xl text-sm text-text-muted dark:text-text-muted-dark">等待连接…</Text>
      ) : null}
      {error ? <Text className="px-xl text-sm text-danger">{error}</Text> : null}

      <ScrollView contentContainerClassName="gap-1 p-md pt-sm">
        {dir !== '' ? (
          <Pressable
            onPress={() => setDir(parentPath(dir))}
            className="flex-row items-center gap-sm rounded-md p-md active:opacity-70"
          >
            <SymbolView name="arrow.up.left" size={16} tintColor="#9aa1ac" />
            <Text className="text-sm text-text-muted dark:text-text-muted-dark">上一级</Text>
          </Pressable>
        ) : null}

        {loading && entries.length === 0 ? (
          <ActivityIndicator className="mt-lg" />
        ) : (
          sorted.map((e) => {
            const isDir = e.kind === 'directory';
            return (
              <Pressable
                key={e.path}
                onPress={() => (isDir ? setDir(e.path) : void openFile(e))}
                className="flex-row items-center gap-sm rounded-md p-md active:opacity-70"
              >
                <SymbolView
                  name={isDir ? 'folder.fill' : 'doc.text'}
                  size={18}
                  tintColor={isDir ? '#3d54ee' : '#9aa1ac'}
                />
                <Text className="flex-1 text-sm text-text dark:text-text-dark" numberOfLines={1}>
                  {e.name}
                </Text>
                {isDir ? (
                  <SymbolView name="chevron.right" size={14} tintColor="#9aa1ac" />
                ) : (
                  <Text className="text-xs text-text-muted dark:text-text-muted-dark">
                    {formatSize(e.size)}
                  </Text>
                )}
              </Pressable>
            );
          })
        )}
      </ScrollView>
    </SafeAreaView>
  );
}

function formatSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
