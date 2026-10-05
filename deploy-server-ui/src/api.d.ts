/**
 * 全局 API 命名空间。
 * access.ts / app.tsx 等运行时文件用 `API.CurrentUser` 引用当前用户类型。
 */
declare namespace API {
  /** 通用表格接口响应；业务接口的响应类型由 services 单独定义。 */
  type Result<T = unknown> =
    | { ok: true; data: T; msg?: string }
    | { ok: false; data?: T; msg?: string };

  type TablePage<T = Record<string, unknown>> = {
    list: T[];
    total: number;
  };

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
