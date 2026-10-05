import ProjectKindIcon from '@/components/ProjectKindIcon';
import { projectApi } from '@/services/project';
import type { ProjectBrief } from '@/services/types';
import { RUN_STATUS_META, formatTime } from '@/utils/constants';
import { PlayCircleOutlined, PlusOutlined } from '@ant-design/icons';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PageContainer, ProTable } from '@ant-design/pro-components';
import { history, useAccess } from '@umijs/max';
import { Button, Dropdown, Popconfirm, Segmented, Space, Tag, message } from 'antd';
import React, { useRef, useState } from 'react';

const Projects: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const access = useAccess();
  const [messageApi, contextHolder] = message.useMessage();
  const [archivedFilter, setArchivedFilter] = useState<'active' | 'archived' | 'all'>('active');
  const [mineOnly, setMineOnly] = useState(false);

  const runProject = async (key: string, withDependencies = false) => {
    try {
      const res = await projectApi.run(key, { withDependencies });
      const depCount = res?.dependencies?.length || 0;
      messageApi.success(
        withDependencies && depCount > 0
          ? `已触发构建 #${res.buildNumber}，先构建 ${depCount} 个依赖`
          : `已触发构建 #${res.buildNumber}`,
      );
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
      title: '类型',
      dataIndex: 'kind',
      hideInTable: true,
      valueType: 'select',
      valueEnum: {
        app: { text: '应用项目' },
        lib: { text: '构建项目' },
      },
    },
    {
      title: '名称',
      dataIndex: 'name',
      hideInSearch: true,
      render: (_, row) => (
        <Space direction="vertical" size={0}>
          <Space size={6}>
            <a onClick={() => history.push(`/projects/${row.key}`)}>{row.name}</a>
            {/* 类型只用图标表示（悬浮出文字），不再显示「应用项目 / 构建项目」标签 */}
            <ProjectKindIcon kind={row.kind} />
          </Space>
          <span style={{ fontSize: 12, color: '#999' }}>{row.key}</span>
        </Space>
      ),
    },
    {
      title: '依赖',
      dataIndex: 'dependencies',
      hideInSearch: true,
      width: 180,
      render: (_, row) =>
        (row.dependencies || []).length === 0 ? (
          <span style={{ color: '#999' }}>-</span>
        ) : (
          <Space size={4} wrap>
            {(row.dependencies || []).map((k) => (
              <Tag key={k} color="purple">
                {k}
              </Tag>
            ))}
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
        const hasDeps = (row.dependencies || []).length > 0;
        return [
          hasDeps ? (
            <Dropdown.Button
              key="run"
              size="small"
              disabled={row.archived}
              onClick={() => runProject(row.key, true)}
              menu={{
                items: [
                  { key: 'with-deps', label: '含依赖执行（推荐）' },
                  { key: 'only', label: '仅执行本项目' },
                ],
                onClick: ({ key }) => runProject(row.key, key === 'with-deps'),
              }}
            >
              <PlayCircleOutlined /> 执行
            </Dropdown.Button>
          ) : (
            <Button
              key="run"
              type="link"
              size="small"
              icon={<PlayCircleOutlined />}
              disabled={row.archived}
              onClick={() => runProject(row.key)}
            >
              执行
            </Button>
          ),
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
        scroll={{ x: 1200 }}
        search={{ labelWidth: 'auto' }}
        options={{ density: false }}
        request={async (params) => {
          const { current, pageSize, keyword, kind } = params as any;
          const res = await projectApi.list({
            page: current || 1,
            pageSize: pageSize || 20,
            keyword: keyword || undefined,
            kind: kind || undefined,
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
            // 新建入口必须放在 toolbar.actions 里：ProTable 会用 toolbar.actions
            // 覆盖 toolBarRender 的返回值，写在 toolBarRender 里按钮根本不会渲染。
            <Button
              key="create"
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => history.push('/projects/new')}
            >
              新建项目
            </Button>,
          ],
        }}
      />
    </PageContainer>
  );
};

export default Projects;
