import {
  ActionIcon,
  Badge,
  Box,
  Group,
  Paper,
  ScrollArea,
  Stack,
  Text,
  Textarea,
  Tooltip,
} from '@mantine/core';
import { IconPlayerStopFilled, IconSend, IconTool } from '@tabler/icons-react';
import { useEffect, useMemo, useRef, useState } from 'react';

import { type ChatMessage, type ChatToolCall, useChat } from '../store/chat.js';

type TranscriptItem =
  | { kind: 'msg'; order: number; msg: ChatMessage }
  | { kind: 'tool'; order: number; tool: ChatToolCall };

/**
 * GuestChat is the minimal, read-only chat surface used by the public "ask"
 * share page. It reuses the full `useChat` store (streaming, ingest, history)
 * but renders only a transcript + composer — no session sidebar, mode/model
 * pickers, permission cards, or attachments.
 */
export function GuestChat({ sessionId, disabled }: { sessionId: string; disabled?: boolean }) {
  const session = useChat((s) => s.sessions[sessionId]);
  const sendPrompt = useChat((s) => s.sendPrompt);
  const cancel = useChat((s) => s.cancel);
  const [text, setText] = useState('');
  const viewportRef = useRef<HTMLDivElement>(null);

  const status = session?.session.status;
  const thinking = status === 'thinking' || status === 'awaiting_permission';

  const items = useMemo<TranscriptItem[]>(() => {
    if (!session) return [];
    const out: TranscriptItem[] = [
      ...session.messages.map((msg) => ({ kind: 'msg' as const, order: msg.order, msg })),
      ...Object.values(session.toolCalls).map((tool) => ({
        kind: 'tool' as const,
        order: tool.order,
        tool,
      })),
    ];
    out.sort((a, b) => a.order - b.order);
    return out;
  }, [session]);

  useEffect(() => {
    const el = viewportRef.current;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
  }, [items.length, thinking]);

  if (!session) return null;

  const canSend = !disabled && !thinking && text.trim().length > 0;
  const submit = () => {
    const value = text.trim();
    if (!value || disabled) return;
    setText('');
    void sendPrompt(sessionId, value);
  };

  return (
    <Stack gap={0} h="100%" style={{ minHeight: 0 }}>
      <ScrollArea style={{ flex: 1, minHeight: 0 }} viewportRef={viewportRef}>
        <Stack gap="sm" p="md" maw={820} mx="auto">
          {items.length === 0 ? (
            <Text c="dimmed" size="sm" ta="center" py="xl">
              问点什么吧 —— 这是一个只读（ask）会话，Devin 不会改动任何文件。
            </Text>
          ) : (
            items.map((item) =>
              item.kind === 'msg' ? (
                <MessageBubble key={item.msg.id} msg={item.msg} />
              ) : (
                <ToolLine key={item.tool.id} tool={item.tool} />
              ),
            )
          )}
          {thinking ? (
            <Text c="dimmed" size="sm" fs="italic">
              Devin 正在思考…
            </Text>
          ) : null}
        </Stack>
      </ScrollArea>

      <Box className="cw-hairline-t" p="sm">
        <Group gap="xs" align="flex-end" maw={820} mx="auto" w="100%">
          <Textarea
            flex={1}
            autosize
            minRows={1}
            maxRows={6}
            placeholder={disabled ? '会话已结束' : '给 Devin 发消息…'}
            value={text}
            disabled={disabled}
            onChange={(e) => setText(e.currentTarget.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                submit();
              }
            }}
          />
          {thinking ? (
            <Tooltip label="停止">
              <ActionIcon
                size="lg"
                color="red"
                variant="light"
                onClick={() => void cancel(sessionId)}
              >
                <IconPlayerStopFilled size={18} />
              </ActionIcon>
            </Tooltip>
          ) : (
            <Tooltip label="发送">
              <ActionIcon size="lg" variant="filled" disabled={!canSend} onClick={submit}>
                <IconSend size={18} />
              </ActionIcon>
            </Tooltip>
          )}
        </Group>
      </Box>
    </Stack>
  );
}

function MessageBubble({ msg }: { msg: ChatMessage }) {
  if (msg.role === 'user') {
    return (
      <Group justify="flex-end">
        <Paper bg="brand.6" c="white" px="md" py="xs" radius="md" maw="80%">
          <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>
            {msg.text}
          </Text>
        </Paper>
      </Group>
    );
  }
  if (msg.role === 'agent_thought') {
    return (
      <Text c="dimmed" size="sm" fs="italic" style={{ whiteSpace: 'pre-wrap' }}>
        {msg.text}
      </Text>
    );
  }
  if (msg.role === 'system') {
    return (
      <Text c="dimmed" size="xs" ta="center" style={{ whiteSpace: 'pre-wrap' }}>
        {msg.text}
      </Text>
    );
  }
  return (
    <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>
      {msg.text}
    </Text>
  );
}

function ToolLine({ tool }: { tool: ChatToolCall }) {
  return (
    <Group gap={6} c="dimmed">
      <IconTool size={14} />
      <Text size="xs">{tool.title ?? tool.kind ?? '工具调用'}</Text>
      {tool.status ? (
        <Badge size="xs" variant="light" color={tool.status === 'failed' ? 'red' : 'gray'}>
          {tool.status}
        </Badge>
      ) : null}
    </Group>
  );
}
