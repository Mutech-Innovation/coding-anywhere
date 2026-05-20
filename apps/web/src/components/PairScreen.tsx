import {
  Alert,
  Anchor,
  Button,
  Container,
  Group,
  Paper,
  PinInput,
  Stack,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import { IconCloudCheck, IconLink } from '@tabler/icons-react';
import { useEffect, useState } from 'react';

import { PairingClient, RelayError } from '@coding-anywhere/protocol';

import { useConnection } from '../store/connection.js';

const DEFAULT_RELAY_URL = 'http://localhost:8443';
const DEVICE_LABEL_KEY = 'coding-anywhere/device-label';

interface FormState {
  relayURL: string;
  code: string;
  deviceLabel: string;
}

function suggestedDeviceLabel(): string {
  // Try to use the last label entered, otherwise something sensible.
  if (typeof window !== 'undefined') {
    const stored = window.localStorage.getItem(DEVICE_LABEL_KEY);
    if (stored) return stored;
  }
  if (typeof navigator !== 'undefined') {
    return navigator.userAgent.includes('Macintosh')
      ? 'Mac browser'
      : navigator.userAgent.includes('iPhone')
        ? 'iPhone'
        : navigator.userAgent.includes('Android')
          ? 'Android phone'
          : 'Web browser';
  }
  return 'Web browser';
}

export function PairScreen() {
  const signIn = useConnection((s) => s.signIn);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(() => ({
    relayURL: DEFAULT_RELAY_URL,
    code: '',
    deviceLabel: suggestedDeviceLabel(),
  }));

  useEffect(() => {
    if (form.deviceLabel) window.localStorage.setItem(DEVICE_LABEL_KEY, form.deviceLabel);
  }, [form.deviceLabel]);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (form.code.length !== 6) {
      setError('Enter the 6-digit code shown on your dev machine.');
      return;
    }
    if (!form.deviceLabel.trim()) {
      setError('Give this device a label so you can recognise it later.');
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
      if (err instanceof RelayError) {
        setError(`${err.code}: ${err.message}`);
      } else if (err instanceof Error) {
        setError(err.message);
      } else {
        setError('Pairing failed.');
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Container size="sm" py="xl">
      <Stack gap="md">
        <Group gap="xs">
          <IconCloudCheck size={28} color="var(--mantine-color-brand-5)" />
          <Title order={2}>Pair this browser</Title>
        </Group>
        <Text c="dimmed">
          On your development machine run <code>coding-anywhere init --relay-url …</code>. It will
          print a 6-digit pairing code. Enter the code below to link this browser.
        </Text>
        <Paper withBorder p="lg" radius="md">
          <form onSubmit={onSubmit}>
            <Stack gap="md">
              <TextInput
                label="Relay URL"
                description="The same URL you point your daemon at."
                leftSection={<IconLink size={16} />}
                value={form.relayURL}
                onChange={(e) => setForm({ ...form, relayURL: e.currentTarget.value })}
                placeholder="https://relay.example.com"
                required
              />
              <Stack gap={4}>
                <Text size="sm" fw={500}>
                  Pairing code
                </Text>
                <PinInput
                  length={6}
                  type="number"
                  inputType="tel"
                  oneTimeCode
                  size="lg"
                  value={form.code}
                  onChange={(value) => setForm({ ...form, code: value })}
                  aria-label="Pairing code"
                />
              </Stack>
              <TextInput
                label="Device label"
                description="Shown in your device list."
                value={form.deviceLabel}
                onChange={(e) => setForm({ ...form, deviceLabel: e.currentTarget.value })}
                required
              />
              {error ? (
                <Alert color="red" variant="light" radius="md">
                  {error}
                </Alert>
              ) : null}
              <Group justify="space-between">
                <Anchor size="sm" href="https://github.com/Mutech-Innovation/coding-anywhere">
                  Help & docs
                </Anchor>
                <Button type="submit" loading={submitting}>
                  Pair
                </Button>
              </Group>
            </Stack>
          </form>
        </Paper>
      </Stack>
    </Container>
  );
}
