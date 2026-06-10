import { ActionIcon, Box, Group, ScrollArea, Stack, Text, Textarea, Tooltip } from '@mantine/core';
import { IconPlayerStopFilled, IconSend } from '@tabler/icons-react';
import { useEffect, useMemo, useRef, useState } from 'react';

import { type ChatMessage, type ChatToolCall, useChat } from '../store/chat.js';
import { MessageBubble, ToolCallCard } from './ChatPanel.js';

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

  // The relay stamps the share's owner-chosen agent onto the session; show
  // its name so a cursor-agent share doesn't claim to be Devin.
  const agentName = guestAgentName(session.session.agent_id);
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
            <Stack gap={4} align="center" py="xl">
              <Text c="dimmed" size="sm" ta="center">
                问点什么吧 —— 这是一个只读（ask）会话，{agentName} 不会改动任何文件。
              </Text>
              <Text c="dimmed" size="xs" ta="center">
                提示：对话只保留在本页，刷新或关闭后会开始新的对话。
              </Text>
            </Stack>
          ) : (
            items.map((item) =>
              item.kind === 'msg' ? (
                <MessageBubble key={item.msg.id} message={item.msg} />
              ) : (
                <ToolCallCard key={item.tool.id} call={item.tool} />
              ),
            )
          )}
          {thinking ? (
            <Text c="dimmed" size="sm" fs="italic">
              {agentName} 正在思考…
            </Text>
          ) : null}
        </Stack>
      </ScrollArea>

      <Box className="cw-hairline-t" p="sm">
        <Stack gap={6} maw={820} mx="auto" w="100%">
          <Group gap="xs" align="flex-end" w="100%">
            <Textarea
              flex={1}
              autosize
              minRows={1}
              maxRows={6}
              placeholder={disabled ? '会话已结束' : `给 ${agentName} 发消息…`}
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
          <Text c="dimmed" size="xs" ta="center">
            对话不会保存，刷新或关闭页面后会开始新的对话。
          </Text>
        </Stack>
      </Box>
    </Stack>
  );
}

// guestAgentName renders the session's agent id for guest-facing copy.
function guestAgentName(agentId: string): string {
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
