import { runApi } from '@/services/run';
import type { Run } from '@/services/types';
import { RUN_STATUS_META, RUN_STATUS_OPTIONS, formatDuration, formatTime } from '@/utils/constants';
import { PageContainer, ProTable } from '@ant-design/pro-components';
import type { ProColumns } from '@ant-design/pro-components';
import { history } from '@umijs/max';
import { Space, Tag } from 'antd';
import React from 'react';

const Runs: React.FC = () => {
  const columns: ProColumns<Run>[] = [
    {
      title: '项目',
      dataIndex: 'projectKey',
      fieldProps: { placeholder: '项目 key' },
      render: (_, row) => (
        <a onClick={() => history.push(`/projects/${row.projectKey}`)}>
          {row.projectName || row.projectKey}
        </a>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      valueType: 'select',
      fieldProps: { options: RUN_STATUS_OPTIONS, placeholder: '全部状态' },
      render: (_, row) => (
        <Tag color={RUN_STATUS_META[row.status]?.color}>{RUN_STATUS_META[row.status]?.text}</Tag>
      ),
    },
    {
      title: '构建号',
      dataIndex: 'buildNumber',
      hideInSearch: true,
      width: 100,
      render: (_, row) => <a onClick={() => history.push(`/runs/${row.id}`)}>#{row.buildNumber}</a>,
    },
    {
      title: '触发方式',
      dataIndex: 'triggerType',
      hideInSearch: true,
      width: 110,
    },
    {
      title: '触发人',
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
      title: '分支',
      dataIndex: 'requestedRef',
      hideInSearch: true,
      width: 110,
      render: (_, row) => row.requestedRef || '-',
    },
    {
      title: '耗时',
      dataIndex: 'durationMs',
      hideInSearch: true,
      width: 100,
      render: (_, row) => formatDuration(row.durationMs),
    },
    {
      title: '退出码',
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
      title: '开始时间',
      dataIndex: 'startedAt',
      hideInSearch: true,
      width: 175,
      render: (_, row) => formatTime(row.startedAt),
    },
    {
      title: '操作',
      valueType: 'option',
      width: 100,
      fixed: 'right',
      render: (_, row) => [
        <a key="detail" onClick={() => history.push(`/runs/${row.id}`)}>
          日志
        </a>,
      ],
    },
  ];

  return (
    <PageContainer title="运行记录">
      <ProTable<Run>
        rowKey="id"
        columns={columns}
        cardBordered
        scroll={{ x: 1200 }}
        search={{ labelWidth: 'auto' }}
        options={{ density: false }}
        pagination={{ pageSize: 20, showSizeChanger: true }}
        request={async (params) => {
          const { current, pageSize, projectKey, status } = params as any;
          const res = await runApi.list({
            page: current || 1,
            pageSize: pageSize || 20,
            projectKey: projectKey || undefined,
            status: status || undefined,
          });
          return { data: res?.list || [], total: res?.total || 0, success: true };
        }}
      />
    </PageContainer>
  );
};

export default Runs;
