import { delData, getData, postData, putData } from './api';
import { API_BASE } from './api';
import { getToken } from './authStore';
import type {
  Credential,
  CredentialInput,
  DoctorResult,
  GitTestResult,
  ServerInfo,
  SystemSettings,
  ToolchainInstallInput,
  ToolchainInstallResult,
  ToolchainList,
} from './types';

export const systemApi = {
  doctor() {
    return getData<DoctorResult>('/system/doctor');
  },

  /**
   * 服务器基本信息与资源占用（CPU / 内存 / 磁盘）。
   *
   * 后端读的是内存里的采样快照（每 3 秒刷新一次），接口本身不做系统调用，
   * 所以可以放心按几秒的间隔轮询。
   */
  info() {
    return getData<ServerInfo>('/system/info');
  },

  getSettings() {
    return getData<SystemSettings>('/system/settings');
  },

  /** 仅 admin 可写 */
  updateSettings(body: SystemSettings) {
    return putData<SystemSettings>('/system/settings', body);
  },

  /** 可一键安装的工具链清单（登录可见，安装仅 admin） */
  toolchains() {
    return getData<ToolchainList>('/system/toolchains');
  },

  /** 触发一次工具链安装：以普通 Run 的形式执行，返回可供跳转看日志的 runId */
  installToolchains(body: ToolchainInstallInput) {
    return postData<ToolchainInstallResult>('/system/toolchains/install', body);
  },
};

/**
 * 下载内嵌的安装脚本：接口需要 Authorization 头，浏览器直接打开链接不会带 token，
 * 所以用 fetch 取回 blob 再交给 <a download>（与下载运行日志同一套做法）。
 */
export async function downloadToolchainScript() {
  const token = getToken();
  const res = await fetch(`${API_BASE}/system/toolchains/script`, {
    headers: token ? { Authorization: token } : undefined,
  });
  if (!res.ok) {
    throw new Error(`下载失败（HTTP ${res.status}）`);
  }
  return res.blob();
}

export const credentialApi = {
  list() {
    return getData<Credential[]>('/credentials');
  },

  create(body: CredentialInput) {
    return postData<Credential>('/credentials', body);
  },

  update(id: number, body: CredentialInput) {
    return putData<Credential>(`/credentials/${id}`, body);
  },

  remove(id: number) {
    return delData<null>(`/credentials/${id}`);
  },

  testGit(body: { gitUrl: string; credentialId?: number | null; branch?: string }) {
    return postData<GitTestResult>('/credentials/test-git', body);
  },
};
