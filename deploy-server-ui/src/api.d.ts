/**
 * 全局 API 命名空间。
 * access.ts / app.tsx 等运行时文件用 `API.CurrentUser` 引用当前用户类型。
 */
declare namespace API {
  type UserRole = 'admin' | 'user';
  type UserStatus = 'active' | 'disabled';

  type CurrentUser = {
    id: number;
    username: string;
    nickname: string;
    email: string;
    role: UserRole;
    status: UserStatus;
    mustChangePassword: boolean;
    lastLoginAt: string | null;
    createdAt: string;
  };

  type LoginResult = {
    token: string;
    expiresAt: number;
    mustChangePassword: boolean;
    user: CurrentUser;
  };

  type LoginParams = {
    username: string;
    password: string;
  };
}
