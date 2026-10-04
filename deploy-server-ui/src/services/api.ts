import { request } from '@umijs/max';
import type { ResponseEnvelope } from './authStore';

/**
 * 后端 Base path（见 docs/API设计.md §0）。
 * 生产环境前端由 deploy-server 同源提供，dev 环境走 config/proxy.ts 代理，
 * 所以这里用相对路径，不需要区分环境。
 */
export const API_BASE = '/api/v1';

/** 带类型信封的请求类型（模板的 request 只声明了裸类型） */
type ApiRequest = <T = any>(
  url: string,
  opts?: Record<string, any>,
) => Promise<ResponseEnvelope<T>>;

const api = request as unknown as ApiRequest;

export function get<T = any>(url: string, params?: any, opts?: Record<string, any>) {
  return api<T>(url, { method: 'GET', params, ...(opts || {}) });
}

export function post<T = any>(url: string, data?: any, opts?: Record<string, any>) {
  return api<T>(url, { method: 'POST', data, ...(opts || {}) });
}

export function put<T = any>(url: string, data?: any, opts?: Record<string, any>) {
  return api<T>(url, { method: 'PUT', data, ...(opts || {}) });
}

export function del<T = any>(url: string, data?: any, opts?: Record<string, any>) {
  return api<T>(url, { method: 'DELETE', data, ...(opts || {}) });
}

/** 取 data 的语法糖，失败时抛错（由 requestErrorConfig 统一弹提示） */
export async function getData<T = any>(url: string, params?: any, opts?: Record<string, any>) {
  const res = await get<T>(url, params, opts);
  return res.data;
}

export async function postData<T = any>(url: string, data?: any, opts?: Record<string, any>) {
  const res = await post<T>(url, data, opts);
  return res.data;
}

export async function putData<T = any>(url: string, data?: any, opts?: Record<string, any>) {
  const res = await put<T>(url, data, opts);
  return res.data;
}

export async function delData<T = any>(url: string, data?: any, opts?: Record<string, any>) {
  const res = await del<T>(url, data, opts);
  return res.data;
}
