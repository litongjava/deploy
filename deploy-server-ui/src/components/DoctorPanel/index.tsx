import { systemApi } from '@/services/system';
import type { DoctorResult } from '@/services/types';
import { CheckCircleFilled, CloseCircleFilled, ReloadOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Descriptions, Space, Spin, Table, Tag, Typography } from 'antd';
import React, { useCallback, useEffect, useState } from 'react';
import { t } from '@/i18n';

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
        <Spin tip={t('正在检测服务端环境…')}>
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
        message={t('未能获取环境自检结果')}
        description={t('后端 /system/doctor 接口不可用，或当前未登录。')}
      />
    );
  }

  const missing = (data.checks || []).filter((c) => !c.found);

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Descriptions size="small" column={{ xs: 1, sm: 2, md: 3 }} bordered>
        <Descriptions.Item label={t('操作系统')}>
          {data.os}/{data.arch}
        </Descriptions.Item>
        <Descriptions.Item label={t('主机名')}>{data.hostname || '-'}</Descriptions.Item>
        <Descriptions.Item label={t('服务端版本')}>{data.serverVersion || '-'}</Descriptions.Item>
        <Descriptions.Item label={t('Go 版本')}>{data.goVersion || '-'}</Descriptions.Item>
        <Descriptions.Item label={t('默认 Shell')}>
          {data.shell?.selected || '-'}
          {data.shell?.path ? `（${data.shell.path}）` : ''}
        </Descriptions.Item>
        <Descriptions.Item label={t('可用 Shell')}>
          {(data.shell?.available || []).join(', ') || '-'}
        </Descriptions.Item>
        <Descriptions.Item label={t('工作根目录')}>
          <Text code>{data.webRoot || '-'}</Text>{' '}
          {data.webRootWritable ? (
            <Tag color="success">{t('可写')}</Tag>
          ) : (
            <Tag color="error">{t('不可写')}</Tag>
          )}
        </Descriptions.Item>
        <Descriptions.Item label={t('数据目录')}>
          <Text code>{data.dataDir || '-'}</Text>{' '}
          {data.dataDirWritable ? (
            <Tag color="success">{t('可写')}</Tag>
          ) : (
            <Tag color="error">{t('不可写')}</Tag>
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
            title: t('工具'),
            dataIndex: 'name',
            width: 120,
            render: (v: string) => <Text strong>{v}</Text>,
          },
          {
            title: t('状态'),
            dataIndex: 'found',
            width: 110,
            render: (found: boolean) =>
              found ? (
                <Tag icon={<CheckCircleFilled />} color="success">
                   {t('已就绪')} </Tag>
              ) : (
                <Tag icon={<CloseCircleFilled />} color="error">
                   {t('未找到')} </Tag>
              ),
          },
          {
            title: t('版本 / 路径'),
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
            title: t('说明 / 安装提示'),
            dataIndex: 'hint',
            render: (hint: string) => hint || <Text type="secondary">-</Text>,
          },
        ]}
      />

      {missing.length > 0 ? (
        <Alert
          type="warning"
          showIcon
          message={t('有 {length} 项工具未找到：{p1}', { length: missing.length, p1: missing
            .map((m) => m.name)
            .join('、') })}
          description={
            <div>
              <Paragraph style={{ marginBottom: 4 }}>
                 {t('可以在下面「工具链一键安装」里一键装上（管理员），或按下面的提示在这台服务器上手动装：')} </Paragraph>
              <ul style={{ marginBottom: 0, paddingLeft: 18, listStyle: 'disc' }}>
                {missing.map((m) => (
                  <li key={m.name}>
                    <Text code>{m.name}</Text>：{m.hint || t('请参考对应工具的官方安装文档')}
                  </li>
                ))}
              </ul>
            </div>
          }
        />
      ) : (
        <Alert type="success" showIcon message={t('所有检测项均已就绪')} />
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
      title={t('环境自检')}
      extra={
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
           {t('重新检测')} </Button>
      }
    >
      <DoctorPanel data={data} loading={loading} />
    </Card>
  );
};

export default DoctorPanel;
