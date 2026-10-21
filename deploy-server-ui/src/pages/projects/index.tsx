import ProjectKindIcon from '@/components/ProjectKindIcon';
import { t } from '@/i18n';
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
          ? t('project.triggeredBuildBuildingDependenciesFirst', {
              buildNumber: res.buildNumber,
              depCount: depCount,
            })
          : t('project.triggeredBuild', { buildNumber: res.buildNumber }),
      );
      history.push(`/runs/${res.runId}`);
    } catch (e) {
      // 统一错误提示
    }
  };

  const toggleArchive = async (key: string, archived: boolean) => {
    try {
      await projectApi.archive(key, archived);
      messageApi.success(archived ? t('common.archived') : t('common.unarchivedSuccess'));
      actionRef.current?.reload();
    } catch (e) {
      // 统一错误提示
    }
  };

  const removeProject = async (key: string) => {
    try {
      await projectApi.remove(key);
      messageApi.success(t('common.deleted'));
      actionRef.current?.reload();
    } catch (e) {
      // 统一错误提示
    }
  };

  const columns: ProColumns<ProjectBrief>[] = [
    {
      title: t('common.project'),
      dataIndex: 'keyword',
      hideInTable: true,
      fieldProps: { placeholder: t('common.searchByKeyOrName') },
    },
    {
      title: t('common.type'),
      dataIndex: 'kind',
      hideInTable: true,
      valueType: 'select',
      valueEnum: {
        app: { text: t('common.applicationProject') },
        lib: { text: t('common.buildProject') },
      },
    },
    {
      title: t('common.name'),
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
      title: t('project.dependencies'),
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
      title: t('common.workingDirectory'),
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
      title: t('common.owner'),
      dataIndex: 'ownerName',
      hideInSearch: true,
      width: 110,
      render: (_, row) => row.ownerName || '-',
    },
    {
      title: t('project.commands'),
      dataIndex: 'runCount',
      hideInSearch: true,
      width: 90,
      render: (_, row) => t('project.runs', { runCount: row.runCount }),
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
                  { key: 'with-deps', label: t('common.runWithDependenciesRecommended') },
                  { key: 'only', label: t('common.runThisProjectOnly') },
                ],
                onClick: ({ key }) => runProject(row.key, key === 'with-deps'),
              }}
            >
              <PlayCircleOutlined /> {t('common.execute')}
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
              {t('common.execute')}{' '}
            </Button>
          ),
          <a key="detail" onClick={() => history.push(`/projects/${row.key}`)}>
            {t('common.details')}{' '}
          </a>,
          canOperate ? (
            <a key="edit" onClick={() => history.push(`/projects/${row.key}/edit`)}>
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
              title={t('project.deleteProject')}
              description={t('common.deleteProjectConfirm', { key: row.key })}
              okText={t('common.delete')}
              okButtonProps={{ danger: true }}
              cancelText={t('common.cancel')}
              onConfirm={() => removeProject(row.key)}
            >
              <a style={{ color: '#ff4d4f' }}>{t('common.delete')}</a>
            </Popconfirm>
          ) : null,
        ];
      },
    },
  ];

  return (
    <PageContainer title={t('common.project')}>
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
            // 新建入口必须放在 toolbar.actions 里：ProTable 会用 toolbar.actions
            // 覆盖 toolBarRender 的返回值，写在 toolBarRender 里按钮根本不会渲染。
            <Button
              key="create"
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => history.push('/projects/new')}
            >
              {t('common.newProject')}{' '}
            </Button>,
          ],
        }}
      />
    </PageContainer>
  );
};

export default Projects;
