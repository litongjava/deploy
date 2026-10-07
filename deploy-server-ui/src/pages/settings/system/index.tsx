import { systemApi } from '@/services/system';
import type { SystemSettings } from '@/services/types';
import { formatBytes } from '@/utils/constants';
import {
  PageContainer,
  ProForm,
  ProFormDigit,
  ProFormSelect,
  ProFormSwitch,
  ProFormText,
} from '@ant-design/pro-components';
import { history, useAccess } from '@umijs/max';
import { Alert, Card, Descriptions, Spin, Tag, message } from 'antd';
import React, { useCallback, useEffect, useState } from 'react';
import { t } from '@/i18n';

const VISIBILITY_OPTIONS = [
  { label: t('其他登录用户只读可见（allReadable）'), value: 'allReadable' },
  { label: t('仅归属者可见（ownerOnly）'), value: 'ownerOnly' },
];

const NOTIFY_ON_OPTIONS = [
  { label: t('成功'), value: 'success' },
  { label: t('失败'), value: 'failed' },
  { label: t('全部'), value: 'all' },
];

const SystemSettingsPage: React.FC = () => {
  const access = useAccess();
  const [settings, setSettings] = useState<SystemSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [messageApi, contextHolder] = message.useMessage();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await systemApi.getSettings();
      setSettings(data);
    } catch (e) {
      setSettings(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const canEdit = !!access.canAdmin;

  const save = async (values: SystemSettings) => {
    if (!canEdit) {
      messageApi.warning(t('只有管理员可以修改系统设置'));
      return;
    }
    setSaving(true);
    try {
      await systemApi.updateSettings(values);
      messageApi.success(t('已保存'));
      load();
    } catch (e) {
      // 统一提示
    } finally {
      setSaving(false);
    }
  };

  return (
    <PageContainer title={t('系统设置')}>
      {contextHolder}
      {!canEdit ? (
        <Alert
          style={{ marginBottom: 16 }}
          type="info"
          showIcon
          message={t('只读模式')}
          description={t('当前账号不是管理员，可以查看但不能修改系统设置。')}
        />
      ) : null}

      <Card title={t('运行与日志')} style={{ marginBottom: 16 }}>
        {loading ? (
          <div style={{ textAlign: 'center', padding: '40px 0' }}>
            <Spin />
          </div>
        ) : (
          <ProForm<SystemSettings>
            initialValues={
              settings || {
                webRoot: '',
                serverUrl: '',
                maxConcurrentRuns: 5,
                logRetentionDays: 30,
                logMaxBytes: 104857600,
                hookRateLimitPerMinute: 60,
                visibility: 'allReadable',
                allowRegistration: false,
                notify: { enabled: false, url: '', on: ['failed'] },
              }
            }
            disabled={!canEdit}
            submitter={{
              searchConfig: { submitText: t('保存设置') },
              submitButtonProps: { loading: saving, disabled: !canEdit },
            }}
            onFinish={async (values) => {
              await save(values as SystemSettings);
              return true;
            }}
          >
            <ProFormText
              name="webRoot"
              label={t('工作根目录')}
              width="xl"
              tooltip={t('项目工作目录的默认根路径，改这里不会搬动已有目录')}
              placeholder="/srv/deploy/workspaces"
            />
            <ProFormText
              name="serverUrl"
              label={t('服务端外部地址')}
              width="xl"
              tooltip={t('注入给脚本的 DEPLOY_SERVER_URL。对外地址变了在这里改，保存后对后续部署立即生效，不用重启服务；留空表示回退到启动参数（-server-url / DEPLOY_SERVER_URL）里的默认值')}
              placeholder="http://192.168.31.97:10055"
              rules={[
                {
                  pattern: /^https?:\/\/\S+$/,
                  message: t('需填 http:// 或 https:// 开头的完整地址'),
                },
              ]}
            />
            <ProFormDigit
              name="maxConcurrentRuns"
              label={t('全局最大并发运行数')}
              width="sm"
              min={1}
              max={200}
              tooltip={t('超出后新触发的运行进入 queued 排队')}
            />
            <ProFormDigit
              name="logRetentionDays"
              label={t('日志保留天数')}
              width="sm"
              min={0}
              tooltip={t('0 表示不自动清理')}
            />
            <ProFormDigit
              name="logMaxBytes"
              label={t('单条日志大小上限（字节）')}
              width="md"
              min={1024}
              tooltip={t('超出后截断并提示')}
              fieldProps={{ step: 1048576 }}
            />
            <ProFormDigit
              name="hookRateLimitPerMinute"
              label={t('Hook 每分钟限流')}
              width="sm"
              min={1}
              tooltip={t('单 Token 每分钟最大触发次数')}
            />
            <ProFormSelect
              name="visibility"
              label={t('项目可见性')}
              width="md"
              options={VISIBILITY_OPTIONS}
            />
            <ProFormSwitch
              name="allowRegistration"
              label={t('开放注册')}
              tooltip={t('默认关闭；开启后任何人都能注册账号')}
            />
          </ProForm>
        )}
      </Card>

      <Card title={t('通知')} style={{ marginBottom: 16 }}>
        {settings ? (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label={t('状态')}>
              {settings.notify?.enabled ? (
                <Tag color="green">{t('已启用')}</Tag>
              ) : (
                <Tag>{t('未启用')}</Tag>
              )}
            </Descriptions.Item>
            <Descriptions.Item label={t('Webhook 地址')}>
              {settings.notify?.url || '-'}
            </Descriptions.Item>
            <Descriptions.Item label={t('触发条件')}>
              {(settings.notify?.on || []).join('、') || '-'}
            </Descriptions.Item>
            <Descriptions.Item label={t('当前日志策略')}>
              {settings.logRetentionDays
                ? t('保留 {logRetentionDays} 天', { logRetentionDays: settings.logRetentionDays })
                : t('不自动清理')}
              {settings.logMaxBytes ? t(' · 单条上限 {p0}', { p0: formatBytes(settings.logMaxBytes) }) : ''}
            </Descriptions.Item>
            <Descriptions.Item label={t('服务端 uptime')}>
               {t('见下方「环境自检」')} </Descriptions.Item>
          </Descriptions>
        ) : (
          <Spin />
        )}
      </Card>

      <Alert
        style={{ marginBottom: 16 }}
        type="info"
        showIcon
        message={t('环境自检与工具链安装已独立成菜单')}
        description={
          <span>
{t('服务端环境、缺失工具链与一键安装都在侧边栏的「环境自检」页：')}
            <a onClick={() => history.push('/doctor')}>  {t('去环境自检 →')}</a>
          </span>
        }
      />

      <div style={{ marginTop: 12, color: '#999', fontSize: 12 }}>
         {t('提示：日志清理与并发上限都由服务端热读取这些配置；修改后立即生效，无需重启。')} </div>
    </PageContainer>
  );
};

export default SystemSettingsPage;
