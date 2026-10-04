import { delData, getData, postData, putData } from './api';
import type {
  Credential,
  CredentialInput,
  DoctorResult,
  GitTestResult,
  SystemSettings,
} from './types';

export const systemApi = {
  doctor() {
    return getData<DoctorResult>('/system/doctor');
  },

  getSettings() {
    return getData<SystemSettings>('/system/settings');
  },

  /** 仅 admin 可写 */
  updateSettings(body: SystemSettings) {
    return putData<SystemSettings>('/system/settings', body);
  },
};

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
