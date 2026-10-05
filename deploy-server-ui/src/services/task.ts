import { delData, getData, postData, putData } from './api';
import type {
  HookToken,
  HookTokenCreated,
  PageResult,
  PreviewResult,
  Task,
  TaskBrief,
  TaskInput,
  TaskQuery,
  TriggerResult,
} from './types';

/**
 * 任务接口。
 *
 * 与 projectApi 的差别是有意做小的：
 *   - 没有 commands / reorderCommands：任务的步骤是内联 JSON，随 create/update 整体提交；
 *   - 没有 git 相关接口：任务没有 Git 准备阶段；
 *   - 没有 withDependencies：任务没有依赖。
 *
 * Hook 的删除与重置依旧是 id 维度的（后端一个令牌只属于一个项目或一个任务），
 * 所以复用 /hooks/{id} 与 /hook-tokens/{id}/reset，只把「新建/列表」做成任务维度的。
 */
export const taskApi = {
  list: (params: TaskQuery) => getData<PageResult<TaskBrief>>('/tasks', params),
  detail: (key: string) => getData<Task>(`/tasks/${encodeURIComponent(key)}`),
  create: (body: TaskInput) => postData<Task>('/tasks', body),
  update: (key: string, body: TaskInput) => putData<Task>(`/tasks/${encodeURIComponent(key)}`, body),
  remove: (key: string) => delData<null>(`/tasks/${encodeURIComponent(key)}`),
  archive: (key: string, archived: boolean) =>
    postData<null>(`/tasks/${encodeURIComponent(key)}/archive`, { archived }),
  preview: (key: string, body?: { env?: Record<string, string> }) =>
    postData<Omit<PreviewResult, 'git'>>(`/tasks/${encodeURIComponent(key)}/preview`, body || {}),
  run: (key: string, body?: { env?: Record<string, string>; commandId?: number; commandIds?: number[] }) =>
    postData<TriggerResult>(`/tasks/${encodeURIComponent(key)}/run`, body || {}),

  hooks: (key: string) => getData<HookToken[]>(`/tasks/${encodeURIComponent(key)}/hooks`),
  createHook: (key: string, body: { name: string; expiresInDays?: number }) =>
    postData<HookTokenCreated>(`/tasks/${encodeURIComponent(key)}/hooks`, body),
  // 与项目共用：令牌 id 全局唯一，服务端按 taskId 判断该用哪套权限。
  removeHook: (id: number) => delData<null>(`/hooks/${id}`),
  resetHook: (id: number) => postData<HookTokenCreated>(`/hook-tokens/${id}/reset`, {}),
};

export default taskApi;
