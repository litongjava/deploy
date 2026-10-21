import { t } from '@/i18n';
import type {
  ConcurrencyType,
  CredentialType,
  FailStrategy,
  PrepareStatus,
  ProjectKind,
  Run,
  RunStatus,
  ShellType,
  TriggerType,
} from '@/services/types';

/** 状态 → 颜色 + 中文 */
type RunStatusMeta = { color: string; text: string };

// 空字符串表示尚无运行状态，映射中没有对应项，保留调用处的回退显示。
export const RUN_STATUS_META: Record<RunStatus, RunStatusMeta> &
  Partial<Record<'', RunStatusMeta>> = {
  queued: { color: 'default', text: t('common.queued') },
  running: { color: 'processing', text: t('common.running') },
  success: { color: 'success', text: t('common.success') },
  failed: { color: 'error', text: t('common.failed') },
  canceled: { color: 'warning', text: t('status.canceled') },
  timeout: { color: 'volcano', text: t('status.timeout') },
  error: { color: 'error', text: t('status.error') },
};

export const RUN_STATUS_OPTIONS = (Object.keys(RUN_STATUS_META) as RunStatus[]).map((k) => ({
  label: RUN_STATUS_META[k].text,
  value: k,
}));

export const PREPARE_STATUS_META: Record<PrepareStatus, { color: string; text: string }> = {
  skipped: { color: 'default', text: t('common.disabled') },
  running: { color: 'processing', text: t('status.progress') },
  success: { color: 'success', text: t('common.success') },
  failed: { color: 'error', text: t('common.failed') },
};

export const TRIGGER_TYPE_META: Record<TriggerType, string> = {
  web: t('status.web'),
  api: 'API',
  cli: 'CLI',
  hook: 'Hook',
  rerun: t('status.reRun'),
};

export const SHELL_OPTIONS: { label: string; value: ShellType }[] = [
  { label: t('common.shellAuto'), value: 'auto' },
  { label: 'bash', value: 'bash' },
  { label: 'sh', value: 'sh' },
  { label: 'pwsh', value: 'pwsh' },
  { label: 'cmd', value: 'cmd' },
];

export const CONCURRENCY_OPTIONS: { label: string; value: ConcurrencyType }[] = [
  { label: t('status.concurrencyQueue'), value: 'queue' },
  { label: t('status.concurrencyAllow'), value: 'allow' },
  { label: t('status.concurrencyCancelPrev'), value: 'cancelPrev' },
];

export const FAIL_STRATEGY_OPTIONS: { label: string; value: FailStrategy }[] = [
  { label: t('common.failureAbort'), value: 'abort' },
  { label: t('common.failureContinue'), value: 'continue' },
];

export const CREDENTIAL_TYPE_OPTIONS: { label: string; value: CredentialType }[] = [
  { label: t('status.authHttps'), value: 'https_token' },
  { label: t('common.sshPrivateKey'), value: 'ssh_key' },
];

export const PROJECT_KEY_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/;

/**
 * 项目类型 → 颜色 + 中文 + 图标色。
 *
 * color 给 antd Tag 用（预设色名），iconColor 给「用图标代替文字」的场景用（十六进制），
 * 两者是同一个语义的两种着色方式，改的时候要一起改，别让同一个概念出现两种颜色。
 */
export const PROJECT_KIND_META: Record<
  ProjectKind,
  { color: string; text: string; iconColor: string }
> = {
  app: { color: 'blue', text: t('common.applicationProject'), iconColor: '#1677ff' },
  lib: { color: 'purple', text: t('common.buildProject'), iconColor: '#722ed1' },
};

export const PROJECT_KIND_OPTIONS: { label: string; value: ProjectKind }[] = [
  { label: t('common.applicationProject'), value: 'app' },
  { label: t('common.buildProject'), value: 'lib' },
];

/** Secret 环境变量的占位符（提交时若仍为该值则保持原值） */
export const SECRET_PLACEHOLDER = '******';

/**
 * 新建项目 / 任务时「工作目录」的默认根目录：最终值是 `<根目录>/<Key>`。
 *
 * 只影响新建时的预填值；已有项目 / 任务保存的工作目录不会被改动。
 */
export const DEFAULT_WORKDIR_ROOT = '/srv/workspaces';

/** 按 Key 生成默认工作目录；Key 为空时返回空串（表单里表现为"还没填"） */
export function defaultWorkdir(key?: string | null): string {
  const k = (key ?? '').trim();
  return k ? `${DEFAULT_WORKDIR_ROOT}/${k}` : '';
}

export function formatDuration(ms?: number | null): string {
  if (ms === undefined || ms === null || ms < 0) return '-';
  if (ms < 1000) return `${ms} ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)} s`;
  const m = Math.floor(s / 60);
  const rest = Math.round(s % 60);
  if (m < 60) return `${m}m ${rest}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

export function formatBytes(bytes?: number | null): string {
  if (!bytes || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatTime(value?: string | null): string {
  if (!value) return '-';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/** 运行是否已经结束（结束态不再重连 WebSocket） */
export function isRunFinished(status?: RunStatus): boolean {
  return (
    status === 'success' ||
    status === 'failed' ||
    status === 'canceled' ||
    status === 'timeout' ||
    status === 'error'
  );
}

export function isRunActive(run?: Pick<Run, 'status'>): boolean {
  return !!run && (run.status === 'running' || run.status === 'queued');
}

/** 把可选值渲染成 '-' */
export function orDash(v?: string | number | null): string {
  if (v === undefined || v === null || v === '') return '-';
  return String(v);
}
