import { t } from '@/i18n';
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

const VISIBILITY_OPTIONS = [
  { label: t('settings.system.readableByOtherSignedIn'), value: 'allReadable' },
  { label: t('settings.system.visibleToOwnerOnlyOwneronly'), value: 'ownerOnly' },
];

const NOTIFY_ON_OPTIONS = [
  { label: t('common.success'), value: 'success' },
  { label: t('common.failed'), value: 'failed' },
  { label: t('common.all'), value: 'all' },
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
      messageApi.warning(t('settings.system.adminOnly'));
      return;
    }
    setSaving(true);
    try {
      await systemApi.updateSettings(values);
      messageApi.success(t('common.saved'));
      load();
    } catch (e) {
      // 统一提示
    } finally {
      setSaving(false);
    }
  };

  return (
    <PageContainer title={t('common.systemSettings')}>
      {contextHolder}
      {!canEdit ? (
        <Alert
          style={{ marginBottom: 16 }}
          type="info"
          showIcon
          message={t('settings.system.readOnlyMode')}
          description={t('settings.system.accountIsNotAnAdministrator')}
        />
      ) : null}

      <Card title={t('settings.system.runsAndLogs')} style={{ marginBottom: 16 }}>
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
              searchConfig: { submitText: t('settings.system.saveSettings') },
              submitButtonProps: { loading: saving, disabled: !canEdit },
            }}
            onFinish={async (values) => {
              await save(values as SystemSettings);
              return true;
            }}
          >
            <ProFormText
              name="webRoot"
              label={t('common.workspaceRoot')}
              width="xl"
              tooltip={t('settings.system.defaultRootHint')}
              placeholder="/srv/deploy/workspaces"
            />
            <ProFormText
              name="serverUrl"
              label={t('settings.system.serverExternalURL')}
              width="xl"
              tooltip={t('settings.system.deployServerUrlHint')}
              placeholder="http://192.168.31.97:10055"
              rules={[
                {
                  pattern: /^https?:\/\/\S+$/,
                  message: t('settings.system.enterAFullURLStarting'),
                },
              ]}
            />
            <ProFormDigit
              name="maxConcurrentRuns"
              label={t('settings.system.globalMaxConcurrentRuns')}
              width="sm"
              min={1}
              max={200}
              tooltip={t('settings.system.newRunsBeyondThisEnter')}
            />
            <ProFormDigit
              name="logRetentionDays"
              label={t('settings.system.logRetentionDays')}
              width="sm"
              min={0}
              tooltip={t('settings.system.cleanupZeroHint')}
            />
            <ProFormDigit
              name="logMaxBytes"
              label={t('settings.system.maxSizePerLogEntry')}
              width="md"
              min={1024}
              tooltip={t('settings.system.truncatedWithANoticeWhen')}
              fieldProps={{ step: 1048576 }}
            />
            <ProFormDigit
              name="hookRateLimitPerMinute"
              label={t('settings.system.hookRateLimitPerMinute')}
              width="sm"
              min={1}
              tooltip={t('settings.system.maxTriggersPerTokenPer')}
            />
            <ProFormSelect
              name="visibility"
              label={t('settings.system.projectVisibility')}
              width="md"
              options={VISIBILITY_OPTIONS}
            />
            <ProFormSwitch
              name="allowRegistration"
              label={t('settings.system.openRegistration')}
              tooltip={t('settings.system.offByDefaultWhenOn')}
            />
          </ProForm>
        )}
      </Card>

      <Card title={t('settings.system.notifications')} style={{ marginBottom: 16 }}>
        {settings ? (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label={t('common.status')}>
              {settings.notify?.enabled ? (
                <Tag color="green">{t('common.enabled')}</Tag>
              ) : (
                <Tag>{t('common.disabled')}</Tag>
              )}
            </Descriptions.Item>
            <Descriptions.Item label={t('settings.system.webhookURL')}>
              {settings.notify?.url || '-'}
            </Descriptions.Item>
            <Descriptions.Item label={t('settings.system.triggerConditions')}>
              {(settings.notify?.on || []).join('、') || '-'}
            </Descriptions.Item>
            <Descriptions.Item label={t('settings.system.currentLogPolicy')}>
              {settings.logRetentionDays
                ? t('settings.system.keepForDays', { logRetentionDays: settings.logRetentionDays })
                : t('settings.system.noAutomaticCleanup')}
              {settings.logMaxBytes
                ? t('settings.system.maxPerEntry', { p0: formatBytes(settings.logMaxBytes) })
                : ''}
            </Descriptions.Item>
            <Descriptions.Item label={t('settings.system.serverUptime')}>
              {t('settings.system.seeDoctorBelow')}{' '}
            </Descriptions.Item>
          </Descriptions>
        ) : (
          <Spin />
        )}
      </Card>

      <Alert
        style={{ marginBottom: 16 }}
        type="info"
        showIcon
        message={t('settings.system.doctorAndToolchainInstallAre')}
        description={
          <span>
            {t('settings.system.doctorHint')}
            <a onClick={() => history.push('/doctor')}> {t('settings.system.goToDoctor')}</a>
          </span>
        }
      />

      <div style={{ marginTop: 12, color: '#999', fontSize: 12 }}>
        {t('settings.system.hotReloadHint')}{' '}
      </div>
    </PageContainer>
  );
};

export default SystemSettingsPage;
