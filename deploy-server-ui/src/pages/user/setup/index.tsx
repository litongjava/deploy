import Settings from '@/../config/defaultSettings';
import { Footer } from '@/components';
import DoctorPanel from '@/components/DoctorPanel';
import { t } from '@/i18n';
import { authApi } from '@/services/auth';
import { getToken, setToken } from '@/services/authStore';
import { systemApi } from '@/services/system';
import type { DoctorResult } from '@/services/types';
import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { ProFormText, StepsForm } from '@ant-design/pro-components';
import { Helmet, history } from '@umijs/max';
import { Alert, Button, Result } from 'antd';
import React, { useEffect, useRef, useState } from 'react';

/**
 * 首次启动初始化向导（FR-INIT-1）：
 * ① 创建管理员 → ② 环境自检结果 → 进入平台。
 * 已初始化时直接跳登录页。
 */
const Setup: React.FC = () => {
  const [doctor, setDoctor] = useState<DoctorResult | null>(null);
  const [loadingDoctor, setLoadingDoctor] = useState(false);
  const [errorMessage, setErrorMessage] = useState('');
  const createdRef = useRef(false);

  // 已初始化则不允许再次 bootstrap
  useEffect(() => {
    let alive = true;
    authApi
      .bootstrapStatus()
      .then((status) => {
        if (alive && status?.initialized) {
          history.replace('/user/login');
        }
      })
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);

  const loadDoctor = async () => {
    setLoadingDoctor(true);
    try {
      const res = await systemApi.doctor();
      setDoctor(res);
    } catch (e: any) {
      setDoctor(null);
    } finally {
      setLoadingDoctor(false);
    }
  };

  const handleBootstrap = async (values: any) => {
    setErrorMessage('');
    try {
      const result = await authApi.bootstrap({
        username: values.username,
        password: values.password,
      });
      setToken(result.token);
      createdRef.current = true;
      // 拿到 token 后立刻做一次环境自检
      loadDoctor();
      return true;
    } catch (error: any) {
      const msg =
        error?.info?.errorMessage ||
        error?.response?.data?.errorMessage ||
        error?.message ||
        t('setup.setupFailedPleaseRetry');
      setErrorMessage(msg);
      return false;
    }
  };

  return (
    <>
      <Helmet>
        <title>{t('setup.setup', { title: Settings.title })}</title>
      </Helmet>
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          minHeight: '100vh',
          padding: '40px 16px',
          background: '#f5f7fa',
        }}
      >
        <div style={{ flex: 1, maxWidth: 960, width: '100%', margin: '0 auto' }}>
          <StepsForm
            onFinish={async () => {
              window.location.href = '/';
              return true;
            }}
            stepsProps={{ style: { marginBottom: 24 } }}
            submitter={{
              render: (props, dom) => {
                // 最后一步的提交按钮文案改成「进入平台」
                if (props.step === 1) {
                  return (
                    <div style={{ textAlign: 'right' }}>
                      <Button
                        type="primary"
                        onClick={() => {
                          window.location.href = '/';
                        }}
                      >
                        {t('setup.enterPlatform')}{' '}
                      </Button>
                    </div>
                  );
                }
                return dom;
              },
            }}
          >
            <StepsForm.StepForm
              name="admin"
              title={t('setup.createAdministrator')}
              onFinish={handleBootstrap}
              stepProps={{ description: t('setup.setUpTheSuperAdministrator') }}
            >
              {errorMessage && (
                <Alert style={{ marginBottom: 16 }} type="error" showIcon message={errorMessage} />
              )}
              <Alert
                style={{ marginBottom: 16 }}
                type="info"
                showIcon
                message={t('setup.platformSFirstAccountAnd')}
              />
              <ProFormText
                name="username"
                label={t('common.username')}
                placeholder={t('setup.usernamePlaceholder')}
                fieldProps={{ prefix: <UserOutlined />, autoComplete: 'off' }}
                rules={[
                  { required: true, message: t('setup.enterTheUsername') },
                  { pattern: /^[a-zA-Z0-9_.-]{2,32}$/, message: t('common.usernameRule') },
                ]}
              />
              <ProFormText.Password
                name="password"
                label={t('common.password')}
                placeholder={t('setup.least8Characters')}
                fieldProps={{ prefix: <LockOutlined />, autoComplete: 'new-password' }}
                rules={[
                  { required: true, message: t('setup.enterThePassword') },
                  { min: 8, message: t('common.passwordMin8') },
                ]}
              />
              <ProFormText.Password
                name="confirm"
                label={t('setup.confirmPassword')}
                placeholder={t('setup.confirmPasswordPlaceholder')}
                dependencies={['password']}
                fieldProps={{ prefix: <LockOutlined />, autoComplete: 'new-password' }}
                rules={[
                  { required: true, message: t('setup.confirmPasswordRequired') },
                  ({ getFieldValue }) => ({
                    validator(_, value) {
                      if (!value || getFieldValue('password') === value) {
                        return Promise.resolve();
                      }
                      return Promise.reject(new Error(t('common.twoPasswordsDoNotMatch')));
                    },
                  }),
                ]}
              />
            </StepsForm.StepForm>

            <StepsForm.StepForm
              name="doctor"
              title={t('common.doctor')}
              stepProps={{ description: t('setup.checkWhetherTheServerToolchain') }}
              onFinish={async () => true}
            >
              {getToken() ? (
                <DoctorPanel data={doctor} loading={loadingDoctor} />
              ) : (
                <Result
                  status="warning"
                  title={t('setup.administratorAccountMustBeCreated')}
                  extra={
                    <Button type="primary" onClick={() => history.replace('/user/login')}>
                      {t('setup.backToSignIn')}{' '}
                    </Button>
                  }
                />
              )}
            </StepsForm.StepForm>
          </StepsForm>
        </div>
        <Footer />
      </div>
    </>
  );
};

export default Setup;
