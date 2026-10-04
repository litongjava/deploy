import { getData, postData } from './api';
import type { BootstrapStatus, LoginResult, User } from './types';

export const authApi = {
  /** 是否已完成初始化（公开接口，登录页先调它决定是否跳初始化向导） */
  bootstrapStatus() {
    return getData<BootstrapStatus>('/auth/bootstrap-status', undefined, {
      skipErrorHandler: true,
    });
  },

  /** 首次初始化管理员（公开接口，仅未初始化时可用） */
  bootstrap(body: { username: string; password: string }) {
    return postData<LoginResult>('/auth/bootstrap', body, {
      skipErrorHandler: true,
    });
  },

  login(body: { username: string; password: string }) {
    return postData<LoginResult>('/auth/login', body, {
      skipErrorHandler: true,
    });
  },

  me() {
    return getData<User>('/auth/me', undefined, { skipErrorHandler: true });
  },

  logout() {
    return postData<null>('/auth/logout');
  },

  changePassword(body: { oldPassword: string; newPassword: string }) {
    return postData<null>('/auth/change-password', body);
  },
};
