import { colors } from '@coding-anywhere/tokens';
import { useRouter } from 'expo-router';
import { SymbolView } from 'expo-symbols';
import { useColorScheme } from 'nativewind';
import { useMemo, useState } from 'react';
import {
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { Composer } from '../../components/Composer';
import { groupByWorkspace, type WorkspaceGroup } from '../../components/session-groups';
import { useChat } from '../../store/chat';
import { useConnection } from '../../store/connection';

export default function Workspaces() {
  const router = useRouter();
  const { colorScheme: scheme } = useColorScheme();
  const t = scheme === 'dark' ? colors.dark : colors.light;
  const sessions = useChat((s) => s.sessions);
  const status = useConnection((s) => s.status);

  const [draft, setDraft] = useState('');
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState('');

  const workspaces = useMemo(() => groupByWorkspace(sessions), [sessions]);
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return workspaces;
    return workspaces.filter(
      (w) => w.label.toLowerCase().includes(q) || w.cwd.toLowerCase().includes(q),
    );
  }, [workspaces, query]);

  function openWorkspace(w: WorkspaceGroup) {
    router.push({ pathname: '/workspace', params: { cwd: w.cwd, label: w.label } });
  }

  function onSend() {
    const text = draft.trim();
    if (!text) return;
    setDraft('');
    router.push({ pathname: '/new-session', params: { prompt: text } });
  }

  return (
    <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark" edges={['top']}>
      <KeyboardAvoidingView
        className="flex-1"
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        {/* Top bar */}
        <View className="flex-row items-center justify-end px-xl pb-sm pt-sm">
          <View className="flex-row items-center gap-sm">
            <Pressable
              onPress={() => {
                setSearchOpen((v) => !v);
                if (searchOpen) setQuery('');
              }}
              className="h-10 w-10 items-center justify-center rounded-full bg-surface-muted active:opacity-70 dark:bg-surface-muted-dark"
            >
              <SymbolView name="magnifyingglass" size={17} tintColor={t.text} />
            </Pressable>
            <Pressable
              onPress={() => router.push('/new-session')}
              className="h-10 w-10 items-center justify-center rounded-full bg-ink-900 active:opacity-70 dark:bg-ink-100"
            >
              <SymbolView
                name="plus"
                size={17}
                tintColor={scheme === 'dark' ? '#0e0e10' : '#ffffff'}
              />
            </Pressable>
          </View>
        </View>

        <Text className="px-xl pb-xs text-xxl font-bold text-text dark:text-text-dark">工作区</Text>

        {searchOpen ? (
          <View className="mx-xl mb-sm flex-row items-center gap-sm rounded-md border border-border bg-surface-muted px-md dark:border-border-dark dark:bg-surface-muted-dark">
            <SymbolView name="magnifyingglass" size={14} tintColor={t.textMuted} />
            <TextInput
              autoFocus
              className="h-10 flex-1 text-md text-text dark:text-text-dark"
              placeholder="搜索工作区"
              placeholderTextColor={t.textMuted}
              value={query}
              onChangeText={setQuery}
            />
          </View>
        ) : null}

        {status !== 'connected' ? (
          <Text className="px-xl pb-1 text-sm text-text-muted dark:text-text-muted-dark">
            等待 relay 连接…
          </Text>
        ) : null}

        <ScrollView contentContainerClassName="px-xl pb-md" keyboardShouldPersistTaps="handled">
          {!query ? (
            <WorkspaceRow
              icon="square.stack.3d.up"
              label="All Repos"
              meta={`${workspaces.reduce((n, w) => n + w.sessions.length, 0)} 个会话`}
              tint={t.text}
              onPress={() =>
                router.push({ pathname: '/workspace', params: { all: '1', label: 'All Repos' } })
              }
            />
          ) : null}

          {filtered.map((w) => (
            <WorkspaceRow
              key={w.cwd}
              icon="folder"
              label={w.label}
              meta={`${w.sessions.length} 个会话`}
              tint={t.text}
              onPress={() => openWorkspace(w)}
            />
          ))}

          {filtered.length === 0 && query ? (
            <Text className="mt-lg text-center text-sm text-text-muted dark:text-text-muted-dark">
              没有匹配的工作区
            </Text>
          ) : null}

          {!query ? (
            <WorkspaceRow
              icon="folder.badge.plus"
              label="Add Repo"
              tint={t.textMuted}
              onPress={() => router.push('/new-session')}
            />
          ) : null}

          {workspaces.length === 0 && !query ? (
            <Text className="mt-lg text-sm text-text-muted dark:text-text-muted-dark">
              还没有任何会话。点右上角「+」或在下面输入开始。
            </Text>
          ) : null}
        </ScrollView>

        <Composer
          draft={draft}
          onChangeDraft={setDraft}
          onSend={onSend}
          busy={status !== 'connected'}
          placeholder="Plan, ask, build…"
        />
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}

function WorkspaceRow({
  icon,
  label,
  meta,
  tint,
  onPress,
}: {
  icon: Parameters<typeof SymbolView>[0]['name'];
  label: string;
  meta?: string;
  tint: string;
  onPress: () => void;
}) {
  return (
    <Pressable
      onPress={onPress}
      className="flex-row items-center gap-md border-b border-border py-md active:opacity-70 dark:border-border-dark"
    >
      <SymbolView name={icon} size={20} tintColor={tint} />
      <View className="flex-1">
        <Text
          className="text-md text-text dark:text-text-dark"
          numberOfLines={1}
          style={{ color: tint }}
        >
          {label}
        </Text>
      </View>
      {meta ? (
        <Text className="text-xs text-text-muted dark:text-text-muted-dark">{meta}</Text>
      ) : null}
      <SymbolView name="chevron.right" size={14} tintColor={tint} />
    </Pressable>
  );
}
