import { delData, getData, postData } from './api';
import type { ApiToken, ApiTokenCreated } from './types';

export const tokenApi = {
  list() {
    return getData<ApiToken[]>('/tokens');
  },

  /** 返回体里带一次性的明文 token */
  create(body: { name: string; expiresInDays?: number }) {
    return postData<ApiTokenCreated>('/tokens', body);
  },

  remove(id: number) {
    return delData<null>(`/tokens/${id}`);
  },
};
