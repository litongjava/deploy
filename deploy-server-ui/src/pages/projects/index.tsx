import { projectApi } from '@/services/project';
import type { ProjectBrief } from '@/services/types';
import { RUN_STATUS_META, formatTime } from '@/utils/constants';
import { PlayCircleOutlined, PlusOutlined } from '@ant-design/icons';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PageContainer, ProTable } from '@ant-design/pro-components';
import { history, useAccess } from '@umijs/max';
import { Button, Popconfirm, Segmented, Space, Tag, message } from 'antd';
import React, { useRef, useState } from 'react';

const Projects: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const access = useAccess();
  const [messageApi, contextHolder] = message.useMessage();
  const [archivedFilter, setArchivedFilter] = useState<'active' | 'archived' | 'all'>('active');
  const [mineOnly, setMineOnly] = useState(false);

  const runProject = async (key: string) => {
    try {
      const res = await projectApi.run(key);
      messageApi.success(`已触发构建 #${res.buildNumber}`);
      history.push(`/runs/${res.runId}`);
    } catch (e) {
      // 统一错误提示
    }
  };

  const toggleArchive = async (key: string, archived: boolean) => {
    try {
      await projectApi.archive(key, archived);
      messageApi.success(archived ? '已归档' : '已取消归档');
      actionRef.current?.reload();
    } catch (e) {
      // 统一错误提示
    }
  };

  const removeProject = async (key: string) => {
    try {
      await projectApi.remove(key);
      messageApi.success('已删除');
      actionRef.current?.reload();
    } catch (e) {
      // 统一错误提示
    }
  };

  const columns: ProColumns<ProjectBrief>[] = [
    {
      title: '项目',
      dataIndex: 'keyword',
      hideInTable: true,
      fieldProps: { placeholder: '按 key 或名称搜索' },
    },
    {
      title: '名称',
      dataIndex: 'name',
      hideInSearch: true,
      render: (_, row) => (
        <Space direction="vertical" size={0}>
          <a onClick={() => history.push(`/projects/${row.key}`)}>{row.name}</a>
          <span style={{ fontSize: 12, color: '#999' }}>{row.key}</span>
        </Space>
      ),
    },
    {
      title: '工作目录',
      dataIndex: 'workdir',
      hideInSearch: true,
      ellipsis: true,
      render: (_, row) => (
        <Space direction="vertical" size={0}>
          <span>{row.workdir}</span>
          {row.useGit ? (
            <span style={{ fontSize: 12, color: '#999' }}>
              Git: {row.gitUrl} @ {row.gitBranch}
            </span>
          ) : null}
        </Space>
      ),
    },
    {
      title: '归属',
      dataIndex: 'ownerName',
      hideInSearch: true,
      width: 110,
      render: (_, row) => row.ownerName || '-',
    },
    {
      title: '命令数',
      dataIndex: 'runCount',
      hideInSearch: true,
      width: 90,
      render: (_, row) => `${row.runCount} 次运行`,
    },
    {
      title: '上次运行',
      dataIndex: 'lastRunAt',
      hideInSearch: true,
      width: 210,
      render: (_, row) =>
        row.lastRunAt ? (
          <Space size={6}>
            <Tag color={RUN_STATUS_META[row.lastRunStatus as never]?.color || 'default'}>
              {RUN_STATUS_META[row.lastRunStatus as never]?.text || row.lastRunStatus}
            </Tag>
            <span style={{ fontSize: 12 }}>{formatTime(row.lastRunAt)}</span>
          </Space>
        ) : (
          <span style={{ color: '#999' }}>从未运行</span>
        ),
    },
    {
      title: '状态',
      dataIndex: 'archived',
      hideInSearch: true,
      width: 90,
      render: (_, row) =>
        row.archived ? <Tag color="default">已归档</Tag> : <Tag color="green">正常</Tag>,
    },
    {
      title: '操作',
      valueType: 'option',
      width: 230,
      fixed: 'right',
      render: (_, row) => {
        const canOperate = row.canEdit || access.canAdmin;
        return [
          <Button
            key="run"
            type="link"
            size="small"
            icon={<PlayCircleOutlined />}
            disabled={row.archived}
            onClick={() => runProject(row.key)}
          >
            执行
          </Button>,
          <a key="detail" onClick={() => history.push(`/projects/${row.key}`)}>
            详情
          </a>,
          canOperate ? (
            <a key="edit" onClick={() => history.push(`/projects/${row.key}/edit`)}>
              编辑
            </a>
          ) : null,
          canOperate ? (
            <a key="archive" onClick={() => toggleArchive(row.key, !row.archived)}>
              {row.archived ? '取消归档' : '归档'}
            </a>
          ) : null,
          canOperate ? (
            <Popconfirm
              key="delete"
              title="删除项目"
              description={`将连带删除运行历史与日志文件，且不可恢复。确认删除 ${row.key}？`}
              okText="删除"
              okButtonProps={{ danger: true }}
              cancelText="取消"
              onConfirm={() => removeProject(row.key)}
            >
              <a style={{ color: '#ff4d4f' }}>删除</a>
            </Popconfirm>
          ) : null,
        ];
      },
    },
  ];

  return (
    <PageContainer title="项目">
      {contextHolder}
      <ProTable<ProjectBrief>
        rowKey="key"
        actionRef={actionRef}
        columns={columns}
        cardBordered
        scroll={{ x: 1100 }}
        search={{ labelWidth: 'auto' }}
        options={{ density: false }}
        request={async (params) => {
          const { current, pageSize, keyword } = params as any;
          const res = await projectApi.list({
            page: current || 1,
            pageSize: pageSize || 20,
            keyword: keyword || undefined,
            archived: archivedFilter === 'all' ? undefined : archivedFilter === 'archived',
            mine: mineOnly || undefined,
          });
          return { data: res?.list || [], total: res?.total || 0, success: true };
        }}
        toolbar={{
          actions: [
            <Segmented
              key="archived"
              value={archivedFilter}
              onChange={(v) => {
                setArchivedFilter(v as any);
                actionRef.current?.reload();
              }}
              options={[
                { label: '未归档', value: 'active' },
                { label: '已归档', value: 'archived' },
                { label: '全部', value: 'all' },
              ]}
            />,
            <Button
              key="mine"
              type={mineOnly ? 'primary' : 'default'}
              onClick={() => {
                setMineOnly((v) => !v);
                actionRef.current?.reload();
              }}
            >
              只看我的
            </Button>,
          ],
        }}
        toolBarRender={() => [
          <Button
            key="create"
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => history.push('/projects/edit')}
          >
            新建项目
          </Button>,
        ]}
      />
    </PageContainer>
  );
};

export default Projects;
