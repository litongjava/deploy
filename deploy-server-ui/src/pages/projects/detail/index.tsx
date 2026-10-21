import ProjectKindIcon from '@/components/ProjectKindIcon';
import { t } from '@/i18n';
import { projectApi } from '@/services/project';
import { runApi } from '@/services/run';
import type { HookToken, HookTokenCreated, Project, Run } from '@/services/types';
import {
  PROJECT_KIND_META,
  RUN_STATUS_META,
  SECRET_PLACEHOLDER,
  formatDuration,
  formatTime,
} from '@/utils/constants';
import { CopyOutlined, PlayCircleOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PageContainer, ProCard, ProDescriptions, ProTable } from '@ant-design/pro-components';
import { history, useParams } from '@umijs/max';
import {
  Alert,
  Badge,
  Button,
  Card,
  Descriptions,
  Dropdown,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import React, { useCallback, useEffect, useRef, useState } from 'react';

const { Text, Paragraph } = Typography;

const ProjectDetail: React.FC = () => {
  const { key = '' } = useParams<{ key: string }>();
  const [project, setProject] = useState<Project | null>(null);
  const [loading, setLoading] = useState(true);
  const [hooks, setHooks] = useState<HookToken[]>([]);
  const [hookLoading, setHookLoading] = useState(false);
  const [plainToken, setPlainToken] = useState<HookTokenCreated | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [running, setRunning] = useState(false);
  const [messageApi, contextHolder] = message.useMessage();
  const runTableRef = useRef<ActionType>();
  const [hookForm] = Form.useForm();

  const load = useCallback(async () => {
    if (!key) return false;
    setLoading(true);
    try {
      const data = await projectApi.detail(key);
      setProject(data);
      return true;
    } catch (e) {
      setProject(null);
      return false;
    } finally {
      setLoading(false);
    }
  }, [key]);

  const loadHooks = useCallback(async () => {
    if (!key) return;
    setHookLoading(true);
    try {
      const list = await projectApi.hooks(key);
      setHooks(list || []);
    } catch (e) {
      setHooks([]);
    } finally {
      setHookLoading(false);
    }
  }, [key]);

  useEffect(() => {
    // 项目都不存在时不必再查 Hook：否则同一个 404 会弹两次错误提示
    load().then((ok) => {
      if (ok) loadHooks();
    });
  }, [load, loadHooks]);

  const handleRun = async (commandId?: number, withDependencies = false) => {
    setRunning(true);
    try {
      const res = await projectApi.run(key, {
        ...(commandId ? { commandId } : {}),
        withDependencies,
      });
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
      // 统一提示
    } finally {
      setRunning(false);
    }
  };

  const copy = async (text: string, tip = t('common.copied')) => {
    try {
      await navigator.clipboard.writeText(text);
      messageApi.success(tip);
      return;
    } catch (e) {
      // 降级方案：clipboard API 在非 HTTPS 下不可用
      const el = document.createElement('textarea');
      el.value = text;
      document.body.appendChild(el);
      el.select();
      try {
        document.execCommand('copy');
        messageApi.success(tip);
      } catch (err) {
        messageApi.error(t('common.copyFailed'));
      }
      document.body.removeChild(el);
    }
  };

  const createHook = async () => {
    const values = await hookForm.validateFields();
    try {
      const res = await projectApi.createHook(key, {
        name: values.name,
        expiresInDays: values.expiresInDays || undefined,
      });
      setCreateOpen(false);
      hookForm.resetFields();
      setPlainToken(res);
      loadHooks();
    } catch (e) {
      // 统一提示
    }
  };

  const resetHook = async (id: number) => {
    try {
      const res = await projectApi.resetHook(id);
      setPlainToken(res);
      loadHooks();
    } catch (e) {
      // 统一提示
    }
  };

  const removeHook = async (id: number) => {
    try {
      await projectApi.removeHook(id);
      messageApi.success(t('common.deleted'));
      loadHooks();
    } catch (e) {
      // 统一提示
    }
  };

  const baseUrl =
    typeof window !== 'undefined' ? window.location.origin : 'https://deploy.example.com';
  const hookExample = `curl -X POST ${baseUrl}/api/v1/hooks/<HOOK_TOKEN> \\
  -H 'Content-Type: application/json' \\
  -d '{"branch":"main","env":{"TAG":"v1.2.3"},"async":true}'`;
  const headerExample = `curl -X POST ${baseUrl}/api/v1/hooks/ \\
  -H 'X-Deploy-Token: <HOOK_TOKEN>' \\
  -H 'Content-Type: application/json' \\
  -d '{"async":false}'`;

  const runColumns: ProColumns<Run>[] = [
    {
      title: t('common.buildNumber'),
      dataIndex: 'buildNumber',
      width: 90,
      render: (_, row) => <a onClick={() => history.push(`/runs/${row.id}`)}>#{row.buildNumber}</a>,
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      width: 100,
      render: (_, row) => (
        <Tag color={RUN_STATUS_META[row.status]?.color}>{RUN_STATUS_META[row.status]?.text}</Tag>
      ),
    },
    {
      title: t('common.trigger'),
      dataIndex: 'triggerUserName',
      width: 140,
      render: (_, row) => `${row.triggerType} / ${row.triggerUserName || '-'}`,
    },
    { title: t('common.branch'), dataIndex: 'requestedRef', width: 110 },
    {
      title: t('common.duration'),
      dataIndex: 'durationMs',
      width: 100,
      render: (_, row) => formatDuration(row.durationMs),
    },
    {
      title: t('common.startTime'),
      dataIndex: 'startedAt',
      width: 170,
      render: (_, row) => formatTime(row.startedAt),
    },
    {
      title: t('common.actions'),
      valueType: 'option',
      width: 100,
      render: (_, row) => [
        <a key="log" onClick={() => history.push(`/runs/${row.id}`)}>
          {t('common.viewLogs')}{' '}
        </a>,
      ],
    },
  ];

  return (
    <PageContainer
      loading={loading}
      title={project ? `${project.name}` : key}
      subTitle={key}
      onBack={() => history.push('/projects')}
      extra={[
        (project?.dependencies || []).length > 0 && !project?.archived ? (
          <Dropdown.Button
            key="run"
            type="primary"
            icon={<PlayCircleOutlined />}
            loading={running}
            disabled={!project || project.archived || !project.canEdit}
            onClick={() => handleRun(undefined, true)}
            menu={{
              items: [
                { key: 'with-deps', label: t('common.runWithDependenciesRecommended') },
                { key: 'only', label: t('common.runThisProjectOnly') },
              ],
              onClick: ({ key }) => handleRun(undefined, key === 'with-deps'),
            }}
          >
            {t('common.execute')}{' '}
          </Dropdown.Button>
        ) : (
          <Button
            key="run"
            type="primary"
            icon={<PlayCircleOutlined />}
            loading={running}
            disabled={!project || project.archived || !project.canEdit}
            onClick={() => handleRun()}
          >
            {t('common.execute')}{' '}
          </Button>
        ),
        <Button
          key="edit"
          disabled={!project || !project.canEdit}
          onClick={() => history.push(`/projects/${key}/edit`)}
        >
          {t('common.edit')}{' '}
        </Button>,
      ]}
    >
      {contextHolder}
      {project?.archived ? (
        <Alert
          style={{ marginBottom: 16 }}
          type="warning"
          showIcon
          message={t('project.projectIsArchivedAndCannot')}
          description={t('project.useItAgainUnarchiveIt')}
        />
      ) : null}

      {project?.dependencyPlan && (project.dependencies || []).length > 0 ? (
        project.dependencyPlan.ok ? (
          <Alert
            style={{ marginBottom: 16 }}
            type="success"
            showIcon
            message={t('project.dependencyBuildOrderRunIncludes')}
            description={
              <Space wrap size={4}>
                {project.dependencyPlan.order.map((d) => (
                  <Tag key={d.key} color="purple">
                    <a onClick={() => history.push(`/projects/${d.key}`)}>{d.key}</a>
                  </Tag>
                ))}
                <span>→</span>
                <Tag color={PROJECT_KIND_META[project.kind || 'app']?.color}>{project.key}</Tag>
              </Space>
            }
          />
        ) : (
          <Alert
            style={{ marginBottom: 16 }}
            type="error"
            showIcon
            message={t('project.dependencyUnavailableReject')}
            description={project.dependencyPlan.error}
          />
        )
      ) : null}

      {(project?.dependents || []).length > 0 ? (
        <Alert
          style={{ marginBottom: 16 }}
          type="info"
          showIcon
          message={t('project.dependedOnBy')}
          description={
            <Space wrap size={4}>
              {(project?.dependents || []).map((d) => (
                <Tag key={d.key}>
                  <a onClick={() => history.push(`/projects/${d.key}`)}>{d.name || d.key}</a>
                </Tag>
              ))}
            </Space>
          }
        />
      ) : null}

      <ProCard gutter={16} wrap>
        <ProCard colSpan={{ xs: 24, lg: 14 }} title={t('common.basicInformation')} bordered>
          <ProDescriptions<Project>
            column={2}
            dataSource={project || undefined}
            columns={[
              { title: t('project.projectKey'), dataIndex: 'key', copyable: true },
              { title: t('common.name'), dataIndex: 'name' },
              {
                title: t('project.projectType'),
                dataIndex: 'kind',
                // 与项目列表一致：图标 + 悬浮提示，不再铺一段「应用项目 / 构建项目」文字
                render: (_, row) => <ProjectKindIcon kind={row.kind} />,
              },
              {
                title: t('project.dependentProjects'),
                dataIndex: 'dependencies',
                render: (_, row) =>
                  (row.dependencies || []).length === 0 ? (
                    <Text type="secondary">{t('project.none')}</Text>
                  ) : (
                    <Space size={4} wrap>
                      {(row.dependencies || []).map((k) => (
                        <Tag key={k} color="purple">
                          <a onClick={() => history.push(`/projects/${k}`)}>{k}</a>
                        </Tag>
                      ))}
                    </Space>
                  ),
              },
              {
                title: t('common.workingDirectory'),
                dataIndex: 'workdir',
                span: 2,
                copyable: true,
              },
              { title: 'Shell', dataIndex: 'shell' },
              { title: t('common.concurrencyPolicy'), dataIndex: 'concurrency' },
              {
                title: t('common.owner'),
                dataIndex: 'ownerName',
              },
              {
                title: t('common.defaultTimeout'),
                dataIndex: 'timeoutSec',
                render: (v) => (v ? t('project.seconds', { v: v }) : t('common.unlimited')),
              },
              {
                title: t('common.hookTrigger'),
                dataIndex: 'hookEnabled',
                render: (v) =>
                  v ? <Badge status="success" text={t('common.enabled')} /> : t('common.closed'),
              },
              {
                title: t('project.gitPreparation'),
                dataIndex: 'useGit',
                render: (v, row) =>
                  v ? (
                    <Space direction="vertical" size={0}>
                      <Text>{row.gitUrl}</Text>
                      <Text type="secondary" style={{ fontSize: 12 }}>
                        {t('project.branch', { branch: row.gitBranch })}
                        {row.gitSubdir ? t('project.subdir', { gitSubdir: row.gitSubdir }) : ''}
                        {row.gitDepth ? t('project.depth', { gitDepth: row.gitDepth }) : ''}
                      </Text>
                    </Space>
                  ) : (
                    t('common.disabled')
                  ),
              },
              {
                title: t('common.totalRuns'),
                dataIndex: 'runCount',
              },
              {
                title: t('common.lastRun'),
                dataIndex: 'lastRunAt',
                span: 2,
                render: (v, row) =>
                  v ? (
                    <Space>
                      <Tag color={RUN_STATUS_META[row.lastRunStatus as Run['status']]?.color}>
                        {RUN_STATUS_META[row.lastRunStatus as Run['status']]?.text ||
                          row.lastRunStatus}
                      </Tag>
                      <span>{formatTime(row.lastRunAt)}</span>
                    </Space>
                  ) : (
                    t('common.neverRun')
                  ),
              },
            ]}
          />
        </ProCard>

        <ProCard colSpan={{ xs: 24, lg: 10 }} title={t('common.environmentVariables')} bordered>
          {(project?.envs || []).length === 0 ? (
            <Empty
              description={t('common.noEnvironmentVariables')}
              image={Empty.PRESENTED_IMAGE_SIMPLE}
            />
          ) : (
            <Table
              rowKey={(r) => String(r.id ?? r.key)}
              size="small"
              pagination={false}
              dataSource={project?.envs || []}
              columns={[
                { title: t('common.variableName'), dataIndex: 'key' },
                {
                  title: t('common.value'),
                  dataIndex: 'value',
                  render: (_, row) =>
                    row.isSecret ? (
                      <Text code>{SECRET_PLACEHOLDER}</Text>
                    ) : (
                      <Text code>{row.value}</Text>
                    ),
                },
                {
                  title: t('common.type'),
                  dataIndex: 'isSecret',
                  width: 90,
                  render: (v) =>
                    v ? <Tag color="orange">Secret</Tag> : <Tag>{t('project.standard')}</Tag>,
                },
              ]}
            />
          )}
        </ProCard>
      </ProCard>

      <Card
        title={t('project.commandList')}
        style={{ marginTop: 16 }}
        extra={
          project?.canEdit ? (
            <Button onClick={() => history.push(`/projects/${key}/edit`)}>
              {t('project.editCommands')}
            </Button>
          ) : null
        }
      >
        {(project?.commands || []).length === 0 ? (
          <Empty description={t('project.noCommandsConfiguredAddThem')} />
        ) : (
          <Table
            rowKey="id"
            size="small"
            pagination={false}
            dataSource={project?.commands || []}
            expandable={{
              expandedRowRender: (row) => (
                <Paragraph>
                  <pre
                    style={{
                      margin: 0,
                      padding: 12,
                      background: '#fafafa',
                      borderRadius: 4,
                      maxHeight: 320,
                      overflow: 'auto',
                      fontFamily: 'Menlo, Consolas, monospace',
                      fontSize: 12,
                      whiteSpace: 'pre-wrap',
                    }}
                  >
                    {row.script}
                  </pre>
                </Paragraph>
              ),
            }}
            columns={[
              { title: t('common.order'), dataIndex: 'sortOrder', width: 70 },
              { title: t('common.name'), dataIndex: 'name' },
              {
                title: t('common.enable'),
                dataIndex: 'enabled',
                width: 80,
                render: (v) =>
                  v ? t('common.yes') : <Text type="secondary">{t('common.no')}</Text>,
              },
              {
                title: t('common.failureStrategy'),
                dataIndex: 'failStrategy',
                width: 110,
                render: (v) => (v === 'continue' ? t('project.continue') : t('project.abort')),
              },
              {
                title: t('common.overrides'),
                width: 220,
                render: (_, row) => (
                  <Space size={4} wrap>
                    {row.workdirOverride ? <Tag>cwd: {row.workdirOverride}</Tag> : null}
                    {row.timeoutSec ? (
                      <Tag>{t('common.timeoutSec', { sec: row.timeoutSec })}</Tag>
                    ) : null}
                    {row.shellOverride ? <Tag>shell: {row.shellOverride}</Tag> : null}
                    {!row.workdirOverride && !row.timeoutSec && !row.shellOverride ? (
                      <Text type="secondary">-</Text>
                    ) : null}
                  </Space>
                ),
              },
              {
                title: t('common.actions'),
                width: 110,
                render: (_, row) => (
                  <Button
                    type="link"
                    size="small"
                    disabled={!project?.canEdit || project?.archived}
                    onClick={() => handleRun(row.id)}
                  >
                    {t('common.runOnlyThisProject')}{' '}
                  </Button>
                ),
              },
            ]}
          />
        )}
      </Card>

      <Card title="Hook Token" style={{ marginTop: 16 }}>
        <Alert
          style={{ marginBottom: 16 }}
          type="info"
          showIcon
          message={t('project.hookTriggerURLNoSign')}
          description={
            <Space direction="vertical" style={{ width: '100%' }} size={8}>
              <Paragraph style={{ margin: 0 }}>
                POST <Text code>{`${baseUrl}/api/v1/hooks/{token}`}</Text>
                {t('project.hookHeaderHint')} <Text code>X-Deploy-Token</Text>
                {t('project.hookBodyHint')} <Text code>?wait=true</Text> {t('common.or')}{' '}
                <Text code>"async": false</Text> {t('project.hookSyncHint')}
              </Paragraph>
              <pre
                style={{
                  margin: 0,
                  padding: 12,
                  background: '#fafafa',
                  borderRadius: 4,
                  fontFamily: 'Menlo, Consolas, monospace',
                  fontSize: 12,
                  whiteSpace: 'pre-wrap',
                }}
              >
                {hookExample}
              </pre>
              <Space>
                <Button
                  size="small"
                  icon={<CopyOutlined />}
                  onClick={() => copy(hookExample, t('project.exampleCopied'))}
                >
                  {t('project.copyExample')}{' '}
                </Button>
                <Button
                  size="small"
                  onClick={() => copy(headerExample, t('project.exampleCopied'))}
                >
                  {t('project.copyHeaderExample')}{' '}
                </Button>
              </Space>
            </Space>
          }
        />

        <Space style={{ marginBottom: 12 }}>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            disabled={!project?.canEdit}
            onClick={() => setCreateOpen(true)}
          >
            {t('common.newHookToken')}{' '}
          </Button>
          <Button icon={<ReloadOutlined />} onClick={loadHooks} loading={hookLoading}>
            {t('common.refresh')}{' '}
          </Button>
        </Space>

        <Table<HookToken>
          rowKey="id"
          size="small"
          loading={hookLoading}
          pagination={false}
          dataSource={hooks}
          locale={{ emptyText: <Empty description={t('common.noHookTokenYet')} /> }}
          columns={[
            { title: t('common.name'), dataIndex: 'name' },
            {
              title: t('common.prefix'),
              dataIndex: 'prefix',
              render: (v) => <Text code>{v}…</Text>,
            },
            {
              title: t('common.status'),
              dataIndex: 'enabled',
              width: 90,
              render: (v) =>
                v ? (
                  <Tag color="green">{t('common.enable')}</Tag>
                ) : (
                  <Tag>{t('common.disable')}</Tag>
                ),
            },
            {
              title: t('common.expiresAt'),
              dataIndex: 'expiresAt',
              width: 170,
              render: (v) => (v ? formatTime(v) : t('common.neverExpires')),
            },
            {
              title: t('common.calls'),
              dataIndex: 'callCount',
              width: 100,
            },
            {
              title: t('common.lastUsed'),
              dataIndex: 'lastUsedAt',
              width: 220,
              render: (_, row) =>
                row.lastUsedAt ? (
                  <Space size={6}>
                    <span>{formatTime(row.lastUsedAt)}</span>
                    {row.lastUsedIp ? <Text type="secondary">{row.lastUsedIp}</Text> : null}
                  </Space>
                ) : (
                  <Text type="secondary">{t('common.neverUsed')}</Text>
                ),
            },
            {
              title: t('common.actions'),
              width: 160,
              render: (_, row) => (
                <Space>
                  <Popconfirm
                    title={t('project.originalTokenIsInvalidatedImmediately')}
                    description={t('project.regenerateThisHookToken')}
                    onConfirm={() => resetHook(row.id)}
                    okText={t('common.reset')}
                    cancelText={t('common.cancel')}
                  >
                    <a>{t('common.reset')}</a>
                  </Popconfirm>
                  <Popconfirm
                    title={t('project.callersUsingThisTokenWill')}
                    onConfirm={() => removeHook(row.id)}
                    okText={t('common.delete')}
                    okButtonProps={{ danger: true }}
                    cancelText={t('common.cancel')}
                  >
                    <a style={{ color: '#ff4d4f' }}>{t('common.delete')}</a>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Card title={t('common.runHistory')} style={{ marginTop: 16 }}>
        <ProTable<Run>
          rowKey="id"
          actionRef={runTableRef}
          search={false}
          options={{ density: false, setting: false }}
          pagination={{ pageSize: 10, showSizeChanger: true }}
          request={async (params) => {
            const res = await runApi.list({
              page: params.current || 1,
              pageSize: params.pageSize || 10,
              projectKey: key,
            });
            return { data: res?.list || [], total: res?.total || 0, success: true };
          }}
          columns={runColumns}
        />
      </Card>

      {/* 明文 token 只显示一次 */}
      <Modal
        open={!!plainToken}
        title={t('common.hookTokenGenerated')}
        onCancel={() => setPlainToken(null)}
        footer={[
          <Button key="ok" type="primary" onClick={() => setPlainToken(null)}>
            {t('common.iHaveSavedIt')}{' '}
          </Button>,
        ]}
        width={640}
      >
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={t('common.copyAndSaveItNow')}
          description={t('project.serverStoresOnlyAHash')}
        />
        {plainToken ? (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label={t('common.name')}>{plainToken.name}</Descriptions.Item>
            <Descriptions.Item label={t('common.plaintextToken')}>
              <Space>
                <Text code copyable style={{ wordBreak: 'break-all' }}>
                  {plainToken.token}
                </Text>
                <Button
                  size="small"
                  icon={<CopyOutlined />}
                  onClick={() => copy(plainToken.token, t('common.tokenCopied'))}
                >
                  {t('common.copy')}{' '}
                </Button>
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label={t('common.expiresAt')}>
              {plainToken.expiresAt ? formatTime(plainToken.expiresAt) : t('common.neverExpires')}
            </Descriptions.Item>
          </Descriptions>
        ) : null}
      </Modal>

      <Modal
        open={createOpen}
        title={t('common.newHookToken')}
        onCancel={() => setCreateOpen(false)}
        onOk={createHook}
        okText={t('common.create')}
        cancelText={t('common.cancel')}
        destroyOnClose
      >
        <Form form={hookForm} layout="vertical" preserve={false}>
          <Form.Item
            name="name"
            label={t('common.name')}
            rules={[{ required: true, message: t('project.enterANameSoCallers') }]}
          >
            <Input placeholder={t('project.exampleCredentialName')} />
          </Form.Item>
          <Form.Item
            name="expiresInDays"
            label={t('common.validForDays')}
            tooltip={t('common.leaveEmptyForNoExpiry')}
          >
            <InputNumber min={1} style={{ width: '100%' }} placeholder={t('common.example90')} />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
};

export default ProjectDetail;
