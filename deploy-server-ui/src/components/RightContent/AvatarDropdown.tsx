import { t } from '@/i18n';
import { authApi } from '@/services/auth';
import { clearToken } from '@/services/authStore';
import {
  LogoutOutlined,
  QuestionCircleOutlined,
  SettingOutlined,
  UserOutlined,
} from '@ant-design/icons';
import { history, useModel } from '@umijs/max';
import { Spin } from 'antd';
import { createStyles } from 'antd-style';
import React, { useCallback } from 'react';
import { flushSync } from 'react-dom';
import HeaderDropdown from '../HeaderDropdown';
import { LanguageMenuItemLabel } from '../LanguageSwitcher';

export type GlobalHeaderRightProps = {
  menu?: boolean;
  children?: React.ReactNode;
};

export const AvatarName = () => {
  const { initialState } = useModel('@@initialState');
  const { currentUser } = initialState || {};
  return <span className="anticon">{currentUser?.nickname || currentUser?.username}</span>;
};

const useStyles = createStyles(({ token }) => {
  return {
    action: {
      display: 'flex',
      height: '48px',
      marginLeft: 'auto',
      overflow: 'hidden',
      alignItems: 'center',
      padding: '0 8px',
      cursor: 'pointer',
      borderRadius: token.borderRadius,
      '&:hover': {
        backgroundColor: token.colorBgTextHover,
      },
    },
  };
});

export const AvatarDropdown: React.FC<GlobalHeaderRightProps> = ({ children }) => {
  const { styles } = useStyles();
  const { initialState, setInitialState } = useModel('@@initialState');

  /** 退出登录：调后端登出接口（失败也无所谓），清本地 token，回登录页 */
  const loginOut = useCallback(async () => {
    try {
      await authApi.logout();
    } catch (e) {
      // 后端不可用也要能退出
    }
    clearToken();
    const { search, pathname } = window.location;
    const urlParams = new URL(window.location.href).searchParams;
    const redirect = urlParams.get('redirect');
    if (window.location.pathname !== '/user/login' && !redirect) {
      history.replace({
        pathname: '/user/login',
        search: `redirect=${encodeURIComponent(pathname + search)}`,
      });
    }
  }, []);

  const onMenuClick = useCallback(
    (event: any) => {
      const { key } = event;
      if (key === 'logout') {
        flushSync(() => {
          setInitialState((s) => ({ ...s, currentUser: undefined }));
        });
        loginOut();
        return;
      }
      if (key === 'help') {
        // 原来顶栏那个「?」只是个 hover 提示（title 里写明接口契约在哪），没有点击行为；
        // 挪进下拉菜单后也保持只做提示，不能落到下面这条默认的「个人设置」跳转上。
        return;
      }
      if (key === 'lang') {
        // 语言项自己处理切换（LanguageMenuItemLabel 里的 onClick），这里不再跳页
        return;
      }
      history.push(`/settings/profile`);
    },
    [setInitialState, loginOut],
  );

  const loading = (
    <span className={styles.action}>
      <Spin
        size="small"
        style={{
          marginLeft: 8,
          marginRight: 8,
        }}
      />
    </span>
  );

  if (!initialState) {
    return loading;
  }

  const { currentUser } = initialState;

  if (!currentUser) {
    return loading;
  }

  const menuItems = [
    {
      key: 'center',
      icon: <UserOutlined />,
      label: t('common.profile'),
    },
    {
      key: 'settings',
      icon: <SettingOutlined />,
      label: t('common.systemSettings'),
    },
    {
      // 语言切换：标签自带 onClick 直接切语言，所以这里不能再落到下面
      // 默认的「跳个人设置」分支上（onMenuClick 里已对 lang 早退）。
      key: 'lang',
      label: <LanguageMenuItemLabel />,
    },
    {
      key: 'help',
      icon: <QuestionCircleOutlined />,
      // 顶栏那个「?」的 title 提示原样保留：悬停在这一项上仍能看到接口契约的位置
      label: <span title={t('account.apiContractDocs')}>{t('account.help')}</span>,
    },
    {
      type: 'divider' as const,
    },
    {
      key: 'logout',
      icon: <LogoutOutlined />,
      label: t('account.signOut'),
    },
  ];

  return (
    <HeaderDropdown
      menu={{
        selectedKeys: [],
        onClick: onMenuClick,
        items: menuItems,
      }}
    >
      {children}
    </HeaderDropdown>
  );
};
