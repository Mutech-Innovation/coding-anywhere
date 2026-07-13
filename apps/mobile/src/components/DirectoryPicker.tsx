// Directory picker for choosing a session's project directory (cwd) instead of
// hand-typing an absolute path. Browses the daemon's allowed workspaces via the
// files API: pick a workspace root, drill into subfolders, then "选择此目录".
// `DirectoryField` is the labelled form control (trigger + manual fallback) that
// embeds the modal; drop it into any form that needs a cwd.
import { type FilesEntry } from '@coding-anywhere/protocol';
import { colors } from '@coding-anywhere/tokens';
import { SymbolView } from 'expo-symbols';
import { useColorScheme } from 'nativewind';
import { useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Modal,
  Pressable,
  ScrollView,
  Text,
  TextInput,
  View,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { useConnection } from '../store/connection';
import { useFiles } from '../store/files';
import { Button } from './ui';

function joinAbs(root: string, rel: string): string {
  if (!rel) return root;
  return `${root.replace(/\/+$/, '')}/${rel}`;
}

export function DirectoryPickerModal({
  visible,
  onClose,
  onSelect,
}: {
  visible: boolean;
  onClose: () => void;
  onSelect: (absolutePath: string) => void;
}) {
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  const insets = useSafeAreaInsets();
  const status = useConnection((s) => s.status);
  const workspaces = useFiles((s) => s.workspaces);
  const workspaceId = useFiles((s) => s.workspaceId);
  const setWorkspace = useFiles((s) => s.setWorkspace);
  const loadWorkspaces = useFiles((s) => s.loadWorkspaces);
  const list = useFiles((s) => s.list);

  const [path, setPath] = useState('');
  const [entries, setEntries] = useState<FilesEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const workspace = workspaces.find((w) => w.id === workspaceId) ?? workspaces[0];

  // Load workspaces + reset to root each time the picker opens.
  useEffect(() => {
    if (!visible) return;
    setPath('');
    setError(null);
    if (status === 'connected') loadWorkspaces().catch((e: Error) => setError(e.message));
  }, [visible, status, loadWorkspaces]);

  // List the current directory whenever the workspace or path changes.
  useEffect(() => {
    if (!visible || !workspaceId) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    list(path)
      .then((es) => {
        if (!cancelled) setEntries(es);
      })
      .catch((e: Error) => {
        if (!cancelled) setError(e.message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [visible, workspaceId, path, list]);

  const dirs = entries.filter((e) => e.kind === 'directory');
  const absPath = workspace ? joinAbs(workspace.root, path) : '';

  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose}>
      <Pressable className="flex-1 justify-end bg-black/40" onPress={onClose}>
        <Pressable
          onPress={(e) => e.stopPropagation()}
          className="h-[85%] rounded-t-xl bg-surface dark:bg-surface-elevated-dark"
          style={{ paddingBottom: Math.max(insets.bottom, 12) }}
        >
          <View className="items-center pt-sm">
            <View className="h-1 w-10 rounded-full bg-ink-300 dark:bg-ink-600" />
          </View>
          <View className="flex-row items-center px-lg py-md">
            <Pressable
              onPress={onClose}
              className="h-8 w-8 items-center justify-center rounded-full bg-surface-muted active:opacity-70 dark:bg-surface-muted-dark"
            >
              <SymbolView name="xmark" size={14} tintColor={t.text} />
            </Pressable>
            <Text className="flex-1 text-center text-md font-bold text-text dark:text-text-dark">
              选择项目目录
            </Text>
            <View className="w-8" />
          </View>

          {status !== 'connected' ? (
            <Text className="px-lg text-sm text-text-muted dark:text-text-muted-dark">
              等待 relay 连接…
            </Text>
          ) : !workspace ? (
            <Text className="px-lg text-sm text-text-muted dark:text-text-muted-dark">
              {loading ? '加载工作区…' : '没有可用的工作区。'}
            </Text>
          ) : (
            <>
              {workspaces.length > 1 ? (
                <ScrollView
                  horizontal
                  showsHorizontalScrollIndicator={false}
                  contentContainerClassName="gap-xs px-lg pb-sm"
                >
                  {workspaces.map((w) => {
                    const active = w.id === (workspaceId ?? workspace.id);
                    return (
                      <Pressable
                        key={w.id}
                        onPress={() => {
                          setWorkspace(w.id);
                          setPath('');
                        }}
                        className={`rounded-full border px-md py-1 active:opacity-70 ${
                          active
                            ? 'border-accent bg-accent-soft dark:border-accent-dark dark:bg-accent-soft-dark'
                            : 'border-border dark:border-border-dark'
                        }`}
                      >
                        <Text
                          className={
                            active
                              ? 'text-xs font-semibold text-accent dark:text-accent-dark'
                              : 'text-xs text-text dark:text-text-dark'
                          }
                        >
                          {w.name || w.root}
                        </Text>
                      </Pressable>
                    );
                  })}
                </ScrollView>
              ) : null}

              <View className="mx-lg mb-sm rounded-md border border-border bg-surface-muted px-md py-sm dark:border-border-dark dark:bg-surface-muted-dark">
                <Text
                  className="text-xs text-text-muted dark:text-text-muted-dark"
                  numberOfLines={1}
                >
                  {absPath}
                </Text>
              </View>

              <ScrollView className="flex-1" contentContainerClassName="px-lg">
                {path ? (
                  <Pressable
                    onPress={() => setPath((p) => p.split('/').slice(0, -1).join('/'))}
                    className="flex-row items-center gap-md border-b border-border py-md active:opacity-70 dark:border-border-dark"
                  >
                    <SymbolView name="arrow.up.left" size={18} tintColor={t.textMuted} />
                    <Text className="text-md text-text-muted dark:text-text-muted-dark">
                      上级目录
                    </Text>
                  </Pressable>
                ) : null}

                {loading && entries.length === 0 ? (
                  <View className="items-center py-lg">
                    <ActivityIndicator />
                  </View>
                ) : null}
                {error ? <Text className="py-md text-sm text-danger">{error}</Text> : null}
                {!loading && dirs.length === 0 && !error ? (
                  <Text className="py-md text-sm text-text-muted dark:text-text-muted-dark">
                    没有子目录
                  </Text>
                ) : null}

                {dirs.map((d) => (
                  <Pressable
                    key={d.path}
                    onPress={() => setPath(d.path)}
                    className="flex-row items-center gap-md border-b border-border py-md active:opacity-70 dark:border-border-dark"
                  >
                    <SymbolView name="folder" size={18} tintColor={t.text} />
                    <Text
                      className="flex-1 text-md text-text dark:text-text-dark"
                      numberOfLines={1}
                    >
                      {d.name}
                    </Text>
                    <SymbolView name="chevron.right" size={13} tintColor={t.textMuted} />
                  </Pressable>
                ))}
              </ScrollView>

              <View className="px-lg pt-sm">
                <Button
                  title="选择此目录"
                  onPress={() => {
                    onSelect(absPath);
                    onClose();
                  }}
                />
              </View>
            </>
          )}
        </Pressable>
      </Pressable>
    </Modal>
  );
}

/**
 * Labelled cwd form control: a tappable field that shows the chosen directory
 * and opens {@link DirectoryPickerModal}, plus a "手动输入" fallback for when no
 * workspace is available or an exact path is needed.
 */
export function DirectoryField({
  label,
  description,
  value,
  onChange,
}: {
  label: string;
  description?: string;
  value: string;
  onChange: (path: string) => void;
}) {
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  const [pickerOpen, setPickerOpen] = useState(false);
  const [manual, setManual] = useState(false);

  return (
    <View className="gap-xs">
      <Text className="text-sm font-semibold text-text dark:text-text-dark">{label}</Text>
      {description ? (
        <Text className="text-xs text-text-muted dark:text-text-muted-dark">{description}</Text>
      ) : null}

      {manual ? (
        <TextInput
          className="h-12 rounded-md border border-border bg-surface px-md text-md text-text dark:border-border-dark dark:bg-surface-elevated-dark dark:text-text-dark"
          placeholder="/Users/you/projects/app"
          placeholderTextColor={t.textMuted}
          value={value}
          onChangeText={onChange}
          autoCapitalize="none"
          autoCorrect={false}
        />
      ) : (
        <Pressable
          onPress={() => setPickerOpen(true)}
          className="h-12 flex-row items-center gap-sm rounded-md border border-border bg-surface px-md active:opacity-70 dark:border-border-dark dark:bg-surface-elevated-dark"
        >
          <SymbolView name="folder" size={16} tintColor={t.textMuted} />
          <Text
            className={`flex-1 text-md ${
              value ? 'text-text dark:text-text-dark' : 'text-text-muted dark:text-text-muted-dark'
            }`}
            numberOfLines={1}
          >
            {value || '点此选择目录'}
          </Text>
          <SymbolView name="chevron.right" size={14} tintColor={t.textMuted} />
        </Pressable>
      )}

      <Pressable onPress={() => setManual((m) => !m)} className="self-start active:opacity-60">
        <Text className="text-xs text-accent dark:text-accent-dark">
          {manual ? '改用目录选择' : '手动输入路径'}
        </Text>
      </Pressable>

      <DirectoryPickerModal
        visible={pickerOpen}
        onClose={() => setPickerOpen(false)}
        onSelect={onChange}
      />
    </View>
  );
}
