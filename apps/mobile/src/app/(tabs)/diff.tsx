import { type DiffChange } from '@coding-anywhere/protocol';
import { SymbolView } from 'expo-symbols';
import { useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { useConnection } from '../../store/connection';
import { useDiff } from '../../store/diff';

type DiffLine = { type: 'ctx' | 'add' | 'del'; text: string };

// Compact LCS line diff. Capped to keep the O(n*m) table bounded on device;
// larger files fall back to showing the current content without inline diff.
const MAX_LINES = 1500;
function lineDiff(a: string, b: string): { lines: DiffLine[]; capped: boolean } {
  const A = a.length ? a.split('\n') : [];
  const B = b.length ? b.split('\n') : [];
  if (A.length > MAX_LINES || B.length > MAX_LINES) {
    return { lines: B.map((text) => ({ type: 'ctx', text })), capped: true };
  }
  const n = A.length;
  const m = B.length;
  const dp: Uint16Array[] = Array.from({ length: n + 1 }, () => new Uint16Array(m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i]![j] = A[i] === B[j] ? dp[i + 1]![j + 1]! + 1 : Math.max(dp[i + 1]![j]!, dp[i]![j + 1]!);
    }
  }
  const lines: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (A[i] === B[j]) {
      lines.push({ type: 'ctx', text: A[i]! });
      i++;
      j++;
    } else if (dp[i + 1]![j]! >= dp[i]![j + 1]!) {
      lines.push({ type: 'del', text: A[i]! });
      i++;
    } else {
      lines.push({ type: 'add', text: B[j]! });
      j++;
    }
  }
  while (i < n) lines.push({ type: 'del', text: A[i++]! });
  while (j < m) lines.push({ type: 'add', text: B[j++]! });
  return { lines, capped: false };
}

const KIND_LABEL: Record<string, string> = {
  added: '新增',
  modified: '修改',
  deleted: '删除',
  renamed: '重命名',
  copied: '复制',
  untracked: '未跟踪',
};

function ChangeRow({ change, onPress }: { change: DiffChange; onPress: () => void }) {
  return (
    <Pressable
      onPress={onPress}
      className="flex-row items-center gap-sm rounded-md p-md active:opacity-70"
    >
      <View className="rounded-sm bg-surface-muted px-sm py-0.5 dark:bg-surface-muted-dark">
        <Text className="text-xs text-text-muted dark:text-text-muted-dark">
          {KIND_LABEL[change.kind] ?? change.kind}
        </Text>
      </View>
      <Text className="flex-1 text-sm text-text dark:text-text-dark" numberOfLines={1}>
        {change.path}
      </Text>
      <SymbolView name="chevron.right" size={14} tintColor="#9aa1ac" />
    </Pressable>
  );
}

function DiffRow({ line }: { line: DiffLine }) {
  const bg = line.type === 'add' ? 'bg-success/10' : line.type === 'del' ? 'bg-danger/10' : '';
  const sign = line.type === 'add' ? '+' : line.type === 'del' ? '-' : ' ';
  const color = line.type === 'add' ? '#16a34a' : line.type === 'del' ? '#dc2626' : undefined;
  return (
    <View className={`flex-row ${bg}`}>
      <Text style={{ fontFamily: 'Menlo', fontSize: 11, width: 14, color }}>{sign}</Text>
      <Text selectable style={{ fontFamily: 'Menlo', fontSize: 11, color }}>
        {line.text || ' '}
      </Text>
    </View>
  );
}

export default function DiffTab() {
  const status = useConnection((s) => s.status);
  const isRepo = useDiff((s) => s.isRepo);
  const changes = useDiff((s) => s.changes);
  const refresh = useDiff((s) => s.refresh);
  const versions = useDiff((s) => s.versions);

  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [viewing, setViewing] = useState<{
    path: string;
    lines: DiffLine[];
    capped: boolean;
  } | null>(null);

  useEffect(() => {
    if (status !== 'connected') return;
    setError(null);
    refresh().catch((e: Error) => setError(e.message));
  }, [status, refresh]);

  async function openChange(c: DiffChange) {
    setLoading(true);
    setError(null);
    try {
      const v = await versions(c.path);
      const { lines, capped } = lineDiff(v.original ?? '', v.current ?? '');
      setViewing({ path: c.path, lines, capped });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

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
            {viewing.path}
          </Text>
        </View>
        {viewing.capped ? (
          <Text className="px-md pt-sm text-xs text-text-muted dark:text-text-muted-dark">
            文件较大，仅显示当前内容（未做逐行 diff）。
          </Text>
        ) : null}
        <ScrollView contentContainerClassName="p-md">
          <ScrollView horizontal>
            <View>
              {viewing.lines.map((l, i) => (
                <DiffRow key={i} line={l} />
              ))}
            </View>
          </ScrollView>
        </ScrollView>
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark" edges={['top']}>
      <View className="flex-row items-center justify-between px-xl pb-sm pt-lg">
        <Text className="text-xxl font-bold text-text dark:text-text-dark">Diff</Text>
        <Pressable onPress={() => void refresh()} className="p-1 active:opacity-60">
          <SymbolView name="arrow.clockwise" size={18} tintColor="#3d54ee" />
        </Pressable>
      </View>

      {status !== 'connected' ? (
        <Text className="px-xl text-sm text-text-muted dark:text-text-muted-dark">等待连接…</Text>
      ) : null}
      {error ? <Text className="px-xl text-sm text-danger">{error}</Text> : null}
      {loading ? <ActivityIndicator className="mt-sm" /> : null}

      <ScrollView contentContainerClassName="gap-1 p-md pt-sm">
        {!isRepo ? (
          <Text className="px-md text-sm text-text-muted dark:text-text-muted-dark">
            当前 workspace 不是 git 仓库。
          </Text>
        ) : changes.length === 0 ? (
          <Text className="px-md text-sm text-text-muted dark:text-text-muted-dark">
            没有未提交的改动。
          </Text>
        ) : (
          changes.map((c) => (
            <ChangeRow key={c.path} change={c} onPress={() => void openChange(c)} />
          ))
        )}
      </ScrollView>
    </SafeAreaView>
  );
}
