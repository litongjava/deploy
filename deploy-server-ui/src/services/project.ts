import { delData, getData, postData, putData } from './api';
import type {
  Command,
  CommandInput,
  EnvVar,
  HookToken,
  HookTokenCreated,
  PageResult,
  PreviewResult,
  Project,
  ProjectBrief,
  ProjectInput,
  ProjectQuery,
  TriggerResult,
  TriggerRunInput,
} from './types';

export const projectApi = {
  list(params: ProjectQuery) {
    return getData<PageResult<ProjectBrief>>('/projects', params);
  },

  detail(key: string) {
    return getData<Project>(`/projects/${encodeURIComponent(key)}`);
  },

  /** 创建（commands / envs 内联在一次请求里，见 API 文档 §4.4） */
  create(body: ProjectInput) {
    return postData<Project>('/projects', body);
  },

  update(key: string, body: ProjectInput) {
    return putData<Project>(`/projects/${encodeURIComponent(key)}`, body);
  },

  remove(key: string) {
    return delData<null>(`/projects/${encodeURIComponent(key)}`);
  },

  archive(key: string, archived: boolean) {
    return postData<null>(`/projects/${encodeURIComponent(key)}/archive`, { archived });
  },

  commands(key: string) {
    return getData<Command[]>(`/projects/${encodeURIComponent(key)}/commands`);
  },

  createCommand(key: string, body: CommandInput) {
    return postData<Command>(`/projects/${encodeURIComponent(key)}/commands`, body);
  },

  updateCommand(id: number, body: CommandInput) {
    return putData<Command>(`/commands/${id}`, body);
  },

  removeCommand(id: number) {
    return delData<null>(`/commands/${id}`);
  },

  reorderCommands(key: string, ids: number[]) {
    return postData<null>(`/projects/${encodeURIComponent(key)}/commands/reorder`, { ids });
  },

  /** 环境变量整体替换 */
  replaceEnvs(key: string, envs: EnvVar[]) {
    return putData<null>(`/projects/${encodeURIComponent(key)}/envs`, { envs });
  },

  preview(key: string, body?: { branch?: string; env?: Record<string, string> }) {
    return postData<PreviewResult>(`/projects/${encodeURIComponent(key)}/preview`, body || {});
  },

  /** 触发执行；withDependencies=true 时先按依赖顺序构建依赖的构建项目 */
  run(key: string, body?: TriggerRunInput) {
    return postData<TriggerResult>(`/projects/${encodeURIComponent(key)}/run`, body || {});
  },

  /* ---------------------------- Hook Token ---------------------------- */

  hooks(key: string) {
    return getData<HookToken[]>(`/projects/${encodeURIComponent(key)}/hooks`);
  },

  createHook(key: string, body: { name: string; expiresInDays?: number }) {
    return postData<HookTokenCreated>(`/projects/${encodeURIComponent(key)}/hooks`, body);
  },

  removeHook(id: number) {
    return delData<null>(`/hooks/${id}`);
  },

  resetHook(id: number) {
    return postData<HookTokenCreated>(`/hooks/${id}/reset`);
  },
};
