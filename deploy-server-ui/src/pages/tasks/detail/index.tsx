import { t } from '@/i18n';
import { runApi } from '@/services/run';
import { taskApi } from '@/services/task';
import type { HookToken, HookTokenCreated, Run, Task, TaskStepDraft } from '@/services/types';
import {
  CONCURRENCY_OPTIONS,
  RUN_STATUS_META,
  SECRET_PLACEHOLDER,
  SHELL_OPTIONS,
  TRIGGER_TYPE_META,
  formatDuration,
  formatTime,
} from '@/utils/constants';
import { EditOutlined, PlayCircleOutlined } from '@ant-design/icons';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PageContainer, ProCard, ProDescriptions, ProTable } from '@ant-design/pro-components';
import { history, useParams } from '@umijs/max';
import {
  Alert,
  Badge,
  Button,
  Card,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import React, { useCallback, useEffect, useRef, useState } from 'react';

const { Text, Paragraph } = Typography;

/**
 * 任务详情。
 *
 * 结构与项目详情页一致（基本信息 / 环境变量 / 步骤 / Hook Token / 运行历史），
 * 去掉的是项目独有的部分：项目类型、依赖构建、Git 准备阶段。
 */
const TaskDetail: React.FC = () => {
  const { key } = useParams<{ key: string }>();
  const [task, setTask] = useState<Task>();
  const [hooks, setHooks] = useState<HookToken[]>([]);
  const [loading, setLoading] = useState(false);
  const [plainToken, setPlainToken] = useState<HookTokenCreated>();
  const [hookModalOpen, setHookModalOpen] = useState(false);
  const [hookForm] = Form.useForm<{ name: string; expiresInDays?: number }>();
  const [messageApi, contextHolder] = message.useMessage();
  const runActionRef = useRef<ActionType>();

  const load = useCallback(async () => {
    if (!key) return;
    setLoading(true);
    try {
      const t = await taskApi.detail(key);
      setTask(t);
      const h = await taskApi.hooks(key);
      setHooks(h || []);
    } catch (e) {
      // 统一错误提示
    } finally {
      setLoading(false);
    }
  }, [key]);

  useEffect(() => {
    load();
  }, [load]);

  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      messageApi.success(t('common.copied'));
    } catch {
      // 非 HTTPS 或旧浏览器下 Clipboard API 不可用，退回 execCommand。
      const el = document.createElement('textarea');
      el.value = text;
      document.body.appendChild(el);
      el.select();
      document.execCommand('copy');
      document.body.removeChild(el);
      messageApi.success(t('common.copied'));
    }
  };

  const handleRun = async (stepIndex?: number) => {
    if (!key) return;
    try {
      const res = await taskApi.run(key, stepIndex ? { commandId: stepIndex } : {});
      messageApi.success(t('task.taskExecuted', { buildNumber: res.buildNumber }));
      history.push(`/runs/${res.runId}`);
    } catch (e) {
      // 统一错误提示
    }
  };

  const createHook = async () => {
    if (!key) return;
    try {
      const values = await hookForm.validateFields();
      const created = await taskApi.createHook(key, {
        name: values.name,
        expiresInDays: values.expiresInDays,
      });
      setHookModalOpen(false);
      hookForm.resetFields();
      setPlainToken(created);
      load();
    } catch (e) {
      // 校验失败或统一错误提示
    }
  };

  const stepColumns = [
    {
      title: t('common.order'),
      dataIndex: 'sortOrder',
      width: 70,
      render: (_: any, __: TaskStepDraft, index: number) => index + 1,
    },
    { title: t('common.name'), dataIndex: 'name' },
    {
      title: t('common.enable'),
      dataIndex: 'enabled',
      width: 80,
      render: (v: boolean) =>
        v ? <Tag color="green">{t('common.enable')}</Tag> : <Tag>{t('common.disable')}</Tag>,
    },
    {
      title: t('common.failureStrategy'),
      dataIndex: 'failStrategy',
      width: 110,
      render: (v: string) =>
        v === 'continue' ? t('task.continueOnError') : t('task.abortOnFailure'),
    },
    {
      title: t('common.overrides'),
      dataIndex: 'workdirOverride',
      width: 220,
      render: (_: any, row: TaskStepDraft) => (
        <Space size={4} wrap>
          {row.workdirOverride ? <Tag>cwd: {row.workdirOverride}</Tag> : null}
          {row.timeoutSec ? <Tag>{t('common.timeoutSec', { sec: row.timeoutSec })}</Tag> : null}
          {row.shellOverride ? <Tag>shell: {row.shellOverride}</Tag> : null}
          {!row.workdirOverride && !row.timeoutSec && !row.shellOverride ? (
            <span style={{ color: '#999' }}>-</span>
          ) : null}
        </Space>
      ),
    },
    {
      title: t('common.actions'),
      valueType: 'option' as const,
      width: 110,
      render: (_: any, __: TaskStepDraft, index: number) => [
        <a key="run" onClick={() => handleRun(index + 1)} title={t('task.runOnlyThisStepTask')}>
          {t('common.runOnlyThisProject')}{' '}
        </a>,
      ],
    },
  ];

  const runColumns: ProColumns<Run>[] = [
    {
      title: t('common.buildNumber'),
      dataIndex: 'buildNumber',
      width: 110,
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
      title: t('common.triggerMode'),
      dataIndex: 'triggerType',
      width: 100,
      render: (_, row) => TRIGGER_TYPE_META[row.triggerType] || row.triggerType,
    },
    {
      title: t('common.triggeredBy'),
      dataIndex: 'triggerUserName',
      width: 150,
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
      title: t('common.duration'),
      dataIndex: 'durationMs',
      width: 100,
      render: (_, row) => (row.durationMs ? formatDuration(row.durationMs) : '-'),
    },
    {
      title: t('common.startTime'),
      dataIndex: 'startedAt',
      width: 180,
      render: (_, row) => formatTime(row.startedAt),
    },
    {
      title: t('common.actions'),
      valueType: 'option',
      width: 90,
      render: (_, row) => [
        <a key="log" onClick={() => history.push(`/runs/${row.id}`)}>
          {t('common.viewLogs')}
        </a>,
      ],
    },
  ];

  const baseUrl = typeof window !== 'undefined' ? window.location.origin : '';
  const isRunning = hooks.some((h) => h.enabled);

  return (
    <PageContainer
      title={task?.name || key}
      subTitle={key}
      loading={loading}
      onBack={() => history.push('/tasks')}
      extra={[
        <Button
          key="run"
          type="primary"
          icon={<PlayCircleOutlined />}
          disabled={!task || task.archived}
          onClick={() => handleRun()}
        >
          {t('task.runTask')}{' '}
        </Button>,
        <Button
          key="edit"
          icon={<EditOutlined />}
          disabled={!task?.canEdit}
          onClick={() => history.push(`/tasks/${key}/edit`)}
        >
          {t('common.edit')}{' '}
        </Button>,
      ]}
    >
      {contextHolder}
      {task?.archived ? (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message={t('task.taskIsArchivedAndCannot')}
        />
      ) : null}

      <ProCard gutter={16} wrap style={{ marginBottom: 16 }}>
        <ProCard colSpan={{ xs: 24, lg: 14 }} title={t('common.basicInformation')} bordered>
          <ProDescriptions<Task> column={2} dataSource={task}>
            <ProDescriptions.Item label={t('task.taskKey')} copyable>
              {task?.key}
            </ProDescriptions.Item>
            <ProDescriptions.Item label={t('common.name')}>{task?.name}</ProDescriptions.Item>
            <ProDescriptions.Item label={t('common.workingDirectory')} span={2} copyable>
              {task?.workdir}
            </ProDescriptions.Item>
            <ProDescriptions.Item label="Shell">
              {SHELL_OPTIONS.find((o) => o.value === task?.shell)?.label ||
                task?.shell ||
                t('task.auto')}
            </ProDescriptions.Item>
            <ProDescriptions.Item label={t('common.concurrencyPolicy')}>
              {CONCURRENCY_OPTIONS.find((o) => o.value === task?.concurrency)?.label ||
                task?.concurrency}
            </ProDescriptions.Item>
            <ProDescriptions.Item label={t('common.defaultTimeout')}>
              {task?.timeoutSec
                ? t('task.seconds', { timeoutSec: task.timeoutSec })
                : t('common.unlimited')}
            </ProDescriptions.Item>
            <ProDescriptions.Item label={t('common.owner')}>
              {task?.ownerName || '-'}
            </ProDescriptions.Item>
            <ProDescriptions.Item label={t('common.hookTrigger')}>
              {task?.hookEnabled ? (
                <Badge status="success" text={t('common.enabled')} />
              ) : (
                <Badge status="default" text={t('common.closed')} />
              )}
            </ProDescriptions.Item>
            <ProDescriptions.Item label={t('common.totalRuns')}>
              {t('task.runCountDetail', { count: task?.runCount ?? 0 })}
            </ProDescriptions.Item>
            <ProDescriptions.Item label={t('common.lastRun')}>
              {task?.lastRunAt ? (
                <Space size={6}>
                  <Tag color={RUN_STATUS_META[task.lastRunStatus]?.color || 'default'}>
                    {RUN_STATUS_META[task.lastRunStatus]?.text || task.lastRunStatus}
                  </Tag>
                  <span>{formatTime(task.lastRunAt)}</span>
                </Space>
              ) : (
                t('common.neverRun')
              )}
            </ProDescriptions.Item>
            <ProDescriptions.Item label={t('common.description')} span={2}>
              {task?.description || '-'}
            </ProDescriptions.Item>
          </ProDescriptions>
        </ProCard>

        <ProCard colSpan={{ xs: 24, lg: 10 }} title={t('common.environmentVariables')} bordered>
          {(task?.envs || []).length === 0 ? (
            <Empty description={t('common.noEnvironmentVariables')} />
          ) : (
            <Table
              rowKey="key"
              size="small"
              pagination={false}
              dataSource={task?.envs || []}
              columns={[
                { title: t('common.variableName'), dataIndex: 'key' },
                {
                  title: t('common.value'),
                  dataIndex: 'value',
                  render: (_: any, row: any) =>
                    row.isSecret ? <Text code>{SECRET_PLACEHOLDER}</Text> : row.value || '-',
                },
                {
                  title: 'Secret',
                  dataIndex: 'isSecret',
                  width: 80,
                  render: (v: boolean) =>
                    v ? <Tag color="orange">{t('common.yes')}</Tag> : <Tag>{t('common.no')}</Tag>,
                },
              ]}
            />
          )}
        </ProCard>
      </ProCard>

      <Card
        title={t('task.stepList')}
        style={{ marginBottom: 16 }}
        extra={
          task?.canEdit ? (
            <Button size="small" onClick={() => history.push(`/tasks/${key}/edit`)}>
              {t('task.editSteps')}{' '}
            </Button>
          ) : null
        }
      >
        {(task?.steps || []).length === 0 ? (
          <Empty description={t('task.noStepsYet')} />
        ) : (
          <Table
            rowKey={(row, index) => `${index}-${row.name}`}
            size="small"
            pagination={false}
            dataSource={task?.steps || []}
            columns={stepColumns as any}
            expandable={{
              expandedRowRender: (row: TaskStepDraft) => (
                <Paragraph>
                  <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{row.script}</pre>
                </Paragraph>
              ),
            }}
          />
        )}
      </Card>

      <Card
        title="Hook Token"
        style={{ marginBottom: 16 }}
        extra={
          <Space>
            <Button size="small" disabled={!task?.canEdit} onClick={() => setHookModalOpen(true)}>
              {t('common.newHookToken')}{' '}
            </Button>
            <Button size="small" onClick={load}>
              {t('common.refresh')}{' '}
            </Button>
          </Space>
        }
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message={t('task.triggerWithoutSigningInTasks')}
          description={
            <Space direction="vertical" size={4} style={{ width: '100%' }}>
              <Space size={6} wrap>
                <Text code>POST {baseUrl}/api/v1/hooks/&lt;HOOK_TOKEN&gt;</Text>
                <a onClick={() => copy(`curl -X POST ${baseUrl}/api/v1/hooks/<HOOK_TOKEN>`)}>
                  {t('task.copyCurl')}{' '}
                </a>
              </Space>
              <Space size={6} wrap>
                <Text code>{t('task.hookExample', { baseUrl })}</Text>
                <Tooltip title={t('task.tokenUrlHint')}>
                  <span style={{ color: '#999', fontSize: 12 }}>{t('task.recommended')}</span>
                </Tooltip>
              </Space>
              <span style={{ color: '#999', fontSize: 12 }}>
                {t('task.append')} <Text code>?wait=true</Text>{' '}
                {t('task.canWaitSynchronouslyForThe')}
              </span>
            </Space>
          }
        />
        {isRunning ? null : (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message={t('task.noHookTokenIsAvailable')}
          />
        )}
        <Table<HookToken>
          rowKey="id"
          size="small"
          pagination={false}
          dataSource={hooks}
          locale={{ emptyText: <Empty description={t('common.noHookTokenYet')} /> }}
          columns={[
            { title: t('common.name'), dataIndex: 'name' },
            {
              title: t('common.prefix'),
              dataIndex: 'prefix',
              render: (v: string) => <Text code>{v}…</Text>,
            },
            {
              title: t('common.status'),
              dataIndex: 'enabled',
              render: (v: boolean) =>
                v ? (
                  <Badge status="success" text={t('common.enable')} />
                ) : (
                  <Badge status="default" text={t('task.disable')} />
                ),
            },
            {
              title: t('common.expiresAt'),
              dataIndex: 'expiresAt',
              render: (v: string | null) => (v ? formatTime(v) : t('common.neverExpires')),
            },
            { title: t('common.calls'), dataIndex: 'callCount', width: 90 },
            {
              title: t('common.lastUsed'),
              dataIndex: 'lastUsedAt',
              render: (_: any, row: HookToken) =>
                row.lastUsedAt ? (
                  <Space direction="vertical" size={0}>
                    <span>{formatTime(row.lastUsedAt)}</span>
                    {row.lastUsedIp ? (
                      <span style={{ fontSize: 12, color: '#999' }}>{row.lastUsedIp}</span>
                    ) : null}
                  </Space>
                ) : (
                  t('task.unused')
                ),
            },
            {
              title: t('common.actions'),
              render: (_: any, row: HookToken) => [
                <Popconfirm
                  key="reset"
                  title={t('task.resetToken')}
                  description={t('task.oldTokenIsInvalidatedImmediately')}
                  onConfirm={async () => {
                    const created = await taskApi.resetHook(row.id);
                    setPlainToken(created);
                    load();
                  }}
                >
                  <a>{t('common.reset')}</a>
                </Popconfirm>,
                <Popconfirm
                  key="del"
                  title={t('task.deleteToken')}
                  description={t('task.cannotBeUndone')}
                  onConfirm={async () => {
                    await taskApi.removeHook(row.id);
                    messageApi.success(t('common.deleted'));
                    load();
                  }}
                >
                  <a style={{ color: '#ff4d4f' }}>{t('common.delete')}</a>
                </Popconfirm>,
              ],
            },
          ]}
        />
      </Card>

      <Card title={t('common.runHistory')}>
        <ProTable<Run>
          rowKey="id"
          actionRef={runActionRef}
          columns={runColumns}
          search={false}
          options={{ density: false }}
          pagination={{ pageSize: 10, showSizeChanger: true }}
          request={async (params) => {
            const res = await runApi.list({
              page: params.current || 1,
              pageSize: params.pageSize || 10,
              taskKey: key,
            });
            return { data: res?.list || [], total: res?.total || 0, success: true };
          }}
        />
      </Card>

      <Modal
        open={!!plainToken}
        title={t('common.hookTokenGenerated')}
        onCancel={() => setPlainToken(undefined)}
        onOk={() => setPlainToken(undefined)}
        okText={t('common.iHaveSavedIt')}
        cancelButtonProps={{ style: { display: 'none' } }}
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message={t('task.serverStoresOnlyAHash')}
        />
        <Paragraph copyable={{ text: plainToken?.token }}>
          <Text code>{plainToken?.token}</Text>
        </Paragraph>
        <Paragraph>
          <Text code>{`curl -X POST ${baseUrl}/api/v1/hooks/${plainToken?.token || ''}`}</Text>
        </Paragraph>
      </Modal>

      <Modal
        open={hookModalOpen}
        title={t('common.newHookToken')}
        onCancel={() => setHookModalOpen(false)}
        onOk={createHook}
        okText={t('common.create')}
      >
        <Form form={hookForm} layout="vertical">
          <Form.Item
            name="name"
            label={t('common.name')}
            rules={[{ required: true, message: t('common.enterAName') }]}
          >
            <Input placeholder={t('task.exampleName')} />
          </Form.Item>
          <Form.Item
            name="expiresInDays"
            label={t('task.validityDays')}
            tooltip={t('common.leaveEmptyForNoExpiry')}
          >
            <InputNumber
              min={1}
              style={{ width: '100%' }}
              placeholder={t('task.emptyNeverExpires')}
            />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
};

export default TaskDetail;
