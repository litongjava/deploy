import { getData, delData, postData, putData } from './api';
import type { LoginResult, PageResult, User } from './types';

export const userApi = {
  list(params: { page?: number; pageSize?: number; keyword?: string }) {
    return getData<PageResult<User>>('/users', params);
  },

  create(body: {
    username: string;
    password: string;
    nickname?: string;
    email?: string;
    role: string;
  }) {
    return postData<User>('/users', body);
  },

  update(
    id: number,
    body: { nickname?: string; email?: string; role?: string; status?: string },
  ) {
    return putData<User>(`/users/${id}`, body);
  },

  remove(id: number) {
    return delData<null>(`/users/${id}`);
  },

  resetPassword(id: number, newPassword: string) {
    return postData<null>(`/users/${id}/reset-password`, { newPassword });
  },
};

export type { LoginResult };
