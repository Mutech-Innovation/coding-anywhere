import { Anchor, Box, Container, Group, Stack, Text, Title } from '@mantine/core';
import {
  IconArrowRight,
  IconBrandGithub,
  IconCode,
  IconFiles,
  IconGitCompare,
  IconMessageCircle,
  IconServer,
  IconTerminal2,
} from '@tabler/icons-react';
import { useMemo } from 'react';

import { useConnection } from '../store/connection.js';
import { useNotifications } from '../store/notifications.js';
import { useTerminals } from '../store/terminals.js';

interface WelcomeTabProps {
  onNavigate: (tab: string) => void;
}

const TICKER_ITEMS = [
  { kind: 'PR', no: '7', title: 'feat(web): CodeWhale-style refresh', time: 'now' },
  { kind: 'PR', no: '6', title: 'feat(daemon): persistent credentials', time: '2h' },
  { kind: 'ISS', no: '12', title: 'iOS Safari WebSocket reconnect storm', time: '3h' },
  { kind: 'PR', no: '5', title: 'fix(relay): JWT refresh skew', time: '4h' },
  { kind: 'ISS', no: '9', title: 'Diff viewer wraps long lines incorrectly', time: '5h' },
  { kind: 'PR', no: '4', title: 'feat(ports): preview localhost via relay proxy', time: '8h' },
  { kind: 'ISS', no: '8', title: 'TerminalsPanel CJK width off-by-one', time: '1d' },
  { kind: 'PR', no: '3', title: 'feat(chat): ACP wiring for Devin', time: '1d' },
];

export function WelcomeTab({ onNavigate }: WelcomeTabProps) {
  const credentials = useConnection((s) => s.credentials);
  const status = useConnection((s) => s.status);
  const sessions = useTerminals((s) => s.sessions);
  const pushState = useNotifications((s) => s.state);

  const stats = useMemo(() => {
    const sessionCount = Object.keys(sessions).length;
    return [
      {
        eyebrow: '设备 · DEVICE',
        value: credentials ? '1' : '0',
        sub: credentials?.bundle.device_label ?? '未配对',
      },
      {
        eyebrow: '终端 · TERMINALS',
        value: String(sessionCount),
        sub: sessionCount > 0 ? '会话进行中' : '无活跃会话',
      },
      {
        eyebrow: '推送 · PUSH',
        value: pushState === 'subscribed' ? 'ON' : 'OFF',
        sub: pushState === 'subscribed' ? '订阅中' : '未启用',
      },
      {
        eyebrow: '版本 · LATEST',
        value: 'v0.1',
        sub: 'main 分支',
      },
    ];
  }, [credentials, sessions, pushState]);

  return (
    <Box>
      {/* Top ticker */}
      <div className="cw-ticker">
        <div className="cw-ticker-tag">
          <span>
            {status === 'connected' ? '实时 LIVE' : status === 'connecting' ? '连接中' : '离线'}
          </span>
        </div>
        <div className="cw-ticker-track">
          <div className="cw-ticker-inner">
            {[...TICKER_ITEMS, ...TICKER_ITEMS].map((it, i) => (
              <span className="cw-ticker-item" key={`${i}-${it.kind}-${it.no}`}>
                <span className="cw-ticker-kind">
                  {it.kind}#{it.no}
                </span>
                <span>{it.title}</span>
                <span className="cw-ticker-time">· {it.time}</span>
                <span className="cw-ticker-sep">◆</span>
              </span>
            ))}
          </div>
        </div>
      </div>

      <Container size="lg" py={56}>
        <Stack gap={36}>
          {/* Hero */}
          <Stack gap="md">
            <Group gap="xs">
              <span className="cw-tag-soft">
                {status === 'connected' ? 'RELAY · 在线' : 'RELAY · ' + status.toUpperCase()}
              </span>
              <span className="cw-tag-outline">MIT LICENSED</span>
            </Group>
            <h1 className="cw-hero-title">
              一个浏览器，
              <br />
              一台开发机，零配置随行。
            </h1>
            <Text size="lg" maw={760} c="dimmed" lh={1.55}>
              <span className="cw-accent">Coding Anywhere</span>{' '}
              通过一条出站 WebSocket 把你的本地终端、文件、Diff、端口和{' '}
              <span className="cw-accent">ACP 智能体</span>{' '}
              一起送到任何浏览器或手机。相同的会话、相同的环境，在哪都能打开。
            </Text>
            <Group gap="sm">
              <PrimaryButton onClick={() => onNavigate('terminals')}>
                打开终端
                <IconArrowRight size={16} />
              </PrimaryButton>
              <SecondaryButton onClick={() => onNavigate('chat')}>
                进入对话
              </SecondaryButton>
              <Anchor
                href="https://github.com/Mutech-Innovation/coding-anywhere"
                target="_blank"
                size="sm"
              >
                <Group gap={6}>
                  <IconBrandGithub size={14} />
                  <span>GitHub</span>
                </Group>
              </Anchor>
            </Group>
          </Stack>

          {/* Stats grid */}
          <Box className="cw-feature-grid cw-feature-grid--four">
            {stats.map((s) => (
              <Stack key={s.eyebrow} gap={6} p="lg" justify="space-between">
                <Text className="cw-eyebrow">{s.eyebrow}</Text>
                <span className="cw-bignum">{s.value}</span>
                <Text size="xs" c="dimmed">
                  {s.sub}
                </Text>
              </Stack>
            ))}
          </Box>

          {/* "它到底是什么" section */}
          <SectionTitle char="是" title="它到底是什么" />
          <Box className="cw-feature-grid">
            <FeatureCard
              index="01"
              eyebrow="对话 · CHAT"
              title="把智能体接进来"
              desc="统一 ACP 通道，让 Devin、Codex 等智能体直接在你的开发机里读、改、跑。"
              icon={<IconMessageCircle size={18} />}
              onClick={() => onNavigate('chat')}
              cta="打开对话"
            />
            <FeatureCard
              index="02"
              eyebrow="终端 · TERMINALS"
              title="完整 PTY，不是 readline"
              desc="多会话复用同一条 WebSocket，CJK 宽度、color、resize 全都保留。"
              icon={<IconTerminal2 size={18} />}
              onClick={() => onNavigate('terminals')}
              cta="打开终端"
            />
            <FeatureCard
              index="03"
              eyebrow="文件 · FILES"
              title="远端目录，本地手感"
              desc="浏览、读取、保存。支持 CodeMirror 语法高亮和实时 diff。"
              icon={<IconFiles size={18} />}
              onClick={() => onNavigate('files')}
              cta="浏览文件"
            />
          </Box>

          <Box className="cw-feature-grid">
            <FeatureCard
              index="04"
              eyebrow="DIFF"
              title="一眼看清这次改了什么"
              desc="基于 git 仓库的实时 diff，左右对照、按 chunk 跳转。"
              icon={<IconGitCompare size={18} />}
              onClick={() => onNavigate('diff')}
              cta="查看 Diff"
            />
            <FeatureCard
              index="05"
              eyebrow="端口 · PORTS"
              title="把 localhost 带在身上"
              desc="开发机的 3000/5173/8080 经 relay 反向代理，手机也能直接预览。"
              icon={<IconServer size={18} />}
              onClick={() => onNavigate('ports')}
              cta="管理端口"
            />
            <FeatureCard
              index="06"
              eyebrow="AGENTS"
              title="按需挂载的智能体"
              desc="Devin 一等公民，OpenCode、Cursor agent、自定义后端按需切换。"
              icon={<IconCode size={18} />}
              onClick={() => onNavigate('agents')}
              cta="查看 Agents"
            />
          </Box>
        </Stack>
      </Container>

      {/* Dark CTA section */}
      <Box className="cw-inverse" py={56}>
        <Container size="lg">
          <Stack gap="md">
            <Title order={2} c="white" fw={800} style={{ letterSpacing: '-0.01em' }}>
              这是一个小项目。你的每个补丁都很重要。
            </Title>
            <Text c="rgba(255,255,255,0.7)" maw={680}>
              无 CLA、无赞助商锁定。Issue 在公开环境下分类，版本从 main 分支发布。
            </Text>
            <Box mt={16} className="cw-feature-grid" style={{ border: 'none' }}>
              <CtaCard
                eyebrow="提 Bug 或功能建议"
                title="提交 Issue"
                desc="Bug 报告、功能需求、或一个好问题。"
                href="https://github.com/Mutech-Innovation/coding-anywhere/issues"
              />
              <CtaCard
                eyebrow="贡献代码"
                title="提交 PR"
                desc="Fork、开分支、Conventional Commit、提交 PR。"
                href="https://github.com/Mutech-Innovation/coding-anywhere/pulls"
              />
              <CtaCard
                eyebrow="参与设计"
                title="发起讨论"
                desc="架构、路线图、任何非 Bug 话题。"
                href="https://github.com/Mutech-Innovation/coding-anywhere/discussions"
              />
            </Box>
          </Stack>
        </Container>
      </Box>
    </Box>
  );
}

function SectionTitle({ char, title }: { char: string; title: string }) {
  return (
    <Group gap="md" mt={16}>
      <Box
        style={{
          width: 44,
          height: 44,
          borderRadius: 6,
          background: 'light-dark(#0e0e10, #e7ecf3)',
          color: 'light-dark(#fff, #0b0f17)',
          display: 'grid',
          placeItems: 'center',
          fontSize: 20,
          fontWeight: 700,
        }}
        aria-hidden
      >
        {char}
      </Box>
      <Title order={2} fw={800} style={{ letterSpacing: '-0.01em' }}>
        {title}
      </Title>
    </Group>
  );
}

interface FeatureCardProps {
  index: string;
  eyebrow: string;
  title: string;
  desc: string;
  icon: React.ReactNode;
  onClick?: () => void;
  cta: string;
}

function FeatureCard({ index, eyebrow, title, desc, icon, onClick, cta }: FeatureCardProps) {
  return (
    <Box
      p="lg"
      style={{ cursor: onClick ? 'pointer' : 'default' }}
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
    >
      <Group justify="space-between" mb={20}>
        <Group gap={8}>
          <Text className="cw-eyebrow">{index}</Text>
          <Text className="cw-eyebrow" style={{ color: 'var(--mantine-color-dimmed)' }}>
            ·
          </Text>
          <Text className="cw-eyebrow">{eyebrow}</Text>
        </Group>
        <Box c="dimmed">{icon}</Box>
      </Group>
      <Title order={3} fw={700} style={{ letterSpacing: '-0.005em' }}>
        {title}
      </Title>
      <Text size="sm" c="dimmed" mt={10} lh={1.6}>
        {desc}
      </Text>
      {onClick ? (
        <Group gap={4} mt={16} style={{ color: 'var(--mantine-color-brand-8)' }}>
          <Text size="sm" fw={600} c="brand.8">
            {cta}
          </Text>
          <IconArrowRight size={14} color="var(--mantine-color-brand-8)" />
        </Group>
      ) : null}
    </Box>
  );
}

function CtaCard(props: { eyebrow: string; title: string; desc: string; href: string }) {
  return (
    <Box p="lg">
      <Text
        size="xs"
        fw={700}
        style={{
          letterSpacing: '0.1em',
          color: 'var(--mantine-color-brand-3)',
          textTransform: 'uppercase',
        }}
      >
        {props.eyebrow}
      </Text>
      <Title order={3} c="white" mt={6} fw={700}>
        {props.title}
      </Title>
      <Text c="rgba(255,255,255,0.65)" size="sm" mt={6}>
        {props.desc}
      </Text>
      <Anchor href={props.href} target="_blank" mt={14} c="white" size="sm" inline>
        <Group gap={4} display="inline-flex">
          <span style={{ fontWeight: 600 }}>前往</span>
          <IconArrowRight size={14} />
        </Group>
      </Anchor>
    </Box>
  );
}

/** Solid primary button (CodeWhale-style: black in light, white in dark). */
function PrimaryButton({
  children,
  onClick,
}: {
  children: React.ReactNode;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 8,
        padding: '12px 20px',
        background: 'light-dark(#0e0e10, #e7ecf3)',
        color: 'light-dark(#fff, #0b0f17)',
        border: '1px solid light-dark(#0e0e10, #e7ecf3)',
        borderRadius: 6,
        fontSize: 14,
        fontWeight: 600,
        cursor: 'pointer',
        letterSpacing: 0.1,
      }}
    >
      {children}
    </button>
  );
}

function SecondaryButton({
  children,
  onClick,
}: {
  children: React.ReactNode;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 8,
        padding: '12px 20px',
        background: 'transparent',
        color: 'var(--mantine-color-text)',
        border: '1px solid var(--mantine-color-default-border)',
        borderRadius: 6,
        fontSize: 14,
        fontWeight: 600,
        cursor: 'pointer',
        letterSpacing: 0.1,
      }}
    >
      {children}
    </button>
  );
}
