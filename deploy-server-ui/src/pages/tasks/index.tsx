import { t } from '@/i18n';
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
      messageApi.success(t('task.taskExecuted', { buildNumber: res.buildNumber }));
      history.push(`/runs/${res.runId}`);
    } catch (e) {
      // 统一错误提示
    }
  };

  const toggleArchive = async (key: string, archived: boolean) => {
    try {
      await taskApi.archive(key, archived);
      messageApi.success(archived ? t('common.archived') : t('common.unarchivedSuccess'));
      actionRef.current?.reload();
    } catch (e) {
      // 统一错误提示
    }
  };

  const removeTask = async (key: string) => {
    try {
      await taskApi.remove(key);
      messageApi.success(t('common.deleted'));
      actionRef.current?.reload();
    } catch (e) {
      // 统一错误提示
    }
  };

  const columns: ProColumns<TaskBrief>[] = [
    {
      title: t('common.task'),
      dataIndex: 'keyword',
      hideInTable: true,
      fieldProps: { placeholder: t('common.searchByKeyOrName') },
    },
    {
      title: t('common.name'),
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
      title: t('common.workingDirectory'),
      dataIndex: 'workdir',
      hideInSearch: true,
      ellipsis: true,
      render: (_, row) => row.workdir,
    },
    {
      title: t('task.stepCountColumn'),
      dataIndex: 'stepCount',
      hideInSearch: true,
      width: 90,
      render: (_, row) => t('task.stepCount', { stepCount: row.stepCount }),
    },
    {
      title: t('common.owner'),
      dataIndex: 'ownerName',
      hideInSearch: true,
      width: 110,
      render: (_, row) => row.ownerName || '-',
    },
    {
      title: t('common.totalRuns'),
      dataIndex: 'runCount',
      hideInSearch: true,
      width: 100,
      render: (_, row) => t('task.runCount', { runCount: row.runCount }),
    },
    {
      title: t('common.lastRun'),
      dataIndex: 'lastRunAt',
      hideInSearch: true,
      width: 210,
      render: (_, row) =>
        row.lastRunAt ? (
          <Space size={6}>
            <Tag color={RUN_STATUS_META[row.lastRunStatus]?.color || 'default'}>
              {RUN_STATUS_META[row.lastRunStatus]?.text || row.lastRunStatus}
            </Tag>
            <span style={{ fontSize: 12 }}>{formatTime(row.lastRunAt)}</span>
          </Space>
        ) : (
          <span style={{ color: '#999' }}>{t('common.neverRun')}</span>
        ),
    },
    {
      title: t('common.status'),
      dataIndex: 'archived',
      hideInSearch: true,
      width: 90,
      render: (_, row) =>
        row.archived ? (
          <Tag color="default">{t('common.archived')}</Tag>
        ) : (
          <Tag color="green">{t('common.active')}</Tag>
        ),
    },
    {
      title: t('common.actions'),
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
            {t('common.execute')}{' '}
          </Button>,
          <a key="detail" onClick={() => history.push(`/tasks/${row.key}`)}>
            {t('common.details')}{' '}
          </a>,
          canOperate ? (
            <a key="edit" onClick={() => history.push(`/tasks/${row.key}/edit`)}>
              {t('common.edit')}{' '}
            </a>
          ) : null,
          canOperate ? (
            <a key="archive" onClick={() => toggleArchive(row.key, !row.archived)}>
              {row.archived ? t('common.unarchive') : t('common.archive')}
            </a>
          ) : null,
          canOperate ? (
            <Popconfirm
              key="delete"
              title={t('task.deleteTask')}
              description={t('common.deleteProjectConfirm', { key: row.key })}
              okText={t('common.delete')}
              okButtonProps={{ danger: true }}
              cancelText={t('common.cancel')}
              onConfirm={() => removeTask(row.key)}
            >
              <a style={{ color: '#ff4d4f' }}>{t('common.delete')}</a>
            </Popconfirm>
          ) : null,
        ];
      },
    },
  ];

  return (
    <PageContainer title={t('common.task')}>
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
                { label: t('common.unarchived'), value: 'active' },
                { label: t('common.archived'), value: 'archived' },
                { label: t('common.all'), value: 'all' },
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
              {t('common.mineOnly')}{' '}
            </Button>,
            <Button
              key="create"
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => history.push('/tasks/new')}
            >
              {t('task.newTask')}{' '}
            </Button>,
          ],
        }}
      />
    </PageContainer>
  );
};

export default Tasks;
