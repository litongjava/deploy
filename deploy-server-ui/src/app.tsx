import { AvatarDropdown, AvatarName, Footer, Question } from '@/components';
import { authApi } from '@/services/auth';
import { SETUP_PATH, getToken } from '@/services/authStore';
import type { Settings as LayoutSettings } from '@ant-design/pro-components';
import { SettingDrawer } from '@ant-design/pro-components';
import type { RequestConfig, RunTimeLayoutConfig } from '@umijs/max';
import { history } from '@umijs/max';
import defaultSettings from '../config/defaultSettings';
import { errorConfig } from './requestErrorConfig';

const isDev = process.env.NODE_ENV === 'development';
const loginPath = '/user/login';

/** 无需登录即可访问的路径 */
const publicPaths = [loginPath, SETUP_PATH];

function isPublicPath(pathname: string) {
  return publicPaths.some((p) => pathname === p || pathname.startsWith(`${p}/`));
}

/**
 * @see  https://umijs.org/zh-CN/plugins/plugin-initial-state
 * */
export async function getInitialState(): Promise<{
  settings?: Partial<LayoutSettings>;
  currentUser?: API.CurrentUser;
  loading?: boolean;
  fetchUserInfo?: () => Promise<API.CurrentUser | undefined>;
}> {
  const fetchUserInfo = async () => {
    if (!getToken()) return undefined;
    try {
      return await authApi.me();
    } catch (error) {
      return undefined;
    }
  };

  const { location } = history;

  if (isPublicPath(location.pathname)) {
    // 登录页/初始化页不做鉴权重定向，避免死循环
    return {
      fetchUserInfo,
      settings: defaultSettings as Partial<LayoutSettings>,
    };
  }

  const currentUser = await fetchUserInfo();
  if (!currentUser) {
    // 没有 token 或 token 已失效：先判断是否需要初始化，再跳登录页
    try {
      const status = await authApi.bootstrapStatus();
      if (status && status.initialized === false) {
        history.replace(SETUP_PATH);
        return {
          fetchUserInfo,
          settings: defaultSettings as Partial<LayoutSettings>,
        };
      }
    } catch (e) {
      // 后端不可用时不做额外处理
    }
    history.replace({
      pathname: loginPath,
      search: `redirect=${encodeURIComponent(location.pathname + location.search)}`,
    });
  }

  return {
    fetchUserInfo,
    currentUser,
    settings: defaultSettings as Partial<LayoutSettings>,
  };
}

// ProLayout 支持的api https://procomponents.ant.design/components/layout
export const layout: RunTimeLayoutConfig = ({ initialState, setInitialState }) => {
  return {
    title: defaultSettings.title,
    logo: defaultSettings.logo,
    actionsRender: () => [<Question key="question" />],
    avatarProps: {
      title: <AvatarName />,
      render: (_, avatarChildren) => {
        return <AvatarDropdown>{avatarChildren}</AvatarDropdown>;
      },
    },
    footerRender: () => <Footer />,
    onPageChange: () => {
      const { location } = history;
      // 如果没有登录，重定向到 login
      if (!initialState?.currentUser && !isPublicPath(location.pathname)) {
        history.replace({
          pathname: loginPath,
          search: `redirect=${encodeURIComponent(location.pathname + location.search)}`,
        });
      }
      // 强制首次改密
      if (
        initialState?.currentUser?.mustChangePassword &&
        location.pathname !== '/settings/profile' &&
        !isPublicPath(location.pathname)
      ) {
        history.replace('/settings/profile');
      }
    },
    menuHeaderRender: undefined,
    // 增加一个 loading 的状态
    childrenRender: (children) => {
      return (
        <>
          {children}
          {isDev && (
            <SettingDrawer
              disableUrlParams
              enableDarkTheme
              settings={initialState?.settings}
              onSettingChange={(settings) => {
                setInitialState((preInitialState) => ({
                  ...preInitialState,
                  settings,
                }));
              }}
            />
          )}
        </>
      );
    },
    ...initialState?.settings,
  };
};

/**
 * @name request 配置，可以配置错误处理
 * @doc https://umijs.org/docs/max/request#配置
 */
export const request: RequestConfig = {
  baseURL: '/api/v1',
  ...errorConfig,
};
