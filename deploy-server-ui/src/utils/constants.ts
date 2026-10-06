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

/**
 * 新建项目 / 任务时「工作目录」的默认根目录：最终值是 `<根目录>/<Key>`。
 *
 * 注意 `worksapce` 是**历史拼写**（见 docs/cases/mosskb-web-部署案例.md §5.1），
 * 服务器上的 tio-boot 就在这个目录里。改成 `workspaces` 会让项目指向别的目录、
 * 重新 clone 一份，所以不要"顺手修正"。
 */
export const DEFAULT_WORKDIR_ROOT = '/srv/worksapce';

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
