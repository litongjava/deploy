import LogTerminal from '@/components/LogTerminal';
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
import { ArrowLeftOutlined, EditOutlined, PlayCircleOutlined } from '@ant-design/icons';
import { PageContainer, ProCard, ProDescriptions, ProTable } from '@ant-design/pro-components';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
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
      messageApi.success('已复制');
    } catch {
      // 非 HTTPS 或旧浏览器下 Clipboard API 不可用，退回 execCommand。
      const el = document.createElement('textarea');
      el.value = text;
      document.body.appendChild(el);
      el.select();
      document.execCommand('copy');
      document.body.removeChild(el);
      messageApi.success('已复制');
    }
  };

  const handleRun = async (stepIndex?: number) => {
    if (!key) return;
    try {
      const res = await taskApi.run(key, stepIndex ? { commandId: stepIndex } : {});
      messageApi.success(`已执行任务 #${res.buildNumber}`);
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
      title: '顺序',
      dataIndex: 'sortOrder',
      width: 70,
      render: (_: any, __: TaskStepDraft, index: number) => index + 1,
    },
    { title: '名称', dataIndex: 'name' },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="green">启用</Tag> : <Tag>禁用</Tag>),
    },
    {
      title: '失败策略',
      dataIndex: 'failStrategy',
      width: 110,
      render: (v: string) => (v === 'continue' ? '忽略错误继续' : '失败即停'),
    },
    {
      title: '覆盖项',
      dataIndex: 'workdirOverride',
      width: 220,
      render: (_: any, row: TaskStepDraft) => (
        <Space size={4} wrap>
          {row.workdirOverride ? <Tag>cwd: {row.workdirOverride}</Tag> : null}
          {row.timeoutSec ? <Tag>超时 {row.timeoutSec}s</Tag> : null}
          {row.shellOverride ? <Tag>shell: {row.shellOverride}</Tag> : null}
          {!row.workdirOverride && !row.timeoutSec && !row.shellOverride ? (
            <span style={{ color: '#999' }}>-</span>
          ) : null}
        </Space>
      ),
    },
    {
      title: '操作',
      valueType: 'option' as const,
      width: 110,
      render: (_: any, __: TaskStepDraft, index: number) => [
        <a
          key="run"
          onClick={() => handleRun(index + 1)}
          title="只执行这一步（任务步骤以 1 为起始序号）"
        >
          单独执行
        </a>,
      ],
    },
  ];

  const runColumns: ProColumns<Run>[] = [
    {
      title: '构建号',
      dataIndex: 'buildNumber',
      width: 110,
      render: (_, row) => <a onClick={() => history.push(`/runs/${row.id}`)}>#{row.buildNumber}</a>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (_, row) => (
        <Tag color={RUN_STATUS_META[row.status]?.color}>{RUN_STATUS_META[row.status]?.text}</Tag>
      ),
    },
    {
      title: '触发方式',
      dataIndex: 'triggerType',
      width: 100,
      render: (_, row) => TRIGGER_TYPE_META[row.triggerType] || row.triggerType,
    },
    {
      title: '触发人',
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
      title: '耗时',
      dataIndex: 'durationMs',
      width: 100,
      render: (_, row) => (row.durationMs ? formatDuration(row.durationMs) : '-'),
    },
    {
      title: '开始时间',
      dataIndex: 'startedAt',
      width: 180,
      render: (_, row) => formatTime(row.startedAt),
    },
    {
      title: '操作',
      valueType: 'option',
      width: 90,
      render: (_, row) => [<a key="log" onClick={() => history.push(`/runs/${row.id}`)}>查看日志</a>],
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
          执行任务
        </Button>,
        <Button
          key="edit"
          icon={<EditOutlined />}
          disabled={!task?.canEdit}
          onClick={() => history.push(`/tasks/${key}/edit`)}
        >
          编辑
        </Button>,
      ]}
    >
      {contextHolder}
      {task?.archived ? (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message="该任务已归档，归档状态下不能执行；需要执行请先取消归档。"
        />
      ) : null}

      <ProCard gutter={16} wrap style={{ marginBottom: 16 }}>
        <ProCard colSpan={{ xs: 24, lg: 14 }} title="基本信息" bordered>
          <ProDescriptions<Task> column={2} dataSource={task}>
            <ProDescriptions.Item label="任务 Key" copyable>
              {task?.key}
            </ProDescriptions.Item>
            <ProDescriptions.Item label="名称">{task?.name}</ProDescriptions.Item>
            <ProDescriptions.Item label="工作目录" span={2} copyable>
              {task?.workdir}
            </ProDescriptions.Item>
            <ProDescriptions.Item label="Shell">
              {SHELL_OPTIONS.find((o) => o.value === task?.shell)?.label || task?.shell || '自动'}
            </ProDescriptions.Item>
            <ProDescriptions.Item label="并发策略">
              {CONCURRENCY_OPTIONS.find((o) => o.value === task?.concurrency)?.label ||
                task?.concurrency}
            </ProDescriptions.Item>
            <ProDescriptions.Item label="默认超时">
              {task?.timeoutSec ? `${task.timeoutSec} 秒` : '不限制'}
            </ProDescriptions.Item>
            <ProDescriptions.Item label="归属">{task?.ownerName || '-'}</ProDescriptions.Item>
            <ProDescriptions.Item label="Hook 触发">
              {task?.hookEnabled ? (
                <Badge status="success" text="已启用" />
              ) : (
                <Badge status="default" text="已关闭" />
              )}
            </ProDescriptions.Item>
            <ProDescriptions.Item label="累计运行">
              {task?.runCount ?? 0} 次
            </ProDescriptions.Item>
            <ProDescriptions.Item label="上次运行">
              {task?.lastRunAt ? (
                <Space size={6}>
                  <Tag color={RUN_STATUS_META[task.lastRunStatus as never]?.color || 'default'}>
                    {RUN_STATUS_META[task.lastRunStatus as never]?.text || task.lastRunStatus}
                  </Tag>
                  <span>{formatTime(task.lastRunAt)}</span>
                </Space>
              ) : (
                '从未运行'
              )}
            </ProDescriptions.Item>
            <ProDescriptions.Item label="描述" span={2}>
              {task?.description || '-'}
            </ProDescriptions.Item>
          </ProDescriptions>
        </ProCard>

        <ProCard colSpan={{ xs: 24, lg: 10 }} title="环境变量" bordered>
          {(task?.envs || []).length === 0 ? (
            <Empty description="没有环境变量" />
          ) : (
            <Table
              rowKey="key"
              size="small"
              pagination={false}
              dataSource={task?.envs || []}
              columns={[
                { title: '变量名', dataIndex: 'key' },
                {
                  title: '值',
                  dataIndex: 'value',
                  render: (_: any, row: any) =>
                    row.isSecret ? <Text code>{SECRET_PLACEHOLDER}</Text> : row.value || '-',
                },
                {
                  title: 'Secret',
                  dataIndex: 'isSecret',
                  width: 80,
                  render: (v: boolean) => (v ? <Tag color="orange">是</Tag> : <Tag>否</Tag>),
                },
              ]}
            />
          )}
        </ProCard>
      </ProCard>

      <Card
        title="步骤列表"
        style={{ marginBottom: 16 }}
        extra={
          task?.canEdit ? (
            <Button size="small" onClick={() => history.push(`/tasks/${key}/edit`)}>
              编辑步骤
            </Button>
          ) : null
        }
      >
        {(task?.steps || []).length === 0 ? (
          <Empty description="还没有步骤" />
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
              新建 Hook Token
            </Button>
            <Button size="small" onClick={load}>
              刷新
            </Button>
          </Space>
        }
      >
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="免登录触发（任务与项目用的是同一套 Hook 机制）"
          description={
            <Space direction="vertical" size={4} style={{ width: '100%' }}>
              <Space size={6} wrap>
                <Text code>POST {baseUrl}/api/v1/hooks/&lt;HOOK_TOKEN&gt;</Text>
                <a onClick={() => copy(`curl -X POST ${baseUrl}/api/v1/hooks/<HOOK_TOKEN>`)}>
                  复制 curl
                </a>
              </Space>
              <Space size={6} wrap>
                <Text code>POST {baseUrl}/api/v1/hooks/ + X-Deploy-Token 请求头</Text>
                <Tooltip title="令牌不出现在 URL 里，也就不会落进 nginx 的 access_log">
                  <span style={{ color: '#999', fontSize: 12 }}>推荐</span>
                </Tooltip>
              </Space>
              <span style={{ color: '#999', fontSize: 12 }}>
                追加 <Text code>?wait=true</Text> 可同步等待任务跑完再返回结果。
              </span>
            </Space>
          }
        />
        {isRunning ? null : (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message="当前没有可用的 Hook Token；即便有令牌，也需要任务本身开启「Hook 触发」。"
          />
        )}
        <Table<HookToken>
          rowKey="id"
          size="small"
          pagination={false}
          dataSource={hooks}
          locale={{ emptyText: <Empty description="还没有 Hook Token" /> }}
          columns={[
            { title: '名称', dataIndex: 'name' },
            {
              title: '前缀',
              dataIndex: 'prefix',
              render: (v: string) => <Text code>{v}…</Text>,
            },
            {
              title: '状态',
              dataIndex: 'enabled',
              render: (v: boolean) =>
                v ? <Badge status="success" text="启用" /> : <Badge status="default" text="停用" />,
            },
            {
              title: '过期时间',
              dataIndex: 'expiresAt',
              render: (v: string | null) => (v ? formatTime(v) : '永不过期'),
            },
            { title: '调用次数', dataIndex: 'callCount', width: 90 },
            {
              title: '最后使用',
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
                  '未使用'
                ),
            },
            {
              title: '操作',
              valueType: 'option',
              render: (_: any, row: HookToken) => [
                <Popconfirm
                  key="reset"
                  title="重置令牌"
                  description="旧令牌立即失效，新令牌只显示一次。"
                  onConfirm={async () => {
                    const created = await taskApi.resetHook(row.id);
                    setPlainToken(created);
                    load();
                  }}
                >
                  <a>重置</a>
                </Popconfirm>,
                <Popconfirm
                  key="del"
                  title="删除令牌"
                  description="删除后无法恢复。"
                  onConfirm={async () => {
                    await taskApi.removeHook(row.id);
                    messageApi.success('已删除');
                    load();
                  }}
                >
                  <a style={{ color: '#ff4d4f' }}>删除</a>
                </Popconfirm>,
              ],
            },
          ]}
        />
      </Card>

      <Card title="运行历史">
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
        title="Hook Token 已生成（明文仅显示这一次）"
        onCancel={() => setPlainToken(undefined)}
        onOk={() => setPlainToken(undefined)}
        okText="我已保存"
        cancelButtonProps={{ style: { display: 'none' } }}
      >
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="服务端只保存哈希，关闭后无法再次查看；丢失请重置。"
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
        title="新建 Hook Token"
        onCancel={() => setHookModalOpen(false)}
        onOk={createHook}
        okText="创建"
      >
        <Form form={hookForm} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请填写名称' }]}>
            <Input placeholder="例如：ci / 定时压测" />
          </Form.Item>
          <Form.Item name="expiresInDays" label="有效期（天）" tooltip="留空表示永不过期">
            <InputNumber min={1} style={{ width: '100%' }} placeholder="留空 = 永不过期" />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
};

export default TaskDetail;
