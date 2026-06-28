import { type PortsEntry, proxyURL } from '@coding-anywhere/protocol';
import { SymbolView } from 'expo-symbols';
import { useEffect, useState } from 'react';
import { ActivityIndicator, Modal, Pressable, ScrollView, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { WebView } from 'react-native-webview';

import { useConnection } from '../../store/connection';
import { usePorts } from '../../store/ports';

function PortRow({ entry, onOpen }: { entry: PortsEntry; onOpen: () => void }) {
  return (
    <View className="flex-row items-center gap-md rounded-md border border-border p-md dark:border-border-dark">
      <View className="flex-1">
        <Text className="text-md font-semibold text-text dark:text-text-dark">:{entry.port}</Text>
        <Text className="text-xs text-text-muted dark:text-text-muted-dark" numberOfLines={1}>
          {entry.address}
          {entry.command ? ` · ${entry.command}` : ''}
        </Text>
      </View>
      <Pressable
        onPress={onOpen}
        className="h-9 flex-row items-center gap-xs rounded-md bg-ink-900 px-md active:opacity-80 dark:bg-ink-100"
      >
        <SymbolView name="safari" size={14} tintColor="#ffffff" />
        <Text className="text-sm font-semibold text-white dark:text-ink-900">打开</Text>
      </Pressable>
    </View>
  );
}

export default function PortsTab() {
  const status = useConnection((s) => s.status);
  const credentials = useConnection((s) => s.credentials);
  const entries = usePorts((s) => s.entries);
  const refresh = usePorts((s) => s.refresh);

  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [preview, setPreview] = useState<{ port: number; url: string } | null>(null);

  async function doRefresh() {
    if (status !== 'connected') return;
    setLoading(true);
    setError(null);
    try {
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void doRefresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status]);

  function openPreview(port: number) {
    if (!credentials) return;
    const url = proxyURL({
      relayURL: credentials.relay_url,
      port,
      token: credentials.bundle.access_token,
    });
    setPreview({ port, url });
  }

  return (
    <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark" edges={['top']}>
      <View className="flex-row items-center justify-between px-xl pb-sm pt-lg">
        <Text className="text-xxl font-bold text-text dark:text-text-dark">端口</Text>
        <Pressable onPress={() => void doRefresh()} className="p-1 active:opacity-60">
          <SymbolView name="arrow.clockwise" size={18} tintColor="#3d54ee" />
        </Pressable>
      </View>

      {status !== 'connected' ? (
        <Text className="px-xl text-sm text-text-muted dark:text-text-muted-dark">等待连接…</Text>
      ) : null}
      {error ? <Text className="px-xl text-sm text-danger">{error}</Text> : null}
      {loading ? <ActivityIndicator className="mt-sm" /> : null}

      <ScrollView contentContainerClassName="gap-sm p-xl pt-sm">
        {entries.length === 0 && !loading ? (
          <Text className="text-sm text-text-muted dark:text-text-muted-dark">
            没有检测到监听端口。
          </Text>
        ) : (
          entries.map((e) => (
            <PortRow key={`${e.address}:${e.port}`} entry={e} onOpen={() => openPreview(e.port)} />
          ))
        )}
      </ScrollView>

      <Modal visible={!!preview} animationType="slide" onRequestClose={() => setPreview(null)}>
        <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark" edges={['top']}>
          <View className="flex-row items-center gap-sm border-b border-border px-md py-sm dark:border-border-dark">
            <Pressable onPress={() => setPreview(null)} className="p-1 active:opacity-60">
              <SymbolView name="xmark" size={18} tintColor="#3d54ee" />
            </Pressable>
            <Text className="flex-1 text-md font-semibold text-text dark:text-text-dark">
              localhost:{preview?.port}
            </Text>
          </View>
          {preview ? (
            <WebView
              source={{ uri: preview.url }}
              startInLoadingState
              renderLoading={() => (
                <View className="absolute inset-0 items-center justify-center">
                  <ActivityIndicator />
                </View>
              )}
            />
          ) : null}
        </SafeAreaView>
      </Modal>
    </SafeAreaView>
  );
}
