import { Footer } from '@/components';
import { authApi } from '@/services/auth';
import { SETUP_PATH, setToken, takeRedirect } from '@/services/authStore';
import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { LoginForm, ProFormText } from '@ant-design/pro-components';
import { Helmet, history, useModel } from '@umijs/max';
import { Alert, message, Spin } from 'antd';
import { createStyles } from 'antd-style';
import React, { useEffect, useState } from 'react';
import { flushSync } from 'react-dom';
import Settings from '@/../config/defaultSettings';

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
      messageApi.success('登录成功！');
      const user = result.user || (await fetchUserInfo());
      if (user?.mustChangePassword) {
        messageApi.warning('首次登录请先修改密码');
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
        '登录失败，请重试！';
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
          <title>登录 - {Settings.title}</title>
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
            subTitle="轻量命令式 CI/CD 平台 —— 打开网页，选项目，点执行"
            initialValues={{
              autoLogin: true,
            }}
            onFinish={async (values) => {
              await handleSubmit(values as API.LoginParams);
            }}
          >
            {errorMessage && (
              <Alert
                style={{ marginBottom: 24 }}
                message={errorMessage}
                type="error"
                showIcon
              />
            )}
            <ProFormText
              name="username"
              fieldProps={{
                size: 'large',
                prefix: <UserOutlined />,
                autoComplete: 'username',
              }}
              placeholder="用户名"
              rules={[
                {
                  required: true,
                  message: '请输入用户名！',
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
              placeholder="密码"
              rules={[
                {
                  required: true,
                  message: '请输入密码！',
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
