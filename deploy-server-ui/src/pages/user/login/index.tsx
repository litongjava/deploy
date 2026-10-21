import Settings from '@/../config/defaultSettings';
import { Footer } from '@/components';
import { t } from '@/i18n';
import { authApi } from '@/services/auth';
import { setToken, SETUP_PATH, takeRedirect } from '@/services/authStore';
import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { LoginForm, ProFormText } from '@ant-design/pro-components';
import { Helmet, history, useModel } from '@umijs/max';
import { Alert, message, Spin } from 'antd';
import { createStyles } from 'antd-style';
import React, { useEffect, useState } from 'react';
import { flushSync } from 'react-dom';

const useStyles = createStyles(({ token }) => {
  return {
    container: {
      display: 'flex',
      flexDirection: 'column',
      height: '100vh',
      overflow: 'auto',
      backgroundImage:
        "url('https://mdn.alipayobjects.com/yuyan_qk0oxh/afts/img/V-_oS6r-i7wAAAAAAAAAAAAAFl94AQBr')",
      backgroundSize: '100% 100%',
    },
  };
});

const Login: React.FC = () => {
  const { styles } = useStyles();
  const { initialState, setInitialState } = useModel('@@initialState');
  const [messageApi, messageContextHolder] = message.useMessage();
  const [errorMessage, setErrorMessage] = useState<string>('');
  const [checking, setChecking] = useState<boolean>(false);

  // 先问后端是否已初始化；未初始化直接进初始化向导
  useEffect(() => {
    let alive = true;
    authApi
      .bootstrapStatus()
      .then((status) => {
        if (alive && status && status.initialized === false) {
          history.replace(SETUP_PATH);
        }
      })
      .catch(() => {
        // 后端不可用时留在登录页，登录时会给出错误提示
      });
    return () => {
      alive = false;
    };
  }, []);

  const fetchUserInfo = async () => {
    const userInfo = await initialState?.fetchUserInfo?.();
    if (userInfo) {
      flushSync(() => {
        setInitialState((s) => ({
          ...s,
          currentUser: userInfo,
        }));
      });
    }
    return userInfo;
  };

  const handleSubmit = async (values: API.LoginParams) => {
    setChecking(true);
    setErrorMessage('');
    try {
      const result = await authApi.login({
        username: values.username,
        password: values.password,
      });
      setToken(result.token);
      messageApi.success(t('login.signedIn'));
      const user = result.user || (await fetchUserInfo());
      if (user?.mustChangePassword) {
        messageApi.warning(t('login.changeYourPasswordOnFirst'));
        history.replace('/settings/profile');
        return;
      }
      window.location.href = takeRedirect();
    } catch (error: any) {
      const info = error?.info;
      const msg =
        info?.errorMessage ||
        error?.response?.data?.errorMessage ||
        error?.message ||
        t('login.signInFailedPleaseRetry');
      setErrorMessage(msg);
    } finally {
      setChecking(false);
    }
  };

  return (
    <>
      {messageContextHolder}
      <div className={styles.container}>
        <Helmet>
          <title>{t('login.signIn', { title: Settings.title })}</title>
        </Helmet>
        <div
          style={{
            flex: '1',
            padding: '32px 0',
          }}
        >
          {checking && (
            <div style={{ textAlign: 'center', marginBottom: 8 }}>
              <Spin />
            </div>
          )}
          <LoginForm
            contentStyle={{
              minWidth: 280,
              maxWidth: '75vw',
            }}
            logo={<img alt="logo" src={Settings.logo} />}
            title={Settings.title}
            subTitle={t('login.subtitle')}
            initialValues={{
              autoLogin: true,
            }}
            onFinish={async (values) => {
              await handleSubmit(values as API.LoginParams);
            }}
          >
            {errorMessage && (
              <Alert style={{ marginBottom: 24 }} message={errorMessage} type="error" showIcon />
            )}
            <ProFormText
              name="username"
              fieldProps={{
                size: 'large',
                prefix: <UserOutlined />,
                autoComplete: 'username',
              }}
              placeholder={t('common.username')}
              rules={[
                {
                  required: true,
                  message: t('login.enterTheUsername'),
                },
              ]}
            />
            <ProFormText.Password
              name="password"
              fieldProps={{
                size: 'large',
                prefix: <LockOutlined />,
                autoComplete: 'current-password',
              }}
              placeholder={t('common.password')}
              rules={[
                {
                  required: true,
                  message: t('login.enterThePassword'),
                },
              ]}
            />
          </LoginForm>
        </div>
        <Footer />
      </div>
    </>
  );
};

export default Login;
