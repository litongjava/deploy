import { taskApi } from '@/services/task';
import type { TaskBrief } from '@/services/types';
import { RUN_STATUS_META, formatTime } from '@/utils/constants';
import { PlayCircleOutlined, PlusOutlined } from '@ant-design/icons';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PageContainer, ProTable } from '@ant-design/pro-components';
import { history, useAccess } from '@umijs/max';
import { Button, Popconfirm, Segmented, Space, Tag, message } from 'antd';
import React, { useRef, useState } from 'react';

/**
 * 任务列表。
 *
 * 与项目列表的差别：任务没有类型、没有依赖、没有 Git，
 * 所以少了「类型/依赖」两列，执行也不再分「含依赖 / 仅本项目」。
 */
const Tasks: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const access = useAccess();
  const [messageApi, contextHolder] = message.useMessage();
  const [archivedFilter, setArchivedFilter] = useState<'active' | 'archived' | 'all'>('active');
  const [mineOnly, setMineOnly] = useState(false);

  const runTask = async (key: string) => {
    try {
      const res = await taskApi.run(key);
      messageApi.success(`已执行任务 #${res.buildNumber}`);
      history.push(`/runs/${res.runId}`);
    } catch (e) {
      // 统一错误提示
    }
  };

  const toggleArchive = async (key: string, archived: boolean) => {
    try {
      await taskApi.archive(key, archived);
      messageApi.success(archived ? '已归档' : '已取消归档');
      actionRef.current?.reload();
    } catch (e) {
      // 统一错误提示
    }
  };

  const removeTask = async (key: string) => {
    try {
      await taskApi.remove(key);
      messageApi.success('已删除');
      actionRef.current?.reload();
    } catch (e) {
      // 统一错误提示
    }
  };

  const columns: ProColumns<TaskBrief>[] = [
    {
      title: '任务',
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
          <a onClick={() => history.push(`/tasks/${row.key}`)}>{row.name}</a>
          <span style={{ fontSize: 12, color: '#999' }}>{row.key}</span>
        </Space>
      ),
    },
    {
      title: '工作目录',
      dataIndex: 'workdir',
      hideInSearch: true,
      ellipsis: true,
      render: (_, row) => row.workdir,
    },
    {
      title: '步骤数',
      dataIndex: 'stepCount',
      hideInSearch: true,
      width: 90,
      render: (_, row) => `${row.stepCount} 步`,
    },
    {
      title: '归属',
      dataIndex: 'ownerName',
      hideInSearch: true,
      width: 110,
      render: (_, row) => row.ownerName || '-',
    },
    {
      title: '累计运行',
      dataIndex: 'runCount',
      hideInSearch: true,
      width: 100,
      render: (_, row) => `${row.runCount} 次`,
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
      width: 200,
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
            onClick={() => runTask(row.key)}
          >
            执行
          </Button>,
          <a key="detail" onClick={() => history.push(`/tasks/${row.key}`)}>
            详情
          </a>,
          canOperate ? (
            <a key="edit" onClick={() => history.push(`/tasks/${row.key}/edit`)}>
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
              title="删除任务"
              description={`将连带删除运行历史与日志文件，且不可恢复。确认删除 ${row.key}？`}
              okText="删除"
              okButtonProps={{ danger: true }}
              cancelText="取消"
              onConfirm={() => removeTask(row.key)}
            >
              <a style={{ color: '#ff4d4f' }}>删除</a>
            </Popconfirm>
          ) : null,
        ];
      },
    },
  ];

  return (
    <PageContainer title="任务">
      {contextHolder}
      <ProTable<TaskBrief>
        rowKey="key"
        actionRef={actionRef}
        columns={columns}
        cardBordered
        scroll={{ x: 1100 }}
        search={{ labelWidth: 'auto' }}
        options={{ density: false }}
        request={async (params) => {
          const { current, pageSize, keyword } = params as any;
          const res = await taskApi.list({
            page: current || 1,
            pageSize: pageSize || 20,
            keyword: keyword || undefined,
            archived:
              archivedFilter === 'all' ? 'all' : archivedFilter === 'archived' ? true : false,
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
            <Button
              key="create"
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => history.push('/tasks/new')}
            >
              新建任务
            </Button>,
          ],
        }}
      />
    </PageContainer>
  );
};

export default Tasks;
