/**
 * 与 docs/API设计.md（v1，冻结）一一对应的类型定义。
 * 字段命名统一为 camelCase，与后端契约保持一致。
 */

/* ------------------------------- 通用 ------------------------------- */

export interface PageResult<T> {
  list: T[];
  total: number;
  page: number;
  pageSize: number;
}

export interface PageQuery {
  page?: number;
  pageSize?: number;
}

/* ------------------------------- 用户 ------------------------------- */

export type UserRole = 'admin' | 'user';
export type UserStatus = 'active' | 'disabled';

export interface User {
  id: number;
  username: string;
  nickname: string;
  email: string;
  role: UserRole;
  status: UserStatus;
  mustChangePassword: boolean;
  lastLoginAt: string | null;
  createdAt: string;
}

export interface LoginResult {
  token: string;
  expiresAt: number;
  mustChangePassword: boolean;
  user: User;
}

export interface BootstrapStatus {
  initialized: boolean;
}

/* ----------------------------- API Token ---------------------------- */

export interface ApiToken {
  id: number;
  name: string;
  prefix: string;
  expiresAt: string | null;
  lastUsedAt: string | null;
  createdAt: string;
}

export interface ApiTokenCreated extends ApiToken {
  /** 明文 token，仅创建/重置时返回一次 */
  token: string;
}

/* ------------------------------ 凭据 ------------------------------- */

export type CredentialType = 'https_token' | 'ssh_key';

export interface Credential {
  id: number;
  name: string;
  type: CredentialType;
  username: string;
  createdAt: string;
}

export interface CredentialInput {
  name: string;
  type: CredentialType;
  username?: string;
  secret?: string;
  passphrase?: string;
}

export interface GitTestResult {
  ok: boolean;
  message: string;
  sampleRefs?: string[];
}

/* ------------------------------ 项目 ------------------------------- */

export type ShellType = 'auto' | 'bash' | 'sh' | 'pwsh' | 'cmd';
export type ConcurrencyType = 'queue' | 'allow' | 'cancelPrev';
export type FailStrategy = 'abort' | 'continue';

export interface Command {
  id: number;
  projectId: number;
  name: string;
  script: string;
  sortOrder: number;
  enabled: boolean;
  failStrategy: FailStrategy;
  workdirOverride: string;
  timeoutSec: number;
  shellOverride: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface CommandInput {
  id?: number;
  name: string;
  script: string;
  sortOrder?: number;
  enabled: boolean;
  failStrategy: FailStrategy;
  workdirOverride?: string;
  timeoutSec?: number;
  shellOverride?: string;
}

export interface EnvVar {
  id?: number;
  key: string;
  value: string;
  isSecret: boolean;
}

export interface ProjectBrief {
  id: number;
  key: string;
  name: string;
  description: string;
  workdir: string;
  shell: ShellType;
  timeoutSec: number;
  concurrency: ConcurrencyType;
  hookEnabled: boolean;
  ownerId: number;
  ownerName: string;
  archived: boolean;
  useGit: boolean;
  gitUrl: string;
  gitBranch: string;
  allowEnvOverride: boolean;
  lastRunAt: string | null;
  lastRunStatus: RunStatus | '';
  runCount: number;
  canEdit: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface Project extends ProjectBrief {
  gitCredentialId: number | null;
  gitSubdir: string;
  gitDepth: number;
  gitClean: boolean;
  gitSubmodule: boolean;
  commands: Command[];
  envs: EnvVar[];
}

export interface ProjectInput {
  key: string;
  name: string;
  description?: string;
  workdir: string;
  shell?: ShellType;
  timeoutSec?: number;
  concurrency?: ConcurrencyType;
  hookEnabled?: boolean;
  archived?: boolean;
  useGit: boolean;
  gitUrl?: string;
  gitBranch?: string;
  gitCredentialId?: number | null;
  gitSubdir?: string;
  gitDepth?: number;
  gitClean?: boolean;
  gitSubmodule?: boolean;
  allowEnvOverride?: boolean;
  commands?: CommandInput[];
  envs?: EnvVar[];
}

export interface ProjectQuery extends PageQuery {
  keyword?: string;
  archived?: boolean;
  mine?: boolean;
}

export interface PreviewResult {
  workdir: string;
  git: { enabled: boolean; url: string; branch: string; subdir: string };
  envs: { key: string; value: string; isSecret: boolean }[];
  builtinEnvs: { key: string; value: string }[];
  script: string;
}

/* --------------------------- Hook Token ---------------------------- */

export interface HookToken {
  id: number;
  projectId: number;
  name: string;
  prefix: string;
  enabled: boolean;
  expiresAt: string | null;
  lastUsedAt: string | null;
  lastUsedIp: string;
  callCount: number;
  createdAt: string;
}

export interface HookTokenCreated extends HookToken {
  /** 明文 token，仅创建/重置时返回一次 */
  token: string;
}

/* ------------------------------- 运行 ------------------------------- */

export type RunStatus =
  | 'queued'
  | 'running'
  | 'success'
  | 'failed'
  | 'canceled'
  | 'timeout'
  | 'error';

export type TriggerType = 'web' | 'api' | 'cli' | 'hook' | 'rerun';
export type PrepareStatus = 'skipped' | 'running' | 'success' | 'failed';

export interface RunStep {
  id: number;
  runId: number;
  phase: 'prepare' | 'command';
  commandId: number | null;
  name: string;
  sortOrder: number;
  status: RunStatus;
  startedAt: string | null;
  finishedAt: string | null;
  durationMs: number;
  exitCode: number | null;
  logOffsetStart: number;
  logOffsetEnd: number;
}

export interface Run {
  id: number;
  projectId: number;
  projectKey: string;
  projectName: string;
  buildNumber: number;
  status: RunStatus;
  triggerType: TriggerType;
  triggerUserId: number | null;
  triggerUserName: string;
  triggerIp: string;
  triggerUa: string;
  requestedRef: string;
  params: Record<string, string> | null;
  prepareStatus: PrepareStatus;
  prepareDurationMs: number;
  gitCommit: string;
  gitCommitShort: string;
  gitBranchResolved: string;
  startedAt: string | null;
  finishedAt: string | null;
  durationMs: number;
  exitCode: number | null;
  errorMsg: string;
  logSize: number;
  steps?: RunStep[];
}

export interface RunQuery extends PageQuery {
  projectKey?: string;
  status?: RunStatus;
}

export interface TriggerRunInput {
  branch?: string;
  env?: Record<string, string>;
  commandIds?: number[];
  commandId?: number;
}

export interface TriggerResult {
  runId: number;
  buildNumber: number;
  status: RunStatus;
  logUrl: string;
}

export interface LogChunk {
  offset: number;
  nextOffset: number;
  eof: boolean;
  data: string;
}

/* ------------------------------ 系统 ------------------------------- */

export interface DoctorCheck {
  name: string;
  found: boolean;
  version: string;
  path: string;
  hint: string;
}

export interface DoctorResult {
  os: string;
  arch: string;
  hostname: string;
  goVersion: string;
  serverVersion: string;
  uptimeSec: number;
  webRoot: string;
  webRootWritable: boolean;
  dataDir: string;
  dataDirWritable: boolean;
  shell: { selected: string; path: string; available: string[] };
  checks: DoctorCheck[];
}

export interface SystemSettings {
  webRoot: string;
  maxConcurrentRuns: number;
  logRetentionDays: number;
  logMaxBytes: number;
  hookRateLimitPerMinute: number;
  visibility: 'allReadable' | 'ownerOnly';
  allowRegistration: boolean;
  notify: { enabled: boolean; url: string; on: string[] };
}

/* --------------------------- WebSocket 帧 --------------------------- */

export type WsFrame =
  | { type: 'history'; runId: number; data: string; nextOffset: number }
  | { type: 'log'; runId: number; seq: number; data: string; nextOffset: number }
  | {
      type: 'status';
      runId: number;
      status: RunStatus;
      step?: { name: string; sortOrder: number };
    }
  | {
      type: 'exit';
      runId: number;
      status: RunStatus;
      exitCode: number | null;
      durationMs: number;
    }
  | { type: 'error'; runId: number; message: string };
