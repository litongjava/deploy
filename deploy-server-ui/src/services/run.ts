import { getData, postData } from './api';
import { API_BASE } from './api';
import { getToken } from './authStore';
import type { LogChunk, PageResult, Run, RunQuery, TriggerResult } from './types';
import { t } from '@/i18n';

export const runApi = {
  list(params: RunQuery) {
    return getData<PageResult<Run>>('/runs', params);
  },

  detail(id: number | string) {
    return getData<Run>(`/runs/${id}`);
  },

  log(id: number | string, offset = 0, limit = 65536) {
    return getData<LogChunk>(`/runs/${id}/log`, { offset, limit });
  },

  cancel(id: number | string) {
    return postData<null>(`/runs/${id}/cancel`);
  },

  rerun(id: number | string) {
    return postData<TriggerResult>(`/runs/${id}/rerun`);
  },
};

/**
 * 下载日志：接口需要 Authorization 头，浏览器直接打开链接不会带 token，
 * 所以用 fetch 取回 blob 再交给 <a download>。
 */
export async function downloadRunLog(id: number | string) {
  const token = getToken();
  const res = await fetch(`${API_BASE}/runs/${id}/log/download`, {
    headers: token ? { Authorization: token } : undefined,
  });
  if (!res.ok) {
    throw new Error(t('下载失败（HTTP {status}）', { status: res.status }));
  }
  return res.blob();
}
