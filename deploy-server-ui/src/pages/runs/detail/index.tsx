import LogTerminal, { LogTerminalHandle } from '@/components/LogTerminal';
import { downloadRunLog, runApi } from '@/services/run';
import type { Run, RunStep } from '@/services/types';
import {
  PREPARE_STATUS_META,
  RUN_STATUS_META,
  TRIGGER_TYPE_META,
  formatBytes,
  formatDuration,
  formatTime,
  isRunFinished,
} from '@/utils/constants';
import {
  DownloadOutlined,
  ReloadOutlined,
  StopOutlined,
} from '@ant-design/icons';
import { PageContainer, ProDescriptions } from '@ant-design/pro-components';
import { history, useParams } from '@umijs/max';
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Popconfirm,
  Space,
  Table,
  Tag,
  message,
} from 'antd';
import React, { useCallback, useEffect, useRef, useState } from 'react';

const POLL_MS = 5000;
const CHUNK_LIMIT = 262144;

const RunDetail: React.FC = () => {
  const { id = '' } = useParams<{ id: string }>();
  const [run, setRun] = useState<Run | null>(null);
  const [loading, setLoading] = useState(true);
  const [logLoading, setLogLoading] = useState(false);
  const [canceling, setCanceling] = useState(false);
  const [logError, setLogError] = useState('');
  const [messageApi, contextHolder] = message.useMessage();
  const termRef = useRef<LogTerminalHandle>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const fetchedRef = useRef<boolean>(false);

  const loadRun = useCallback(
    async (silent = false) => {
      if (!id) return;
      if (!silent) setLoading(true);
      try {
        const data = await runApi.detail(id);
        setRun(data);
      } catch (e) {
        // 统一提示
      } finally {
        setLoading(false);
      }
    },
    [id],
  );

  /** 拉取历史日志并写入终端（断线重连前先把已有的补齐） */
  const loadLog = useCallback(async () => {
    if (!id || fetchedRef.current) return;
    fetchedRef.current = true;
    setLogLoading(true);
    setLogError('');
    try {
      let offset = 0;
      let guard = 0;
      // 分块拉取，直到 eof（单条日志可能很大，避免一次性请求）
      for (;;) {
        const chunk = await runApi.log(id, offset, CHUNK_LIMIT);
        const start = chunk?.offset ?? offset;
        if (chunk?.data) {
          // 走 writeAt：WebSocket 的 history 帧可能已经写过同一段，按 offset 去重
          termRef.current?.writeAt(start, chunk.data);
        }
        const next = chunk?.nextOffset ?? offset;
        if (chunk?.eof || next <= offset || guard > 200) break;
        offset = next;
        guard += 1;
      }
    } catch (e: any) {
      setLogError(e?.info?.errorMessage || e?.message || '历史日志加载失败');
    } finally {
      setLogLoading(false);
    }
  }, [id]);

  useEffect(() => {
    loadRun();
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [loadRun]);

  // 终端挂载后先补历史日志。
  // 注意：React 18 StrictMode 下 effect 会「挂载→卸载→再挂载」，
  // 所以卸载时要把已拉取标记复位，否则第二次挂载的终端拿不到历史日志。
  useEffect(() => {
    const timer = setTimeout(() => {
      loadLog();
    }, 0);
    return () => {
      clearTimeout(timer);
      fetchedRef.current = false;
    };
  }, [loadLog]);

  // 运行中轮询刷新详情（步骤状态等），结束后停止
  useEffect(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
    if (run && !isRunFinished(run.status)) {
      pollRef.current = setInterval(() => {
        loadRun(true);
      }, POLL_MS);
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [run, loadRun]);

  const handleCancel = async () => {
    setCanceling(true);
    try {
      await runApi.cancel(id);
      messageApi.success('已发送取消请求');
      loadRun(true);
    } catch (e) {
      // 统一提示
    } finally {
      setCanceling(false);
    }
  };

  const handleRerun = async () => {
    try {
      const res = await runApi.rerun(id);
      messageApi.success(`已重新触发构建 #${res.buildNumber}`);
      history.push(`/runs/${res.runId}`);
    } catch (e) {
      // 统一提示
    }
  };

  const handleDownload = async () => {
    try {
      const blob = await downloadRunLog(id);
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `run-${run?.projectKey || 'run'}-${run?.buildNumber ?? id}.log`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      window.URL.revokeObjectURL(url);
    } catch (e: any) {
      messageApi.error(e?.message || '下载失败');
    }
  };

  const active = !!run && !isRunFinished(run.status);

  const stepColumns = [
    { title: '阶段', dataIndex: 'phase', width: 90, render: (v: string) => (v === 'prepare' ? '准备' : '命令') },
    { title: '顺序', dataIndex: 'sortOrder', width: 70 },
    { title: '名称', dataIndex: 'name' },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (v: string) =>
        v ? (
          <Tag color={RUN_STATUS_META[v as Run['status']]?.color}>
            {RUN_STATUS_META[v as Run['status']]?.text || v}
          </Tag>
        ) : (
          '-'
        ),
    },
    {
      title: '耗时',
      dataIndex: 'durationMs',
      width: 100,
      render: (v: number) => formatDuration(v),
    },
    {
      title: '退出码',
      dataIndex: 'exitCode',
      width: 90,
      render: (v: number | null) =>
        v === null || v === undefined ? '-' : <Tag color={v === 0 ? 'green' : 'red'}>{v}</Tag>,
    },
    {
      title: '开始时间',
      dataIndex: 'startedAt',
      width: 170,
      render: (v: string) => formatTime(v),
    },
  ];

  return (
    <PageContainer
      loading={loading && !run}
      title={run ? `运行 #${run.buildNumber}` : `运行 ${id}`}
      subTitle={
        run ? (
          <Space>
            <a onClick={() => history.push(`/projects/${run.projectKey}`)}>
              {run.projectName || run.projectKey}
            </a>
            <Tag color={RUN_STATUS_META[run.status]?.color}>
              {RUN_STATUS_META[run.status]?.text || run.status}
            </Tag>
          </Space>
        ) : undefined
      }
      onBack={() => history.push('/runs')}
      extra={[
        <Button key="refresh" icon={<ReloadOutlined />} onClick={() => loadRun()}>
          刷新
        </Button>,
        <Button key="download" icon={<DownloadOutlined />} onClick={handleDownload}>
          下载日志
        </Button>,
        active ? (
          <Popconfirm
            key="cancel"
            title="取消运行"
            description="会终止整个进程树，确认取消吗？"
            onConfirm={handleCancel}
            okText="取消运行"
            okButtonProps={{ danger: true }}
            cancelText="再想想"
          >
            <Button danger icon={<StopOutlined />} loading={canceling}>
              取消运行
            </Button>
          </Popconfirm>
        ) : (
          <Button key="rerun" onClick={handleRerun}>
            重新执行
          </Button>
        ),
      ]}
    >
      {contextHolder}

      {run?.errorMsg ? (
        <Alert
          style={{ marginBottom: 16 }}
          type="error"
          showIcon
          message="运行错误"
          description={run.errorMsg}
        />
      ) : null}

      {run?.prepareStatus === 'failed' ? (
        <Alert
          style={{ marginBottom: 16 }}
          type="warning"
          showIcon
          message="Git 准备阶段失败"
          description="准备阶段失败时不会执行任何用户命令，请检查仓库地址 / 分支 / 凭据。"
        />
      ) : null}

      <Card style={{ marginBottom: 16 }}>
        <ProDescriptions<Run>
          column={{ xs: 1, sm: 2, md: 3 }}
          dataSource={run || undefined}
          columns={[
            { title: '运行 ID', dataIndex: 'id' },
            { title: '构建号', dataIndex: 'buildNumber' },
            {
              title: '触发方式',
              dataIndex: 'triggerType',
              render: (_, row) => TRIGGER_TYPE_META[row.triggerType] || row.triggerType,
            },
            { title: '触发人', dataIndex: 'triggerUserName', render: (v) => v || '-' },
            { title: '触发 IP', dataIndex: 'triggerIp', render: (v) => v || '-' },
            { title: '请求分支', dataIndex: 'requestedRef', render: (v) => v || '-' },
            {
              title: 'Git',
              dataIndex: 'gitCommitShort',
              render: (_, row) =>
                row.gitCommitShort ? (
                  <Space direction="vertical" size={0}>
                    <span>
                      {row.gitCommitShort} @ {row.gitBranchResolved || '-'}
                    </span>
                    <span style={{ fontSize: 12, color: '#999', wordBreak: 'break-all' }}>
                      {row.gitCommit}
                    </span>
                  </Space>
                ) : (
                  '-'
                ),
            },
            {
              title: '准备阶段',
              dataIndex: 'prepareStatus',
              render: (_, row) => (
                <Space size={6}>
                  <Tag color={PREPARE_STATUS_META[row.prepareStatus]?.color}>
                    {PREPARE_STATUS_META[row.prepareStatus]?.text || row.prepareStatus}
                  </Tag>
                  {row.prepareDurationMs ? (
                    <span style={{ fontSize: 12, color: '#999' }}>
                      {formatDuration(row.prepareDurationMs)}
                    </span>
                  ) : null}
                </Space>
              ),
            },
            { title: '开始时间', dataIndex: 'startedAt', render: (v) => formatTime(v) },
            { title: '结束时间', dataIndex: 'finishedAt', render: (v) => formatTime(v) },
            {
              title: '耗时',
              dataIndex: 'durationMs',
              render: (v) => formatDuration(v),
            },
            {
              title: '退出码',
              dataIndex: 'exitCode',
              render: (v) => (v === null || v === undefined ? '-' : v),
            },
            {
              title: '日志大小',
              dataIndex: 'logSize',
              render: (v) => formatBytes(v),
            },
          ]}
        />
        {run?.params && Object.keys(run.params).length > 0 ? (
          <Descriptions
            size="small"
            column={1}
            style={{ marginTop: 12 }}
            items={[
              {
                key: 'params',
                label: '触发参数',
                children: (
                  <Space wrap>
                    {Object.entries(run.params).map(([k, v]) => (
                      <Tag key={k}>
                        {k}={v}
                      </Tag>
                    ))}
                  </Space>
                ),
              },
            ]}
          />
        ) : null}
      </Card>

      <Card title="实时日志" style={{ marginBottom: 16 }}>
        {logError ? (
          <Alert
            style={{ marginBottom: 12 }}
            type="warning"
            showIcon
            message={logError}
            description={
              <span>
                历史日志接口不可用，但实时流仍会尝试连接；也可以点右上角
                <Button type="link" size="small" onClick={handleDownload}>
                  下载日志
                </Button>
                。
              </span>
            }
          />
        ) : null}
        <LogTerminal
          ref={termRef}
          runId={id}
          active={active}
          onExit={(status) => {
            messageApi.info(`运行结束：${RUN_STATUS_META[status as Run['status']]?.text || status}`);
            loadRun(true);
          }}
          onError={(msg) => {
            messageApi.error(`日志流错误：${msg}`);
          }}
          onDownload={handleDownload}
          height={520}
        />
        <div style={{ marginTop: 8, color: '#999', fontSize: 12 }}>
          连接方式：WebSocket /api/v1/ws/runs/{id}?token=***&offset=N；断线会自动按 nextOffset
          指数退避重连补齐日志。
          {logLoading ? ' 正在补齐历史日志…' : ''}
        </div>
      </Card>

      <Card title="步骤">
        {(run?.steps || []).length === 0 ? (
          <Empty description="还没有步骤记录" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        ) : (
          <Table<RunStep>
            rowKey="id"
            size="small"
            pagination={false}
            dataSource={run?.steps || []}
            columns={stepColumns as any}
          />
        )}
      </Card>
    </PageContainer>
  );
};

export default RunDetail;
