// Presentational chat components for the RN chat view. Mirrors the web
// ChatPanel's MessageBubble / ToolCallCard / PlanCard / PermissionCard, reusing
// the shared RN markdown renderer and the chat store's view types.
import {
  type ChatSessionStatus,
  type PlanEntry,
  type ToolCallStatus,
  type ToolKind,
} from '@coding-anywhere/protocol';
import { colors } from '@coding-anywhere/tokens';
import { SymbolView, type SymbolViewProps } from 'expo-symbols';
import { useColorScheme } from 'nativewind';
import { memo, useState } from 'react';
import { Pressable, Text, View } from 'react-native';

import { type ChatMessage, type ChatToolCall } from '../store/chat';
import { hasMeaningfulPayload } from './transcript';
import { MarkdownBody, StreamingMarkdownBody } from './Markdown';

function useColors() {
  const { colorScheme: scheme } = useColorScheme();
  return scheme === 'dark' ? colors.dark : colors.light;
}

function safeStringify(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

// ---- Status badge ---------------------------------------------------------

const STATUS_META: Record<string, { label: string; bg: string; fg: string }> = {
  // Session statuses.
  idle: { label: '空闲', bg: 'bg-ink-100 dark:bg-ink-700', fg: 'text-ink-600 dark:text-ink-200' },
  thinking: { label: '思考中', bg: 'bg-warning/15', fg: 'text-warning' },
  awaiting_permission: { label: '待授权', bg: 'bg-danger/15', fg: 'text-danger' },
  closed: {
    label: '已关闭',
    bg: 'bg-ink-100 dark:bg-ink-700',
    fg: 'text-ink-500 dark:text-ink-300',
  },
  // Tool-call statuses (distinct keys from session statuses, so no collision).
  pending: {
    label: '等待',
    bg: 'bg-ink-100 dark:bg-ink-700',
    fg: 'text-ink-600 dark:text-ink-200',
  },
  in_progress: { label: '进行中', bg: 'bg-warning/15', fg: 'text-warning' },
  completed: { label: '完成', bg: 'bg-success/15', fg: 'text-success' },
  failed: { label: '失败', bg: 'bg-danger/15', fg: 'text-danger' },
};

export function StatusBadge({ status }: { status: ChatSessionStatus | string }) {
  const meta = STATUS_META[status] ?? STATUS_META.idle;
  return (
    <View className={`rounded-full px-sm py-0.5 ${meta.bg}`}>
      <Text className={`text-xs font-semibold ${meta.fg}`}>{meta.label}</Text>
    </View>
  );
}

// ---- Message bubbles ------------------------------------------------------

export const MessageBubble = memo(function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === 'user';
  const isSystem = message.role === 'system';
  const text = message.text || (message.streaming ? '…' : '');

  // Some Devin models stream content == "None" for tool-only turns; suppress.
  if (message.role === 'agent' && message.text === 'None') return null;
  if (message.role === 'agent_thought') return <ThoughtBubble message={message} />;

  if (isSystem) {
    return (
      <View className="self-center rounded-md bg-surface-muted px-md py-1 dark:bg-surface-muted-dark">
        <Text className="text-xs text-text-muted dark:text-text-muted-dark">{text}</Text>
      </View>
    );
  }

  return (
    <View className={isUser ? 'items-end' : 'items-start'}>
      <View
        className={`max-w-[85%] rounded-lg p-md ${
          isUser
            ? 'bg-brand-800'
            : 'border border-border bg-surface dark:border-border-dark dark:bg-surface-elevated-dark'
        }`}
      >
        {message.streaming ? (
          <StreamingMarkdownBody text={text} color={isUser ? '#ffffff' : undefined} />
        ) : (
          <MarkdownBody text={text} color={isUser ? '#ffffff' : undefined} />
        )}
      </View>
    </View>
  );
});

const ThoughtBubble = memo(function ThoughtBubble({ message }: { message: ChatMessage }) {
  const [expanded, setExpanded] = useState(false);
  const c = useColors();
  const text = message.text || (message.streaming ? '…' : '');
  return (
    <View className="items-start">
      <View className="max-w-[85%] rounded-lg border border-dashed border-border bg-surface-muted p-md dark:border-border-dark dark:bg-surface-muted-dark">
        <Pressable onPress={() => setExpanded((x) => !x)} className="flex-row items-center gap-xs">
          <SymbolView name="brain" size={12} tintColor={c.textMuted} />
          <Text className="text-xs font-semibold text-text-muted dark:text-text-muted-dark">
            思考过程 {expanded ? '▾' : '▸'}
          </Text>
        </Pressable>
        {expanded ? (
          <View className="mt-xs">
            {message.streaming ? (
              <StreamingMarkdownBody text={text} size="xs" color={c.textMuted} />
            ) : (
              <MarkdownBody text={text} size="xs" color={c.textMuted} />
            )}
          </View>
        ) : null}
      </View>
    </View>
  );
});

// ---- Tool call card -------------------------------------------------------

const TOOL_STATUS_TINT: Record<ToolCallStatus | string, keyof typeof TINTS> = {
  pending: 'muted',
  in_progress: 'warning',
  completed: 'success',
  failed: 'danger',
};

const TINTS = {
  muted: '#9aa1ac',
  warning: '#d97706',
  success: '#16a34a',
  danger: '#dc2626',
} as const;

function iconForKind(kind?: ToolKind | string): SymbolViewProps['name'] {
  switch (kind) {
    case 'read':
      return 'doc.text';
    case 'edit':
      return 'pencil';
    case 'delete':
      return 'trash';
    case 'move':
      return 'arrow.right';
    case 'search':
      return 'magnifyingglass';
    case 'execute':
      return 'terminal';
    case 'fetch':
      return 'arrow.down.circle';
    case 'think':
      return 'brain';
    default:
      return 'hammer';
  }
}

export const ToolCallCard = memo(function ToolCallCard({ call }: { call: ChatToolCall }) {
  const [expanded, setExpanded] = useState(false);
  const status = call.status ?? 'pending';
  const title = call.title ?? '(tool)';
  const tint = TINTS[TOOL_STATUS_TINT[status] ?? 'muted'];

  const hasInput = hasMeaningfulPayload(call.rawInput);
  const hasOutput = hasMeaningfulPayload(call.rawOutput);
  const hasContent = !!call.content && call.content.length > 0;
  const hasDetails = hasInput || hasOutput || hasContent;

  return (
    <View className="max-w-[92%] self-start rounded-lg border border-border bg-surface p-md dark:border-border-dark dark:bg-surface-elevated-dark">
      <Pressable
        disabled={!hasDetails}
        onPress={() => setExpanded((x) => !x)}
        className="flex-row items-center gap-sm"
      >
        <SymbolView name={iconForKind(call.kind)} size={15} tintColor={tint} />
        <Text
          className="flex-1 text-sm font-semibold text-text dark:text-text-dark"
          numberOfLines={1}
        >
          {title}
        </Text>
        <StatusBadge status={status} />
        {hasDetails ? (
          <Text className="text-xs text-text-muted dark:text-text-muted-dark">
            {expanded ? '▾' : '▸'}
          </Text>
        ) : null}
      </Pressable>

      {call.locations && call.locations.length > 0 ? (
        <View className="mt-xs">
          {call.locations.map((loc, i) => (
            <Text
              key={i}
              className="text-xs text-text-muted dark:text-text-muted-dark"
              numberOfLines={1}
            >
              {loc.path}
              {typeof loc.line === 'number' ? `:${loc.line}` : ''}
            </Text>
          ))}
        </View>
      ) : null}

      {expanded ? (
        <View className="mt-sm gap-sm">
          {hasInput ? <CodeBlock label="输入" value={safeStringify(call.rawInput)} /> : null}
          {hasOutput ? <CodeBlock label="输出" value={safeStringify(call.rawOutput)} /> : null}
          {!hasInput && !hasOutput && hasContent ? (
            <CodeBlock label="内容" value={safeStringify(call.content)} />
          ) : null}
        </View>
      ) : null}

      {!hasDetails && status === 'completed' ? (
        <Text className="mt-xs text-xs text-text-muted dark:text-text-muted-dark">
          该 agent 未上报调用参数
        </Text>
      ) : null}
    </View>
  );
});

function CodeBlock({ label, value }: { label: string; value: string }) {
  return (
    <View>
      <Text className="mb-1 text-xs text-text-muted dark:text-text-muted-dark">{label}</Text>
      <View className="rounded-md border border-border bg-ink-100 p-sm dark:border-border-dark dark:bg-surface-muted-dark">
        <Text
          selectable
          className="text-text dark:text-text-dark"
          style={{ fontFamily: 'Menlo', fontSize: 11 }}
        >
          {value}
        </Text>
      </View>
    </View>
  );
}

// ---- Tool call group (collapsed run of >= 3 calls) ------------------------

export function ToolCallGroupCard({ calls }: { calls: ChatToolCall[] }) {
  const [expanded, setExpanded] = useState(false);
  const active = calls.some(
    (x) => (x.status ?? 'pending') === 'in_progress' || (x.status ?? 'pending') === 'pending',
  );
  return (
    <View className="max-w-[92%] self-start rounded-lg border border-border bg-surface p-md dark:border-border-dark dark:bg-surface-elevated-dark">
      <Pressable onPress={() => setExpanded((x) => !x)} className="flex-row items-center gap-sm">
        <SymbolView name="hammer" size={15} tintColor={active ? TINTS.warning : TINTS.success} />
        <Text className="flex-1 text-sm font-semibold text-text dark:text-text-dark">
          {calls.length} 个工具调用
        </Text>
        <Text className="text-xs text-text-muted dark:text-text-muted-dark">
          {expanded ? '▾' : '▸'}
        </Text>
      </Pressable>
      {expanded ? (
        <View className="mt-sm gap-sm">
          {calls.map((call) => (
            <ToolCallCard key={call.id} call={call} />
          ))}
        </View>
      ) : (
        <Text className="mt-xs text-xs text-text-muted dark:text-text-muted-dark" numberOfLines={1}>
          {calls
            .map((x) => x.title ?? '(tool)')
            .slice(0, 3)
            .join(' · ')}
          {calls.length > 3 ? ' …' : ''}
        </Text>
      )}
    </View>
  );
}

// ---- Plan card ------------------------------------------------------------

function planGlyph(status: PlanEntry['status']): string {
  switch (status) {
    case 'completed':
      return '✓';
    case 'in_progress':
      return '◐';
    default:
      return '○';
  }
}

export function PlanCard({ entries }: { entries: PlanEntry[] }) {
  const done = entries.filter((e) => e.status === 'completed').length;
  return (
    <View className="rounded-lg border border-border bg-surface p-md dark:border-border-dark dark:bg-surface-elevated-dark">
      <Text className="mb-xs text-sm font-semibold text-text dark:text-text-dark">
        Plan · {done}/{entries.length}
      </Text>
      <View className="gap-1">
        {entries.map((e, i) => (
          <View key={i} className="flex-row gap-sm">
            <Text className="text-sm text-text-muted dark:text-text-muted-dark">
              {planGlyph(e.status)}
            </Text>
            <Text
              className={`flex-1 text-sm ${
                e.status === 'completed'
                  ? 'text-text-muted line-through dark:text-text-muted-dark'
                  : e.status === 'in_progress'
                    ? 'font-semibold text-text dark:text-text-dark'
                    : 'text-text dark:text-text-dark'
              }`}
            >
              {e.content}
            </Text>
          </View>
        ))}
      </View>
    </View>
  );
}

// ---- Permission card ------------------------------------------------------

type AcpOption = { optionId: string; name: string; kind?: string };

function parseAcpPermissionParams(params: Record<string, unknown>): {
  title?: string;
  options: AcpOption[];
} {
  const toolCall = params.toolCall as Record<string, unknown> | undefined;
  const title =
    (params.title as string | undefined) ?? (toolCall?.title as string | undefined) ?? undefined;
  const rawOptions = (params.options as unknown[] | undefined) ?? [];
  const options: AcpOption[] = [];
  for (const o of rawOptions) {
    const obj = o as Record<string, unknown>;
    const optionId = (obj.optionId ?? obj.option_id) as string | undefined;
    const name = (obj.name ?? obj.label) as string | undefined;
    if (!optionId || !name) continue;
    options.push({ optionId, name, kind: obj.kind as string | undefined });
  }
  return { title, options };
}

function optionClasses(kind?: string): { box: string; text: string } {
  if (kind?.startsWith('allow')) return { box: 'bg-success', text: 'text-white' };
  if (kind?.startsWith('reject')) return { box: 'bg-danger', text: 'text-white' };
  return {
    box: 'border border-border dark:border-border-dark',
    text: 'text-text dark:text-text-dark',
  };
}

export function PermissionCard({
  params,
  onResolve,
}: {
  params: Record<string, unknown>;
  onResolve: (decision: unknown) => void;
}) {
  const { title, options } = parseAcpPermissionParams(params);
  const rendered: AcpOption[] =
    options.length > 0
      ? options
      : [
          { optionId: 'allow_once', name: '允许', kind: 'allow_once' },
          { optionId: 'reject_once', name: '拒绝', kind: 'reject_once' },
        ];

  return (
    <View className="rounded-lg border border-warning/40 bg-warning/10 p-md">
      <View className="flex-row items-center gap-xs">
        <SymbolView name="checkmark.shield.fill" size={15} tintColor={TINTS.warning} />
        <Text className="text-sm font-bold text-text dark:text-text-dark">需要授权</Text>
      </View>
      {title ? (
        <Text className="mt-xs text-xs text-text-muted dark:text-text-muted-dark">{title}</Text>
      ) : null}
      <CodeBlock label="详情" value={safeStringify(params)} />
      <View className="mt-sm flex-row flex-wrap gap-sm">
        {rendered.map((opt) => {
          const cls = optionClasses(opt.kind);
          return (
            <Pressable
              key={opt.optionId}
              onPress={() =>
                onResolve({ outcome: { outcome: 'selected', optionId: opt.optionId } })
              }
              className={`h-9 items-center justify-center rounded-md px-md active:opacity-80 ${cls.box}`}
            >
              <Text className={`text-sm font-semibold ${cls.text}`}>{opt.name}</Text>
            </Pressable>
          );
        })}
        <Pressable
          onPress={() => onResolve({ outcome: { outcome: 'cancelled' } })}
          className="h-9 items-center justify-center rounded-md px-md active:opacity-80"
        >
          <Text className="text-sm text-text-muted dark:text-text-muted-dark">取消</Text>
        </Pressable>
      </View>
    </View>
  );
}
