import DoctorPanel from '@/components/DoctorPanel';
import { Footer } from '@/components';
import { authApi } from '@/services/auth';
import { getToken, setToken } from '@/services/authStore';
import { systemApi } from '@/services/system';
import type { DoctorResult } from '@/services/types';
import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { ProFormText, StepsForm } from '@ant-design/pro-components';
import { Helmet, history } from '@umijs/max';
import { Alert, Button, Result } from 'antd';
import React, { useEffect, useRef, useState } from 'react';
import Settings from '@/../config/defaultSettings';

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
        '初始化失败，请重试';
      setErrorMessage(msg);
      return false;
    }
  };

  return (
    <>
      <Helmet>
        <title>初始化 - {Settings.title}</title>
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
                        进入平台
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
              title="创建管理员"
              onFinish={handleBootstrap}
              stepProps={{ description: '设置超级管理员账号（密码至少 8 位）' }}
            >
              {errorMessage && (
                <Alert
                  style={{ marginBottom: 16 }}
                  type="error"
                  showIcon
                  message={errorMessage}
                />
              )}
              <Alert
                style={{ marginBottom: 16 }}
                type="info"
                showIcon
                message="这是平台的第一个账号，拥有全部权限（用户管理、系统设置、所有项目）。"
              />
              <ProFormText
                name="username"
                label="用户名"
                placeholder="例如 admin"
                fieldProps={{ prefix: <UserOutlined />, autoComplete: 'off' }}
                rules={[
                  { required: true, message: '请输入用户名' },
                  { pattern: /^[a-zA-Z0-9_.-]{2,32}$/, message: '2-32 位字母、数字、_ . -' },
                ]}
              />
              <ProFormText.Password
                name="password"
                label="密码"
                placeholder="至少 8 位"
                fieldProps={{ prefix: <LockOutlined />, autoComplete: 'new-password' }}
                rules={[
                  { required: true, message: '请输入密码' },
                  { min: 8, message: '密码长度至少 8 位' },
                ]}
              />
              <ProFormText.Password
                name="confirm"
                label="确认密码"
                placeholder="再次输入密码"
                dependencies={['password']}
                fieldProps={{ prefix: <LockOutlined />, autoComplete: 'new-password' }}
                rules={[
                  { required: true, message: '请再次输入密码' },
                  ({ getFieldValue }) => ({
                    validator(_, value) {
                      if (!value || getFieldValue('password') === value) {
                        return Promise.resolve();
                      }
                      return Promise.reject(new Error('两次输入的密码不一致'));
                    },
                  }),
                ]}
              />
            </StepsForm.StepForm>

            <StepsForm.StepForm
              name="doctor"
              title="环境自检"
              stepProps={{ description: '检查服务端工具链是否就绪' }}
              onFinish={async () => true}
            >
              {getToken() ? (
                <DoctorPanel data={doctor} loading={loadingDoctor} />
              ) : (
                <Result
                  status="warning"
                  title="需要先完成管理员创建"
                  extra={
                    <Button type="primary" onClick={() => history.replace('/user/login')}>
                      返回登录页
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
