import { DoctorPanelWithFetch } from '@/components/DoctorPanel';
import ToolchainPanel from '@/components/ToolchainPanel';
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
import { useAccess } from '@umijs/max';
import { Alert, Card, Descriptions, Spin, Tag, message } from 'antd';
import React, { useCallback, useEffect, useState } from 'react';

const VISIBILITY_OPTIONS = [
  { label: '其他登录用户只读可见（allReadable）', value: 'allReadable' },
  { label: '仅归属者可见（ownerOnly）', value: 'ownerOnly' },
];

const NOTIFY_ON_OPTIONS = [
  { label: '成功', value: 'success' },
  { label: '失败', value: 'failed' },
  { label: '全部', value: 'all' },
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
      messageApi.warning('只有管理员可以修改系统设置');
      return;
    }
    setSaving(true);
    try {
      await systemApi.updateSettings(values);
      messageApi.success('已保存');
      load();
    } catch (e) {
      // 统一提示
    } finally {
      setSaving(false);
    }
  };

  return (
    <PageContainer title="系统设置">
      {contextHolder}
      {!canEdit ? (
        <Alert
          style={{ marginBottom: 16 }}
          type="info"
          showIcon
          message="只读模式"
          description="当前账号不是管理员，可以查看但不能修改系统设置。"
        />
      ) : null}

      <Card title="运行与日志" style={{ marginBottom: 16 }}>
        {loading ? (
          <div style={{ textAlign: 'center', padding: '40px 0' }}>
            <Spin />
          </div>
        ) : (
          <ProForm<SystemSettings>
            initialValues={
              settings || {
                webRoot: '',
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
              searchConfig: { submitText: '保存设置' },
              submitButtonProps: { loading: saving, disabled: !canEdit },
            }}
            onFinish={async (values) => {
              await save(values as SystemSettings);
              return true;
            }}
          >
            <ProFormText
              name="webRoot"
              label="工作根目录"
              width="xl"
              tooltip="项目工作目录的默认根路径，改这里不会搬动已有目录"
              placeholder="/srv/deploy/workspaces"
            />
            <ProFormDigit
              name="maxConcurrentRuns"
              label="全局最大并发运行数"
              width="sm"
              min={1}
              max={200}
              tooltip="超出后新触发的运行进入 queued 排队"
            />
            <ProFormDigit
              name="logRetentionDays"
              label="日志保留天数"
              width="sm"
              min={0}
              tooltip="0 表示不自动清理"
            />
            <ProFormDigit
              name="logMaxBytes"
              label="单条日志大小上限（字节）"
              width="md"
              min={1024}
              tooltip="超出后截断并提示"
              fieldProps={{ step: 1048576 }}
            />
            <ProFormDigit
              name="hookRateLimitPerMinute"
              label="Hook 每分钟限流"
              width="sm"
              min={1}
              tooltip="单 Token 每分钟最大触发次数"
            />
            <ProFormSelect
              name="visibility"
              label="项目可见性"
              width="md"
              options={VISIBILITY_OPTIONS}
            />
            <ProFormSwitch
              name="allowRegistration"
              label="开放注册"
              tooltip="默认关闭；开启后任何人都能注册账号"
            />
          </ProForm>
        )}
      </Card>

      <Card title="通知" style={{ marginBottom: 16 }}>
        {settings ? (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="状态">
              {settings.notify?.enabled ? (
                <Tag color="green">已启用</Tag>
              ) : (
                <Tag>未启用</Tag>
              )}
            </Descriptions.Item>
            <Descriptions.Item label="Webhook 地址">
              {settings.notify?.url || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="触发条件">
              {(settings.notify?.on || []).join('、') || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="当前日志策略">
              {settings.logRetentionDays
                ? `保留 ${settings.logRetentionDays} 天`
                : '不自动清理'}
              {settings.logMaxBytes ? ` · 单条上限 ${formatBytes(settings.logMaxBytes)}` : ''}
            </Descriptions.Item>
            <Descriptions.Item label="服务端 uptime">
              见下方「环境自检」
            </Descriptions.Item>
          </Descriptions>
        ) : (
          <Spin />
        )}
      </Card>

      <DoctorPanelWithFetch />

      <ToolchainPanel />

      <div style={{ marginTop: 12, color: '#999', fontSize: 12 }}>
        提示：日志清理与并发上限都由服务端热读取这些配置；修改后立即生效，无需重启。
        工具链安装同样在服务端执行，装完在「环境自检」里点「重新检测」即可看到结果。
      </div>
    </PageContainer>
  );
};

export default SystemSettingsPage;
