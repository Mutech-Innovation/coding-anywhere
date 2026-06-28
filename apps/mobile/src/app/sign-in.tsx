import {
  isAccountKeyShape,
  PairingClient,
  RelayError,
  type UserAccountSummary,
} from '@coding-anywhere/protocol';
import * as Device from 'expo-device';
import { useState } from 'react';
import { KeyboardAvoidingView, Platform, Pressable, ScrollView, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { Button, Card, ErrorBanner, SegmentedControl, TextField } from '../components/ui';
import { useConnection } from '../store/connection';

const DEFAULT_RELAY_URL = 'http://localhost:8443';

type Method = 'password' | 'key' | 'code';

function suggestedDeviceLabel(): string {
  return Device.deviceName || Device.modelName || 'iPhone';
}

export default function SignIn() {
  const signIn = useConnection((s) => s.signIn);
  const signInWithAccountKey = useConnection((s) => s.signInWithAccountKey);
  const signInWithPassword = useConnection((s) => s.signInWithPassword);

  const [method, setMethod] = useState<Method>('password');
  const [relayURL, setRelayURL] = useState(DEFAULT_RELAY_URL);
  const [deviceLabel, setDeviceLabel] = useState(suggestedDeviceLabel);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [accountKey, setAccountKey] = useState('');
  const [code, setCode] = useState('');
  const [accountChoices, setAccountChoices] = useState<UserAccountSummary[] | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function handleError(err: unknown, fallback: string) {
    if (err instanceof RelayError) setError(`${err.code}: ${err.message}`);
    else if (err instanceof Error) setError(err.message);
    else setError(fallback);
  }

  async function onLoginWithPassword() {
    if (!username.trim()) return setError('请输入用户名。');
    if (!password) return setError('请输入密码。');
    if (!deviceLabel.trim()) return setError('给这台设备起个名字，方便以后认出它。');
    setError(null);
    setSubmitting(true);
    try {
      const { accounts, signedIn } = await signInWithPassword({
        relay_url: relayURL,
        username: username.trim(),
        password,
        device_label: deviceLabel.trim(),
      });
      if (!signedIn) setAccountChoices(accounts);
    } catch (err) {
      handleError(err, '登录失败。');
    } finally {
      setSubmitting(false);
    }
  }

  async function onPickAccount(accountId: string) {
    setError(null);
    setSubmitting(true);
    try {
      await signInWithPassword({
        relay_url: relayURL,
        username: username.trim(),
        password,
        device_label: deviceLabel.trim(),
        account_id: accountId,
      });
    } catch (err) {
      handleError(err, '登录失败。');
    } finally {
      setSubmitting(false);
    }
  }

  async function onLoginWithKey() {
    const key = accountKey.trim();
    if (!isAccountKeyShape(key)) return setError('账户密钥格式不对，应该形如 ca_key_...');
    if (!deviceLabel.trim()) return setError('给这台设备起个名字，方便以后认出它。');
    setError(null);
    setSubmitting(true);
    try {
      await signInWithAccountKey({
        relay_url: relayURL,
        account_key: key,
        device_label: deviceLabel.trim(),
      });
    } catch (err) {
      handleError(err, '登录失败。');
    } finally {
      setSubmitting(false);
    }
  }

  async function onPairCode() {
    if (code.length !== 6) return setError('请输入开发机上显示的 6 位配对码。');
    if (!deviceLabel.trim()) return setError('给这台设备起个名字，方便以后认出它。');
    setError(null);
    setSubmitting(true);
    try {
      const pc = new PairingClient(relayURL);
      const res = await pc.claim({
        code,
        device_kind: 'client',
        device_label: deviceLabel.trim(),
      });
      await signIn({ relay_url: relayURL, bundle: res.credentials });
    } catch (err) {
      handleError(err, '配对失败。');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <SafeAreaView className="flex-1 bg-surface dark:bg-surface-dark">
      <KeyboardAvoidingView
        className="flex-1"
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <ScrollView
          contentContainerClassName="grow justify-center gap-lg p-xl"
          keyboardShouldPersistTaps="handled"
        >
          <View className="gap-xs">
            <Text className="text-xxl font-bold text-text dark:text-text-dark">
              Coding Anywhere
            </Text>
            <Text className="text-sm text-text-muted dark:text-text-muted-dark">
              接入你的开发机 · 远程开发控制台
            </Text>
          </View>

          <Card className="gap-md p-lg">
            <SegmentedControl<Method>
              value={method}
              onChange={(m) => {
                setMethod(m);
                setError(null);
                setAccountChoices(null);
              }}
              options={[
                { value: 'password', label: '密码' },
                { value: 'key', label: '账户密钥' },
                { value: 'code', label: '配对码' },
              ]}
            />

            <TextField
              label="Relay URL"
              description="与开发机 daemon 指向同一个 relay。"
              value={relayURL}
              onChangeText={setRelayURL}
              placeholder="https://relay.example.com"
              autoCapitalize="none"
              autoCorrect={false}
              keyboardType="url"
            />

            {method === 'password' && accountChoices ? (
              <View className="gap-sm">
                <Text className="text-sm font-semibold text-text dark:text-text-dark">
                  选择要连接的机器
                </Text>
                <Text className="text-xs text-text-muted dark:text-text-muted-dark">
                  你的账户下有多台开发机，点选一台进入。
                </Text>
                {accountChoices.map((a) => (
                  <Pressable
                    key={a.account_id}
                    disabled={submitting}
                    onPress={() => onPickAccount(a.account_id)}
                    className="rounded-md border border-border p-md active:opacity-80 dark:border-border-dark"
                  >
                    <Text className="text-sm font-semibold text-text dark:text-text-dark">
                      {a.label}
                    </Text>
                    <Text className="text-xs text-text-muted dark:text-text-muted-dark">
                      {a.account_id}
                    </Text>
                  </Pressable>
                ))}
                <ErrorBanner message={error} />
                <Pressable
                  onPress={() => {
                    setAccountChoices(null);
                    setError(null);
                  }}
                >
                  <Text className="text-sm text-accent dark:text-accent-dark">← 换个账户</Text>
                </Pressable>
              </View>
            ) : (
              <>
                {method === 'password' && (
                  <>
                    <TextField
                      label="用户名"
                      description="在开发机运行 coding-anywhere-daemon user register 创建。"
                      value={username}
                      onChangeText={setUsername}
                      placeholder="me"
                      autoCapitalize="none"
                      autoCorrect={false}
                      textContentType="username"
                    />
                    <TextField
                      label="密码"
                      value={password}
                      onChangeText={setPassword}
                      secureTextEntry
                      autoCapitalize="none"
                      textContentType="password"
                    />
                  </>
                )}

                {method === 'key' && (
                  <TextField
                    label="账户密钥"
                    description="形如 ca_key_...；在开发机运行 coding-anywhere-daemon key show 打印。"
                    value={accountKey}
                    onChangeText={setAccountKey}
                    placeholder="ca_key_..."
                    secureTextEntry
                    autoCapitalize="none"
                    autoCorrect={false}
                  />
                )}

                {method === 'code' && (
                  <TextField
                    label="配对码"
                    description="开发机运行 init 后终端打印的 6 位码。"
                    value={code}
                    onChangeText={(t) => setCode(t.replace(/\D/g, '').slice(0, 6))}
                    placeholder="000000"
                    keyboardType="number-pad"
                    maxLength={6}
                  />
                )}

                <TextField
                  label="设备名称"
                  description="会出现在你的设备列表里。"
                  value={deviceLabel}
                  onChangeText={setDeviceLabel}
                />

                <ErrorBanner message={error} />

                <Button
                  title={method === 'code' ? '配对' : '登录'}
                  loading={submitting}
                  onPress={
                    method === 'password'
                      ? onLoginWithPassword
                      : method === 'key'
                        ? onLoginWithKey
                        : onPairCode
                  }
                />
              </>
            )}
          </Card>
        </ScrollView>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
