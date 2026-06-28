import { type ConnectionState, type UserAccountSummary } from '@coding-anywhere/protocol';
import { useState } from 'react';
import { Pressable, ScrollView, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { Button, Card, SegmentedControl } from '../../components/ui';
import { useTheme, type ThemePref } from '../../lib/theme';
import { useConnection } from '../../store/connection';

const STATUS_LABEL: Record<ConnectionState, string> = {
  connecting: '连接中…',
  connected: '已连接',
  disconnected: '已断开',
};

const STATUS_DOT: Record<ConnectionState, string> = {
  connecting: 'bg-warning',
  connected: 'bg-success',
  disconnected: 'bg-danger',
};

function Row({ label, value }: { label: string; value: string }) {
  return (
    <View className="flex-row items-center justify-between gap-md py-1">
      <Text className="text-sm text-text-muted dark:text-text-muted-dark">{label}</Text>
      <Text
        className="max-w-[62%] text-sm font-semibold text-text dark:text-text-dark"
        numberOfLines={1}
      >
        {value}
      </Text>
    </View>
  );
}

function MachineSwitcher({
  machines,
  currentId,
}: {
  machines: UserAccountSummary[];
  currentId?: string;
}) {
  const switchMachine = useConnection((s) => s.switchMachine);
  const [busy, setBusy] = useState<string | null>(null);
  if (machines.length < 2) return null;

  async function onPick(accountId: string) {
    if (accountId === currentId || busy) return;
    setBusy(accountId);
    try {
      await switchMachine(accountId);
    } finally {
      setBusy(null);
    }
  }

  return (
    <Card className="gap-sm p-lg">
      <Text className="text-xs uppercase tracking-widest text-text-muted dark:text-text-muted-dark">
        切换机器（{machines.length}）
      </Text>
      {machines.map((m) => {
        const active = m.account_id === currentId;
        return (
          <Pressable
            key={m.account_id}
            disabled={active || busy !== null}
            onPress={() => onPick(m.account_id)}
            className={`rounded-md border p-md ${
              active
                ? 'border-accent dark:border-accent-dark'
                : 'border-border active:opacity-80 dark:border-border-dark'
            }`}
          >
            <Text
              className={`text-sm ${
                active
                  ? 'font-semibold text-accent dark:text-accent-dark'
                  : 'text-text dark:text-text-dark'
              }`}
            >
              {m.label}
              {active ? '（当前）' : busy === m.account_id ? ' · 切换中…' : ''}
            </Text>
          </Pressable>
        );
      })}
    </Card>
  );
}

function ThemeToggle() {
  const pref = useTheme((s) => s.pref);
  const setPref = useTheme((s) => s.setPref);
  return (
    <Card className="gap-sm p-lg">
      <Text className="text-xs uppercase tracking-widest text-text-muted dark:text-text-muted-dark">
        外观
      </Text>
      <SegmentedControl<ThemePref>
        value={pref}
        onChange={setPref}
        options={[
          { value: 'system', label: '跟随系统' },
          { value: 'light', label: '浅色' },
          { value: 'dark', label: '深色' },
        ]}
      />
    </Card>
  );
}

export default function Settings() {
  const credentials = useConnection((s) => s.credentials);
  const status = useConnection((s) => s.status);
  const machines = useConnection((s) => s.machines);
  const signOut = useConnection((s) => s.signOut);
  const bundle = credentials?.bundle;

  return (
    <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark" edges={['top']}>
      <ScrollView contentContainerClassName="gap-lg p-xl">
        <View className="gap-xs">
          <Text className="text-xxl font-bold text-text dark:text-text-dark">账户</Text>
          <View className="flex-row items-center gap-sm">
            <View className={`h-2 w-2 rounded-full ${STATUS_DOT[status]}`} />
            <Text className="text-sm text-text-muted dark:text-text-muted-dark">
              {STATUS_LABEL[status]}
            </Text>
          </View>
        </View>

        <Card className="p-lg">
          <Text className="mb-sm text-xs uppercase tracking-widest text-text-muted dark:text-text-muted-dark">
            当前机器
          </Text>
          <Row label="设备名称" value={bundle?.device_label ?? '—'} />
          <Row label="账户" value={bundle?.account_id ?? '—'} />
          <Row label="Relay" value={credentials?.relay_url ?? '—'} />
          <Row label="协议版本" value={String(bundle?.relay_protocol_version ?? '—')} />
        </Card>

        <MachineSwitcher machines={machines} currentId={bundle?.account_id} />

        <ThemeToggle />

        <Button title="退出登录" variant="default" onPress={() => void signOut()} />
      </ScrollView>
    </SafeAreaView>
  );
}
