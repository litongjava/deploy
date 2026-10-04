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
/** 项目类型：app 应用项目（默认）| lib 构建项目（可被别的项目依赖） */
export type ProjectKind = 'app' | 'lib';

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

export interface DepNode {
  key: string;
  name: string;
  kind: ProjectKind;
  archived: boolean;
  order?: number;
}

/** 依赖构建计划：含依赖执行时实际会按什么顺序构建 */
export interface DependencyPlan {
  ok: boolean;
  error?: string;
  order: DepNode[];
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
  /** 项目类型：app 应用项目 | lib 构建项目 */
  kind: ProjectKind;
  /** 直接依赖的构建项目 key 列表 */
  dependencies: string[];
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
  /** 仅详情接口返回 */
  dependencyPlan?: DependencyPlan;
  /** 仅详情接口返回：谁依赖了我 */
  dependents?: DepNode[];
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
  kind?: ProjectKind;
  dependencies?: string[];
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
  /** true = 只看已归档；false/不传 = 只看未归档；'all' = 全部 */
  archived?: boolean | 'all';
  mine?: boolean;
  /** 只取某一类项目（依赖下拉用 kind=lib） */
  kind?: ProjectKind;
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
  /** 依赖构建流水线：同一次触发产生的 Run 共享同一个 pipelineId */
  pipelineId?: string;
  pipelineRole?: '' | 'main' | 'dependency';
  pipelineOrder?: number;
  /** 仅运行详情接口返回：同一条流水线里的全部 Run */
  pipeline?: PipelineRun[];
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

export interface PipelineRun {
  runId: number;
  projectKey: string;
  projectName: string;
  buildNumber: number;
  status: RunStatus;
  pipelineRole: '' | 'main' | 'dependency';
  pipelineOrder: number;
  durationMs: number;
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
  /** 先按依赖顺序构建依赖的构建项目，全部成功后再执行本项目 */
  withDependencies?: boolean;
}

export interface TriggerResult {
  runId: number;
  buildNumber: number;
  status: RunStatus;
  logUrl: string;
  pipelineId?: string;
  dependencyCount?: number;
  dependencies?: { key: string; runId: number; buildNumber: number }[];
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

/* ------------------- 服务器信息（GET /system/info） ------------------- */

export interface ServerLoad {
  /** Windows 没有 load average，为 false，此时应隐藏该项而不是显示 0 */
  available: boolean;
  avg1: number;
  avg5: number;
  avg15: number;
}

export interface ServerCpu {
  model: string;
  /** 逻辑核心数（含超线程） */
  cores: number;
  /** 物理核数；Windows 上恒为 0，此时按 0 处理（隐藏该项） */
  physicalCores: number;
  mhz: number;
  /** 服务刚启动、第一次采样还没完成时为 false，应显示「采样中」而不是 0% */
  available: boolean;
  usagePercent: number;
  /** 使用率的统计窗口（秒），默认采样间隔为 3 */
  sampleWindowSec: number;
  /**
   * 以下都是**所有核心累加**的时间（秒），不是墙上时钟：
   * 16 核跑满 1 秒 → totalSec ≈ 16。usagePercent = busySec / totalSec。
   */
  busySec: number;
  totalSec: number;
  userSec: number;
  systemSec: number;
  idleSec: number;
  iowaitSec: number;
}

export interface ServerMemory {
  totalBytes: number;
  /** Linux 上是 total - available（不是 total - free），见 docs/API设计.md */
  usedBytes: number;
  freeBytes: number;
  availableBytes: number;
  usagePercent: number;
  /**
   * Swap* 的口径两个平台不同（Linux 是交换分区，Windows 是提交内存），
   * 所以标题要照 swapLabel 显示，不要硬写「交换分区」。
   */
  swapTotalBytes: number;
  swapUsedBytes: number;
  swapFreeBytes: number;
  /** linux -> 'swap' | windows -> 'commit' */
  swapKind: string;
  /** 可直接用作标题的中文名，如「交换分区」/「提交内存」 */
  swapLabel: string;
}

export interface ServerDisk {
  /** Linux 是挂载点（/、/home），Windows 是卷根（C:\） */
  path: string;
  device: string;
  fstype: string;
  totalBytes: number;
  usedBytes: number;
  /** Linux 上是「非特权用户可用」，与 df 的 Avail 列口径一致 */
  freeBytes: number;
  usagePercent: number;
  /** 后端算好的阈值判断：ok | warn（≥75% 或剩余 <2GiB） */
  status: 'ok' | 'warn';
}

export interface ServerInfo {
  time: string;
  hostname: string;
  os: string;
  arch: string;
  osName: string;
  kernel: string;
  uptimeSec: number;
  load: ServerLoad;
  cpu: ServerCpu;
  memory: ServerMemory;
  disks: ServerDisk[];
  /** 采集失败的项目说明，用于解释「某项为什么是空的」 */
  warnings: string[];
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

/* --------------------------- 环境工具链安装 --------------------------- */

export interface ToolchainProbe {
  name: string;
  found: boolean;
  version: string;
  path: string;
}

export interface ToolchainComponent {
  key: string;
  name: string;
  note: string;
  estimate: string;
  installed: boolean;
  version: string;
  path: string;
  probes: ToolchainProbe[];
}

export interface ToolchainList {
  /** 只有 Linux 服务端支持真正的安装（dry-run 任何平台都能看） */
  supported: boolean;
  platform: string;
  components: ToolchainComponent[];
  /** 当前缺失的组件 key */
  missing: string[];
  scriptPath: string;
  scriptUrl: string;
  projectKey: string;
  hint: string;
  installCmd: string;
}

export interface ToolchainInstallInput {
  /** 空数组 = 安装所有缺失的组件 */
  components?: string[];
  dryRun?: boolean;
  force?: boolean;
  mirror?: 'cn' | 'off';
}

export interface ToolchainInstallResult {
  runId: number;
  buildNumber?: number;
  status?: RunStatus;
  projectKey?: string;
  components?: string[];
  dryRun?: boolean;
  command?: string;
  logUrl?: string;
  nothingToDo?: boolean;
  message?: string;
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
