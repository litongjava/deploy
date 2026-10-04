/**
 * token 与登录态相关的纯工具函数。
 * 单独成文件是为了避免 requestErrorConfig 与 services 之间形成循环依赖。
 */

export const TOKEN_KEY = 'token';

export const LOGIN_PATH = '/user/login';
export const SETUP_PATH = '/user/setup';

/** 无需登录即可访问的路径（初始化向导 / 登录页） */
const PUBLIC_PATHS = [LOGIN_PATH, SETUP_PATH];

export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) || '';
  } catch (e) {
    return '';
  }
}

export function setToken(token: string) {
  try {
    localStorage.setItem(TOKEN_KEY, token);
  } catch (e) {
    // ignore（隐私模式等）
  }
}

export function clearToken() {
  try {
    localStorage.removeItem(TOKEN_KEY);
  } catch (e) {
    // ignore
  }
}

export function isPublicPath(pathname?: string): boolean {
  const path = pathname ?? (typeof window !== 'undefined' ? window.location.pathname : '');
  return PUBLIC_PATHS.some((p) => path === p || path.startsWith(`${p}/`));
}

let redirecting = false;

/**
 * 登录态失效：清 token 并跳到登录页（带上 redirect）。
 * 已经在登录页/初始化页时不跳，避免死循环。
 */
export function redirectToLogin() {
  clearToken();
  if (typeof window === 'undefined') return;
  if (isPublicPath()) return;
  if (redirecting) return;
  redirecting = true;
  const { pathname, search, hash } = window.location;
  const redirect = encodeURIComponent(`${pathname}${search}${hash}`);
  window.location.href = `${LOGIN_PATH}?redirect=${redirect}`;
}

/** 从当前地址取 redirect 参数（登录成功后回跳） */
export function takeRedirect(): string {
  if (typeof window === 'undefined') return '/';
  const url = new URL(window.location.href);
  const redirect = url.searchParams.get('redirect');
  if (!redirect) return '/';
  return redirect.startsWith('/') ? redirect : '/';
}

/** 后端成功响应信封 */
export interface ResponseEnvelope<T> {
  success: boolean;
  data: T;
  errorCode?: number;
  errorMessage?: string;
  showType?: number;
}
