import {
  Alert,
  Anchor,
  Box,
  Button,
  Container,
  Group,
  Paper,
  PinInput,
  Stack,
  Tabs,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import {
  IconArrowRight,
  IconBrandGithub,
  IconCloudCheck,
  IconKey,
  IconLink,
} from '@tabler/icons-react';
import { useEffect, useState } from 'react';

import {
  PairingClient,
  RelayError,
  isAccountKeyShape,
} from '@coding-anywhere/protocol';

import { useConnection } from '../store/connection.js';

const DEFAULT_RELAY_URL = 'http://localhost:8443';
const DEVICE_LABEL_KEY = 'coding-anywhere/device-label';

interface FormState {
  relayURL: string;
  code: string;
  deviceLabel: string;
}

function suggestedDeviceLabel(): string {
  if (typeof window !== 'undefined') {
    const stored = window.localStorage.getItem(DEVICE_LABEL_KEY);
    if (stored) return stored;
  }
  if (typeof navigator !== 'undefined') {
    return navigator.userAgent.includes('Macintosh')
      ? 'Mac 浏览器'
      : navigator.userAgent.includes('iPhone')
        ? 'iPhone'
        : navigator.userAgent.includes('Android')
          ? 'Android 手机'
          : '网页浏览器';
  }
  return '网页浏览器';
}

export function PairScreen() {
  const signIn = useConnection((s) => s.signIn);
  const signInWithAccountKey = useConnection((s) => s.signInWithAccountKey);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(() => ({
    relayURL: DEFAULT_RELAY_URL,
    code: '',
    deviceLabel: suggestedDeviceLabel(),
  }));
  const [accountKey, setAccountKey] = useState('');

  useEffect(() => {
    if (form.deviceLabel) window.localStorage.setItem(DEVICE_LABEL_KEY, form.deviceLabel);
  }, [form.deviceLabel]);

  function handleError(err: unknown, fallback: string) {
    if (err instanceof RelayError) {
      setError(`${err.code}: ${err.message}`);
    } else if (err instanceof Error) {
      setError(err.message);
    } else {
      setError(fallback);
    }
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (form.code.length !== 6) {
      setError('请输入开发机上显示的 6 位配对码。');
      return;
    }
    if (!form.deviceLabel.trim()) {
      setError('给这台设备起个名字，方便以后认出它。');
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      const pc = new PairingClient(form.relayURL);
      const res = await pc.claim({
        code: form.code,
        device_kind: 'client',
        device_label: form.deviceLabel.trim(),
      });
      signIn({ relay_url: form.relayURL, bundle: res.credentials });
    } catch (err) {
      handleError(err, '配对失败。');
    } finally {
      setSubmitting(false);
    }
  }

  async function onLoginWithKey(e: React.FormEvent) {
    e.preventDefault();
    const key = accountKey.trim();
    if (!isAccountKeyShape(key)) {
      setError('账户密钥格式不对，应该形如 ca_key_...');
      return;
    }
    if (!form.deviceLabel.trim()) {
      setError('给这台设备起个名字，方便以后认出它。');
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      await signInWithAccountKey({
        relay_url: form.relayURL,
        account_key: key,
        device_label: form.deviceLabel.trim(),
      });
    } catch (err) {
      handleError(err, '登录失败。');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Box style={{ minHeight: '100dvh', background: 'var(--mantine-color-body)' }}>
      {/* Top hairline strip — date + brand */}
      <Box
        className="cw-hairline-b"
        px="lg"
        py={6}
        style={{
          fontSize: 11,
          letterSpacing: 2,
          textTransform: 'uppercase',
          color: 'var(--mantine-color-dimmed)',
          fontFamily: '"JetBrains Mono", "IBM Plex Mono", ui-monospace, monospace',
        }}
      >
        <Group justify="space-between">
          <Text inherit>CODING ANYWHERE · 远程开发控制台</Text>
          <Group gap={6}>
            <Box
              style={{
                width: 6,
                height: 6,
                borderRadius: 999,
                background: '#16a34a',
                boxShadow: '0 0 0 3px rgba(22,163,74,0.18)',
              }}
            />
            <Text inherit>RELAY · 在线</Text>
          </Group>
        </Group>
      </Box>

      <Container size="lg" pt={48} pb={64}>
        <Stack gap={28}>
          {/* Tag row */}
          <Group gap="xs">
            <span className="cw-tag-soft">WEB · BROWSER</span>
            <span className="cw-tag-outline">MIT LICENSED</span>
          </Group>

          {/* Hero title */}
          <h1 className="cw-hero-title">
            在任何设备，
            <br />
            掌控你的开发机。
          </h1>

          {/* Lead copy */}
          <Text size="lg" maw={760} c="dimmed" lh={1.55}>
            <span className="cw-accent">Coding Anywhere</span>{' '}
            通过一条出站 WebSocket
            把你的本地开发环境暴露在任何浏览器或手机上。它转发{' '}
            <span className="cw-accent">终端</span>、<span className="cw-accent">文件</span>、
            <span className="cw-accent">Git Diff</span>、
            <span className="cw-accent">本地端口</span>，
            并把对话路由给 Devin 等 ACP 智能体。一切流量经过你自己的 relay，凭据在浏览器本地。
          </Text>

          {/* CTAs */}
          <Group gap="sm" mt={4}>
            <Button
              component="a"
              href="#pair"
              size="md"
              rightSection={<IconArrowRight size={16} />}
              color="ink.9"
            >
              立即配对
            </Button>
            <Button
              size="md"
              variant="default"
              leftSection={<IconBrandGithub size={16} />}
              component="a"
              href="https://github.com/Mutech-Innovation/coding-anywhere"
              target="_blank"
              rel="noreferrer"
            >
              ★ STAR ON GITHUB
            </Button>
            <Anchor
              href="https://github.com/Mutech-Innovation/coding-anywhere#readme"
              target="_blank"
              size="sm"
            >
              阅读文档
            </Anchor>
          </Group>
          <Text size="xs" c="dimmed">
            独立维护者 · Mutech Innovation
          </Text>

          {/* Daemon install snippet */}
          <Stack gap={6} mt={8}>
            <Text className="cw-eyebrow">
              开始使用 <span style={{ color: 'var(--mantine-color-dimmed)' }}>·</span>{' '}
              在开发机运行
            </Text>
            <Box className="cw-code-block">
              <div>
                <span className="cw-code-comment"># 在开发机安装 daemon</span>
              </div>
              <div>
                <span className="cw-code-prompt">$</span>brew install coding-anywhere
              </div>
              <div>
                <span className="cw-code-prompt">$</span>coding-anywhere init --relay-url{' '}
                <span className="cw-code-path">{form.relayURL || 'https://relay.example.com'}</span>
              </div>
              <div style={{ height: 8 }} />
              <div>
                <span className="cw-code-comment"># 复制开发机打印的 6 位配对码，粘贴到右侧 →</span>
              </div>
            </Box>
            <Text size="xs" c="dimmed">
              凭据保存在浏览器本地（localStorage），不会上传到第三方。
            </Text>
          </Stack>

          {/* Auth card (key login | first-time pairing) */}
          <Paper
            id="pair"
            className="cw-hairline"
            withBorder={false}
            radius="md"
            p="xl"
            mt={16}
          >
            <Stack gap="lg">
              <Group gap="xs">
                <IconCloudCheck size={20} color="var(--mantine-color-brand-8)" />
                <Title order={3} fw={700}>
                  接入这台浏览器
                </Title>
              </Group>
              <Tabs defaultValue="key" variant="default" classNames={{ list: 'cw-tabs' }}>
                <Tabs.List>
                  <Tabs.Tab value="key" leftSection={<IconKey size={14} />}>
                    使用账户密钥
                  </Tabs.Tab>
                  <Tabs.Tab value="code" leftSection={<IconCloudCheck size={14} />}>
                    首次配对（输入 6 位码）
                  </Tabs.Tab>
                </Tabs.List>

                <Tabs.Panel value="key" pt="md">
                  <form onSubmit={onLoginWithKey}>
                    <Stack gap="md">
                      <TextInput
                        label="Relay URL"
                        description="与开发机 daemon 指向同一个 relay。"
                        leftSection={<IconLink size={16} />}
                        value={form.relayURL}
                        onChange={(e) => setForm({ ...form, relayURL: e.currentTarget.value })}
                        placeholder="https://relay.example.com"
                        required
                      />
                      <TextInput
                        label="账户密钥"
                        description={
                          <Text component="span" size="xs" c="dimmed">
                            形如 <code>ca_key_...</code>；在开发机运行{' '}
                            <code>coding-anywhere-daemon key show</code> 可以打印出来。
                          </Text>
                        }
                        leftSection={<IconKey size={16} />}
                        value={accountKey}
                        onChange={(e) => setAccountKey(e.currentTarget.value)}
                        placeholder="ca_key_..."
                        required
                        autoComplete="current-password"
                        type="password"
                      />
                      <TextInput
                        label="设备名称"
                        description="会出现在你的设备列表里。"
                        value={form.deviceLabel}
                        onChange={(e) =>
                          setForm({ ...form, deviceLabel: e.currentTarget.value })
                        }
                        required
                      />
                      {error ? (
                        <Alert color="red" variant="light" radius="md">
                          {error}
                        </Alert>
                      ) : null}
                      <Group justify="space-between" mt={4}>
                        <Anchor
                          size="sm"
                          href="https://github.com/Mutech-Innovation/coding-anywhere"
                        >
                          帮助与文档
                        </Anchor>
                        <Button
                          type="submit"
                          loading={submitting}
                          rightSection={<IconArrowRight size={16} />}
                          color="ink.9"
                        >
                          登录账户
                        </Button>
                      </Group>
                    </Stack>
                  </form>
                </Tabs.Panel>

                <Tabs.Panel value="code" pt="md">
                  <form onSubmit={onSubmit}>
                    <Stack gap="md">
                      <TextInput
                        label="Relay URL"
                        description="与开发机 daemon 指向同一个 relay。"
                        leftSection={<IconLink size={16} />}
                        value={form.relayURL}
                        onChange={(e) => setForm({ ...form, relayURL: e.currentTarget.value })}
                        placeholder="https://relay.example.com"
                        required
                      />
                      <Stack gap={6}>
                        <Text size="sm" fw={600}>
                          配对码
                        </Text>
                        <PinInput
                          length={6}
                          type="number"
                          inputType="tel"
                          oneTimeCode
                          size="lg"
                          value={form.code}
                          onChange={(value) => setForm({ ...form, code: value })}
                          aria-label="配对码"
                        />
                        <Text size="xs" c="dimmed">
                          开发机首次运行 <code>coding-anywhere-daemon init</code>{' '}
                          时会在终端打印这串数字。
                        </Text>
                      </Stack>
                      <TextInput
                        label="设备名称"
                        description="会出现在你的设备列表里。"
                        value={form.deviceLabel}
                        onChange={(e) =>
                          setForm({ ...form, deviceLabel: e.currentTarget.value })
                        }
                        required
                      />
                      {error ? (
                        <Alert color="red" variant="light" radius="md">
                          {error}
                        </Alert>
                      ) : null}
                      <Group justify="space-between" mt={4}>
                        <Anchor
                          size="sm"
                          href="https://github.com/Mutech-Innovation/coding-anywhere"
                        >
                          帮助与文档
                        </Anchor>
                        <Button
                          type="submit"
                          loading={submitting}
                          rightSection={<IconArrowRight size={16} />}
                          color="ink.9"
                        >
                          开始配对
                        </Button>
                      </Group>
                    </Stack>
                  </form>
                </Tabs.Panel>
              </Tabs>
            </Stack>
          </Paper>
        </Stack>
      </Container>

      {/* Bottom 3-col CTA */}
      <Box className="cw-inverse" py={56} mt={32}>
        <Container size="lg">
          <Stack gap="md">
            <Title order={2} c="white" fw={800} style={{ letterSpacing: '-0.01em' }}>
              这是一个开放的小项目。
            </Title>
            <Text c="rgba(255,255,255,0.7)" maw={680}>
              无 CLA、无赞助商锁定。维护者亲自阅读每一条 issue。版本从 main 分支发布。
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
