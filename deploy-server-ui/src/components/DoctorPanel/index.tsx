import { t } from '@/i18n';
import { systemApi } from '@/services/system';
import type { DoctorResult } from '@/services/types';
import { CheckCircleFilled, CloseCircleFilled, ReloadOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Descriptions, Space, Spin, Table, Tag, Typography } from 'antd';
import React, { useCallback, useEffect, useState } from 'react';

const { Text, Paragraph } = Typography;

/**
 * 环境自检面板：展示 GET /system/doctor 的结果。
 * 缺失项给出手动安装提示；一键安装见 ToolchainPanel（需求 FR-INIT-2 / FR-INIT-3）。
 */
const DoctorPanel: React.FC<{ data?: DoctorResult | null; loading?: boolean }> = ({
  data,
  loading,
}) => {
  if (loading) {
    return (
      <div style={{ textAlign: 'center', padding: '32px 0' }}>
        <Spin tip={t('doctor.checkingTheServerEnvironment')}>
          <div style={{ height: 60 }} />
        </Spin>
      </div>
    );
  }
  if (!data) {
    return (
      <Alert
        type="warning"
        showIcon
        message={t('doctor.failedToFetchDoctorResults')}
        description={t('doctor.endpointUnavailable')}
      />
    );
  }

  const missing = (data.checks || []).filter((c) => !c.found);

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Descriptions size="small" column={{ xs: 1, sm: 2, md: 3 }} bordered>
        <Descriptions.Item label={t('common.operatingSystem')}>
          {data.os}/{data.arch}
        </Descriptions.Item>
        <Descriptions.Item label={t('common.hostname')}>{data.hostname || '-'}</Descriptions.Item>
        <Descriptions.Item label={t('doctor.serverVersion')}>
          {data.serverVersion || '-'}
        </Descriptions.Item>
        <Descriptions.Item label={t('doctor.goVersion')}>{data.goVersion || '-'}</Descriptions.Item>
        <Descriptions.Item label={t('doctor.defaultShell')}>
          {data.shell?.selected || '-'}
          {data.shell?.path ? `（${data.shell.path}）` : ''}
        </Descriptions.Item>
        <Descriptions.Item label={t('doctor.availableShells')}>
          {(data.shell?.available || []).join(', ') || '-'}
        </Descriptions.Item>
        <Descriptions.Item label={t('common.workspaceRoot')}>
          <Text code>{data.webRoot || '-'}</Text>{' '}
          {data.webRootWritable ? (
            <Tag color="success">{t('doctor.writable')}</Tag>
          ) : (
            <Tag color="error">{t('doctor.readOnly')}</Tag>
          )}
        </Descriptions.Item>
        <Descriptions.Item label={t('doctor.dataDirectory')}>
          <Text code>{data.dataDir || '-'}</Text>{' '}
          {data.dataDirWritable ? (
            <Tag color="success">{t('doctor.writable')}</Tag>
          ) : (
            <Tag color="error">{t('doctor.readOnly')}</Tag>
          )}
        </Descriptions.Item>
      </Descriptions>

      <Table
        size="small"
        rowKey="name"
        pagination={false}
        dataSource={data.checks || []}
        columns={[
          {
            title: t('doctor.tool'),
            dataIndex: 'name',
            width: 120,
            render: (v: string) => <Text strong>{v}</Text>,
          },
          {
            title: t('common.status'),
            dataIndex: 'found',
            width: 110,
            render: (found: boolean) =>
              found ? (
                <Tag icon={<CheckCircleFilled />} color="success">
                  {t('doctor.ready')}{' '}
                </Tag>
              ) : (
                <Tag icon={<CloseCircleFilled />} color="error">
                  {t('doctor.notFound')}{' '}
                </Tag>
              ),
          },
          {
            title: t('doctor.versionPath'),
            dataIndex: 'version',
            render: (_: string, row) => (
              <Space direction="vertical" size={0}>
                <Text>{row.version || '-'}</Text>
                {row.path ? (
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {row.path}
                  </Text>
                ) : null}
              </Space>
            ),
          },
          {
            title: t('doctor.descriptionInstallHints'),
            dataIndex: 'hint',
            render: (hint: string) => hint || <Text type="secondary">-</Text>,
          },
        ]}
      />

      {missing.length > 0 ? (
        <Alert
          type="warning"
          showIcon
          message={t('doctor.toolsNotFound', {
            length: missing.length,
            p1: missing.map((m) => m.name).join('、'),
          })}
          description={
            <div>
              <Paragraph style={{ marginBottom: 4 }}>
                {t('doctor.installItWithOneClick')}{' '}
              </Paragraph>
              <ul style={{ marginBottom: 0, paddingLeft: 18, listStyle: 'disc' }}>
                {missing.map((m) => (
                  <li key={m.name}>
                    <Text code>{m.name}</Text>：{m.hint || t('doctor.referToTheOfficialInstall')}
                  </li>
                ))}
              </ul>
            </div>
          }
        />
      ) : (
        <Alert type="success" showIcon message={t('doctor.allChecksAreReady')} />
      )}
    </Space>
  );
};

/** 自带拉取逻辑的自检面板（用于系统设置页 / 独立使用） */
export const DoctorPanelWithFetch: React.FC = () => {
  const [data, setData] = useState<DoctorResult | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await systemApi.doctor();
      setData(res);
    } catch (e) {
      setData(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  return (
    <Card
      title={t('common.doctor')}
      extra={
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
          {t('common.recheck')}{' '}
        </Button>
      }
    >
      <DoctorPanel data={data} loading={loading} />
    </Card>
  );
};

export default DoctorPanel;
