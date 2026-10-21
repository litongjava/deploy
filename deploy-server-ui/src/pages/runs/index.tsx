import { t } from '@/i18n';
import { runApi } from '@/services/run';
import type { Run } from '@/services/types';
import { RUN_STATUS_META, RUN_STATUS_OPTIONS, formatDuration, formatTime } from '@/utils/constants';
import type { ProColumns } from '@ant-design/pro-components';
import { PageContainer, ProTable } from '@ant-design/pro-components';
import { history } from '@umijs/max';
import { Space, Tag } from 'antd';
import React from 'react';

const Runs: React.FC = () => {
  const columns: ProColumns<Run>[] = [
    {
      // 一列同时放项目与任务：两者都是「一次运行」的对象，
      // 分成两列会让其中一列永远是空的。targetKind 决定链接去哪一侧。
      title: t('run.targets'),
      dataIndex: 'projectKey',
      fieldProps: { placeholder: t('run.projectTaskKey') },
      render: (_, row) => {
        const isTask = row.targetKind === 'task';
        const key = row.targetKey || row.projectKey;
        return (
          <Space size={6}>
            {isTask ? (
              <Tag color="geekblue">{t('common.task')}</Tag>
            ) : (
              <Tag color="blue">{t('common.project')}</Tag>
            )}
            <a onClick={() => history.push(isTask ? `/tasks/${key}` : `/projects/${key}`)}>
              {row.targetName || row.projectName || key}
            </a>
          </Space>
        );
      },
    },
    {
      title: t('common.type'),
      dataIndex: 'targetKind',
      hideInTable: true,
      valueType: 'select',
      valueEnum: {
        project: { text: t('common.project') },
        task: { text: t('common.task') },
      },
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      valueType: 'select',
      fieldProps: { options: RUN_STATUS_OPTIONS, placeholder: t('run.allStatuses') },
      render: (_, row) => (
        <Tag color={RUN_STATUS_META[row.status]?.color}>{RUN_STATUS_META[row.status]?.text}</Tag>
      ),
    },
    {
      title: t('common.buildNumber'),
      dataIndex: 'buildNumber',
      hideInSearch: true,
      width: 100,
      render: (_, row) => <a onClick={() => history.push(`/runs/${row.id}`)}>#{row.buildNumber}</a>,
    },
    {
      title: t('common.triggerMode'),
      dataIndex: 'triggerType',
      hideInSearch: true,
      width: 110,
    },
    {
      title: t('common.triggeredBy'),
      dataIndex: 'triggerUserName',
      hideInSearch: true,
      width: 130,
      render: (_, row) => (
        <Space direction="vertical" size={0}>
          <span>{row.triggerUserName || '-'}</span>
          {row.triggerIp ? (
            <span style={{ fontSize: 12, color: '#999' }}>{row.triggerIp}</span>
          ) : null}
        </Space>
      ),
    },
    {
      title: t('common.branch'),
      dataIndex: 'requestedRef',
      hideInSearch: true,
      width: 110,
      render: (_, row) => row.requestedRef || '-',
    },
    {
      title: t('common.duration'),
      dataIndex: 'durationMs',
      hideInSearch: true,
      width: 100,
      render: (_, row) => formatDuration(row.durationMs),
    },
    {
      title: t('common.exitCode'),
      dataIndex: 'exitCode',
      hideInSearch: true,
      width: 90,
      render: (_, row) =>
        row.exitCode === null || row.exitCode === undefined ? (
          '-'
        ) : (
          <Tag color={row.exitCode === 0 ? 'green' : 'red'}>{row.exitCode}</Tag>
        ),
    },
    {
      title: t('common.startTime'),
      dataIndex: 'startedAt',
      hideInSearch: true,
      width: 175,
      render: (_, row) => formatTime(row.startedAt),
    },
    {
      title: t('common.actions'),
      valueType: 'option',
      width: 100,
      fixed: 'right',
      render: (_, row) => [
        <a key="detail" onClick={() => history.push(`/runs/${row.id}`)}>
          {t('common.logs')}{' '}
        </a>,
      ],
    },
  ];

  return (
    <PageContainer title={t('run.runs')}>
      <ProTable<Run>
        rowKey="id"
        columns={columns}
        cardBordered
        scroll={{ x: 1200 }}
        search={{ labelWidth: 'auto' }}
        options={{ density: false }}
        pagination={{ pageSize: 20, showSizeChanger: true }}
        request={async (params) => {
          const { current, pageSize, projectKey, status, targetKind } = params as any;
          const res = await runApi.list({
            page: current || 1,
            pageSize: pageSize || 20,
            projectKey: projectKey || undefined,
            targetKind: targetKind || undefined,
            status: status || undefined,
          });
          return { data: res?.list || [], total: res?.total || 0, success: true };
        }}
      />
    </PageContainer>
  );
};

export default Runs;
