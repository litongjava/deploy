import LogTerminal, { LogTerminalHandle } from '@/components/LogTerminal';
import { t } from '@/i18n';
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
import { DownloadOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons';
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
      setLogError(e?.info?.errorMessage || e?.message || t('run.failedToLoadHistoryLogs'));
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

  // 换了一条运行（「重新执行」跳到新运行）就把「历史已拉」的闸门复位：
  // 闸门是「这条运行的日志拉过没有」，不是「这个页面拉过没有」。
  useEffect(() => {
    fetchedRef.current = false;
  }, [id]);

  // 终端挂载之后再补历史日志 —— 必须等 run 到位，不能一挂载就拉。
  //
  // 终端是 PageContainer 的 children，而 PageContainer 在 loading 时渲染的是
  // 骨架屏、**根本不渲染 children**；所以详情没回来的那段时间里终端还没挂载，
  // 这时候拉日志会写进「不存在的终端」（termRef 为空，写入被丢掉），
  // 偏偏 fetchedRef 已经落闸 —— 打开一条**已结束**的运行就会看到永远空白的终端
  // （实时流那边也帮不上：结束的运行本来就不建连）。
  // 等 run 到位再拉，顺序就对了：子组件的 effect 先跑，终端实例已经建好。
  useEffect(() => {
    if (!run) return;
    const timer = setTimeout(() => {
      loadLog();
    }, 0);
    return () => clearTimeout(timer);
  }, [run, loadLog]);

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
      messageApi.success(t('run.cancelRequestSent'));
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
      messageApi.success(t('run.buildReTriggered', { buildNumber: res.buildNumber }));
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
      messageApi.error(e?.message || t('common.downloadFailed'));
    }
  };

  const active = !!run && !isRunFinished(run.status);

  const stepColumns = [
    {
      title: t('run.stage'),
      dataIndex: 'phase',
      width: 90,
      render: (v: string) => (v === 'prepare' ? t('run.prepare') : t('common.command')),
    },
    { title: t('common.order'), dataIndex: 'sortOrder', width: 70 },
    { title: t('common.name'), dataIndex: 'name' },
    {
      title: t('common.status'),
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
      title: t('common.duration'),
      dataIndex: 'durationMs',
      width: 100,
      render: (v: number) => formatDuration(v),
    },
    {
      title: t('common.exitCode'),
      dataIndex: 'exitCode',
      width: 90,
      render: (v: number | null) =>
        v === null || v === undefined ? '-' : <Tag color={v === 0 ? 'green' : 'red'}>{v}</Tag>,
    },
    {
      title: t('common.startTime'),
      dataIndex: 'startedAt',
      width: 170,
      render: (v: string) => formatTime(v),
    },
  ];

  return (
    <PageContainer
      loading={loading && !run}
      title={run ? t('run.header', { buildNumber: run.buildNumber }) : t('run.runId', { id: id })}
      subTitle={
        run ? (
          <Space>
            {/* 执行对象可能是项目也可能是任务，按 targetKind 决定链接与文案 */}
            <Tag color={run.targetKind === 'task' ? 'geekblue' : 'blue'}>
              {run.targetKind === 'task' ? t('common.task') : t('common.project')}
            </Tag>
            <a
              onClick={() =>
                history.push(
                  run.targetKind === 'task'
                    ? `/tasks/${run.targetKey || run.projectKey}`
                    : `/projects/${run.targetKey || run.projectKey}`,
                )
              }
            >
              {run.targetName || run.projectName || run.targetKey || run.projectKey}
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
          {t('common.refresh')}{' '}
        </Button>,
        <Button key="download" icon={<DownloadOutlined />} onClick={handleDownload}>
          {t('common.downloadLogs')}{' '}
        </Button>,
        active ? (
          <Popconfirm
            key="cancel"
            title={t('run.cancelRun')}
            description={t('run.terminatesTheWholeProcessTree')}
            onConfirm={handleCancel}
            okText={t('run.cancelRun')}
            okButtonProps={{ danger: true }}
            cancelText={t('run.neverMind')}
          >
            <Button danger icon={<StopOutlined />} loading={canceling}>
              {t('run.cancelRun')}{' '}
            </Button>
          </Popconfirm>
        ) : (
          <Button key="rerun" onClick={handleRerun}>
            {t('run.runAgain')}{' '}
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
          message={t('run.runError')}
          description={run.errorMsg}
        />
      ) : null}

      {(run?.pipeline || []).length > 0 ? (
        <Card
          title={t('run.dependencyBuildPipeline')}
          style={{ marginBottom: 16 }}
          extra={<span style={{ color: '#999', fontSize: 12 }}>pipelineId: {run?.pipelineId}</span>}
        >
          <Table
            rowKey="runId"
            size="small"
            pagination={false}
            dataSource={run?.pipeline || []}
            columns={[
              {
                title: t('common.order'),
                dataIndex: 'pipelineOrder',
                width: 70,
                render: (v: number, row) => (row.pipelineRole === 'main' ? t('run.target') : v),
              },
              {
                title: t('common.role'),
                dataIndex: 'pipelineRole',
                width: 100,
                render: (v: string) =>
                  v === 'main' ? (
                    <Tag color="blue">{t('run.targetProject')}</Tag>
                  ) : (
                    <Tag color="purple">{t('run.dependencyBuild')}</Tag>
                  ),
              },
              {
                title: t('common.project'),
                dataIndex: 'projectName',
                render: (_, row) => (
                  <Space size={6}>
                    <a onClick={() => history.push(`/projects/${row.projectKey}`)}>
                      {row.projectName || row.projectKey}
                    </a>
                    <span style={{ color: '#999', fontSize: 12 }}>{row.projectKey}</span>
                  </Space>
                ),
              },
              {
                title: t('run.build'),
                dataIndex: 'buildNumber',
                width: 90,
                render: (v: number, row) => (
                  <a onClick={() => history.push(`/runs/${row.runId}`)}>#{v}</a>
                ),
              },
              {
                title: t('common.status'),
                dataIndex: 'status',
                width: 100,
                render: (v: Run['status']) => (
                  <Tag color={RUN_STATUS_META[v]?.color}>{RUN_STATUS_META[v]?.text || v}</Tag>
                ),
              },
              {
                title: t('common.duration'),
                dataIndex: 'durationMs',
                width: 100,
                render: (v: number) => formatDuration(v),
              },
              {
                title: t('common.actions'),
                width: 100,
                render: (_, row) =>
                  row.runId === Number(id) ? (
                    <span style={{ color: '#999' }}>{t('run.current')}</span>
                  ) : (
                    <a onClick={() => history.push(`/runs/${row.runId}`)}>{t('common.viewLogs')}</a>
                  ),
              },
            ]}
          />
        </Card>
      ) : null}

      {run?.prepareStatus === 'failed' ? (
        <Alert
          style={{ marginBottom: 16 }}
          type="warning"
          showIcon
          message={t('run.gitPreparationFailed')}
          description={t('run.whenThePreparationStageFails')}
        />
      ) : null}

      <Card style={{ marginBottom: 16 }}>
        <ProDescriptions<Run>
          column={{ xs: 1, sm: 2, md: 3 }}
          dataSource={run || undefined}
          columns={[
            { title: t('run.runID'), dataIndex: 'id' },
            { title: t('common.buildNumber'), dataIndex: 'buildNumber' },
            {
              title: t('common.triggerMode'),
              dataIndex: 'triggerType',
              render: (_, row) => TRIGGER_TYPE_META[row.triggerType] || row.triggerType,
            },
            {
              title: t('common.triggeredBy'),
              dataIndex: 'triggerUserName',
              render: (v) => v || '-',
            },
            { title: t('run.sourceIP'), dataIndex: 'triggerIp', render: (v) => v || '-' },
            { title: t('run.requestedBranch'), dataIndex: 'requestedRef', render: (v) => v || '-' },
            {
              title: 'Git',
              dataIndex: 'gitCommitShort',
              // 详情页这一格要说清「这次部署的是哪一版」：短 sha @ 分支、
              // 完整 sha、提交说明，以及作者与提交时间。没有 Git 提交的 Run
              // （任务、或 prepare 失败的 Run）整格显示 '-'。
              span: 3,
              render: (_, row) =>
                row.gitCommitShort ? (
                  <Space direction="vertical" size={2}>
                    <span>
                      <strong>{row.gitCommitShort}</strong> @ {row.gitBranchResolved || '-'}
                    </span>
                    <span style={{ fontSize: 12, color: '#999', wordBreak: 'break-all' }}>
                      {row.gitCommit}
                    </span>
                    {row.gitMessage ? <span>{row.gitMessage}</span> : null}
                    {row.gitAuthor || row.gitCommitAt ? (
                      <span style={{ fontSize: 12, color: '#999' }}>
                        {[row.gitAuthor, formatTime(row.gitCommitAt)].filter(Boolean).join(' · ')}
                      </span>
                    ) : null}
                  </Space>
                ) : (
                  '-'
                ),
            },
            {
              title: t('run.preparationStage'),
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
            {
              title: t('common.startTime'),
              dataIndex: 'startedAt',
              render: (_, row) => formatTime(row.startedAt),
            },
            {
              title: t('run.endTime'),
              dataIndex: 'finishedAt',
              render: (_, row) => formatTime(row.finishedAt),
            },
            {
              title: t('common.duration'),
              dataIndex: 'durationMs',
              render: (_, row) => formatDuration(row.durationMs),
            },
            {
              title: t('common.exitCode'),
              dataIndex: 'exitCode',
              render: (v) => (v === null || v === undefined ? '-' : v),
            },
            {
              title: t('run.logSize'),
              dataIndex: 'logSize',
              render: (_, row) => formatBytes(row.logSize),
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
                label: t('run.triggerParameters'),
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

      <Card
        title={t('run.liveLogs')}
        style={{ marginBottom: 16 }}
        extra={
          <Space>
            <Button
              size="small"
              icon={<ReloadOutlined />}
              onClick={() => {
                // fetchedRef 是「HTTP 历史只拉一次」的闸门，手动重载要把它放开
                fetchedRef.current = false;
                termRef.current?.reload();
                loadLog();
                loadRun(true);
              }}
            >
              {t('common.reloadLogs')}{' '}
            </Button>
            <Button size="small" onClick={() => termRef.current?.reconnect()}>
              {t('run.reconnectTheLogStream')}{' '}
            </Button>
          </Space>
        }
      >
        {logError ? (
          <Alert
            style={{ marginBottom: 12 }}
            type="warning"
            showIcon
            message={logError}
            description={
              <span>
                {t('run.historyLogEndpointIsUnavailable')}
                <Button type="link" size="small" onClick={handleDownload}>
                  {t('common.downloadLogs')}
                </Button>
                {t('common.period')}
              </span>
            }
          />
        ) : null}
        <LogTerminal
          // key 必须带上 runId：从「重新执行」跳到新运行时，
          // React 会复用同一个 RunDetail（路由参数变、组件不重挂），
          // 而 LogTerminal 内部记着旧运行的「已落屏字节数」与 WebSocket，
          // 新日志会被按 offset 去重整段丢掉 —— 表现就是终端一片空白。
          key={id}
          ref={termRef}
          runId={id}
          active={active}
          onExit={(status) => {
            messageApi.info(
              t('run.runFinished', {
                p0: RUN_STATUS_META[status as Run['status']]?.text || status,
              }),
            );
            loadRun(true);
          }}
          onError={(msg) => {
            messageApi.error(t('run.logStreamError', { msg: msg }));
          }}
          onDownload={handleDownload}
          height={520}
        />
        <div style={{ marginTop: 8, color: '#999', fontSize: 12 }}>
          {t('run.transportHint', {
            id,
          })}
          {logLoading ? t('run.backfillingHistoryLogs') : ''}
        </div>
      </Card>

      <Card title={t('common.step')}>
        {(run?.steps || []).length === 0 ? (
          <Empty description={t('run.noStepRecordsYet')} image={Empty.PRESENTED_IMAGE_SIMPLE} />
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
