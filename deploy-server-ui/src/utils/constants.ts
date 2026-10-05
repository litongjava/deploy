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
export const RUN_STATUS_META: Record<RunStatus, { color: string; text: string }> = {
  queued: { color: 'default', text: '排队中' },
  running: { color: 'processing', text: '运行中' },
  success: { color: 'success', text: '成功' },
  failed: { color: 'error', text: '失败' },
  canceled: { color: 'warning', text: '已取消' },
  timeout: { color: 'volcano', text: '超时' },
  error: { color: 'error', text: '异常' },
};

export const RUN_STATUS_OPTIONS = (Object.keys(RUN_STATUS_META) as RunStatus[]).map((k) => ({
  label: RUN_STATUS_META[k].text,
  value: k,
}));

export const PREPARE_STATUS_META: Record<PrepareStatus, { color: string; text: string }> = {
  skipped: { color: 'default', text: '未启用' },
  running: { color: 'processing', text: '进行中' },
  success: { color: 'success', text: '成功' },
  failed: { color: 'error', text: '失败' },
};

export const TRIGGER_TYPE_META: Record<TriggerType, string> = {
  web: '网页',
  api: 'API',
  cli: 'CLI',
  hook: 'Hook',
  rerun: '重跑',
};

export const SHELL_OPTIONS: { label: string; value: ShellType }[] = [
  { label: '自动（auto）', value: 'auto' },
  { label: 'bash', value: 'bash' },
  { label: 'sh', value: 'sh' },
  { label: 'pwsh', value: 'pwsh' },
  { label: 'cmd', value: 'cmd' },
];

export const CONCURRENCY_OPTIONS: { label: string; value: ConcurrencyType }[] = [
  { label: '排队串行（queue）', value: 'queue' },
  { label: '允许并行（allow）', value: 'allow' },
  { label: '取消上一次（cancelPrev）', value: 'cancelPrev' },
];

export const FAIL_STRATEGY_OPTIONS: { label: string; value: FailStrategy }[] = [
  { label: '失败即停（abort）', value: 'abort' },
  { label: '忽略错误继续（continue）', value: 'continue' },
];

export const CREDENTIAL_TYPE_OPTIONS: { label: string; value: CredentialType }[] = [
  { label: 'HTTPS Token / 密码', value: 'https_token' },
  { label: 'SSH 私钥', value: 'ssh_key' },
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
  app: { color: 'blue', text: '应用项目', iconColor: '#1677ff' },
  lib: { color: 'purple', text: '构建项目', iconColor: '#722ed1' },
};

export const PROJECT_KIND_OPTIONS: { label: string; value: ProjectKind }[] = [
  { label: '应用项目', value: 'app' },
  { label: '构建项目', value: 'lib' },
];

/** Secret 环境变量的占位符（提交时若仍为该值则保持原值） */
export const SECRET_PLACEHOLDER = '******';

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
