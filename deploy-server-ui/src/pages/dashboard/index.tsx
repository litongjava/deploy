import { projectApi } from '@/services/project';
import { runApi } from '@/services/run';
import type { ProjectBrief, Run } from '@/services/types';
import { RUN_STATUS_META, formatDuration, formatTime } from '@/utils/constants';
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  PlayCircleOutlined,
  SyncOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { PageContainer } from '@ant-design/pro-components';
import { history } from '@umijs/max';
import { Button, Card, Col, Empty, List, Row, Space, Statistic, Table, Tag, message } from 'antd';
import React, { useCallback, useEffect, useMemo, useState } from 'react';

const REFRESH_MS = 10000;

const Dashboard: React.FC = () => {
  const [runs, setRuns] = useState<Run[]>([]);
  const [projects, setProjects] = useState<ProjectBrief[]>([]);
  const [loading, setLoading] = useState(false);
  const [messageApi, contextHolder] = message.useMessage();

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const [runPage, projectPage] = await Promise.all([
        runApi.list({ page: 1, pageSize: 100 }),
        projectApi.list({ page: 1, pageSize: 8, archived: false }),
      ]);
      setRuns(runPage?.list || []);
      setProjects(projectPage?.list || []);
    } catch (e) {
      // 错误提示由 requestErrorConfig 统一处理
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const timer = setInterval(() => load(true), REFRESH_MS);
    return () => clearInterval(timer);
  }, [load]);

  /** 全部统计都基于 /runs 列表在本地聚合（不为概览单独加接口） */
  const stats = useMemo(() => {
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const isToday = (v?: string | null) => !!v && new Date(v).getTime() >= today.getTime();

    let running = 0;
    let queued = 0;
    let todaySuccess = 0;
    let todayFailed = 0;
    runs.forEach((r) => {
      if (r.status === 'running') running += 1;
      if (r.status === 'queued') queued += 1;
      if (isToday(r.startedAt || r.finishedAt)) {
        if (r.status === 'success') todaySuccess += 1;
        if (r.status === 'failed' || r.status === 'error' || r.status === 'timeout') {
          todayFailed += 1;
        }
      }
    });
    return { running, queued, todaySuccess, todayFailed, total: runs.length };
  }, [runs]);

  const handleRun = async (key: string) => {
    try {
      const res = await projectApi.run(key);
      messageApi.success(`已触发 #${res.buildNumber}，正在跳转日志…`);
      history.push(`/runs/${res.runId}`);
    } catch (e) {
      // 已统一提示
    }
  };

  return (
    <PageContainer
      title="概览"
      extra={[
        <Button key="refresh" onClick={() => load()} loading={loading}>
          刷新
        </Button>,
        <Button
          key="new"
          type="primary"
          icon={<ThunderboltOutlined />}
          onClick={() => history.push('/projects/edit')}
        >
          新建项目
        </Button>,
      ]}
    >
      {contextHolder}
      <Row gutter={[16, 16]}>
        <Col xs={12} sm={12} md={6}>
          <Card>
            <Statistic
              title="运行中"
              value={stats.running}
              prefix={<SyncOutlined spin={stats.running > 0} />}
              valueStyle={{ color: stats.running > 0 ? '#1890ff' : undefined }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={12} md={6}>
          <Card>
            <Statistic title="排队中" value={stats.queued} prefix={<PlayCircleOutlined />} />
          </Card>
        </Col>
        <Col xs={12} sm={12} md={6}>
          <Card>
            <Statistic
              title="今日成功"
              value={stats.todaySuccess}
              prefix={<CheckCircleOutlined />}
              valueStyle={{ color: '#52c41a' }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={12} md={6}>
          <Card>
            <Statistic
              title="今日失败"
              value={stats.todayFailed}
              prefix={<CloseCircleOutlined />}
              valueStyle={{ color: stats.todayFailed > 0 ? '#ff4d4f' : undefined }}
            />
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} lg={16}>
          <Card
            title="最近运行"
            extra={
              <Button type="link" onClick={() => history.push('/runs')}>
                查看全部
              </Button>
            }
          >
            <Table<Run>
              rowKey="id"
              size="small"
              loading={loading}
              dataSource={runs.slice(0, 10)}
              pagination={false}
              locale={{ emptyText: <Empty description="还没有运行记录" /> }}
              columns={[
                {
                  title: '构建号',
                  dataIndex: 'buildNumber',
                  width: 90,
                  render: (v: number, row) => (
                    <a onClick={() => history.push(`/runs/${row.id}`)}>#{v}</a>
                  ),
                },
                {
                  title: '项目',
                  dataIndex: 'projectName',
                  render: (v: string, row) => (
                    <a onClick={() => history.push(`/projects/${row.projectKey}`)}>
                      {v || row.projectKey}
                    </a>
                  ),
                },
                {
                  title: '状态',
                  dataIndex: 'status',
                  width: 100,
                  render: (v: Run['status']) => (
                    <Tag color={RUN_STATUS_META[v]?.color}>{RUN_STATUS_META[v]?.text || v}</Tag>
                  ),
                },
                {
                  title: '触发',
                  dataIndex: 'triggerUserName',
                  width: 110,
                  render: (v: string, row) => v || row.triggerType,
                },
                {
                  title: '耗时',
                  dataIndex: 'durationMs',
                  width: 100,
                  render: (v: number) => formatDuration(v),
                },
                {
                  title: '开始时间',
                  dataIndex: 'startedAt',
                  width: 170,
                  render: (v: string) => formatTime(v),
                },
              ]}
            />
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card title="快捷执行" extra={<a onClick={() => history.push('/projects')}>项目列表</a>}>
            <List
              dataSource={projects}
              loading={loading}
              locale={{ emptyText: <Empty description="还没有项目" /> }}
              renderItem={(item) => (
                <List.Item
                  actions={[
                    <Button
                      key="run"
                      type="link"
                      size="small"
                      disabled={item.archived}
                      onClick={() => handleRun(item.key)}
                    >
                      执行
                    </Button>,
                    <a key="detail" onClick={() => history.push(`/projects/${item.key}`)}>
                      详情
                    </a>,
                  ]}
                >
                  <List.Item.Meta
                    title={
                      <Space size={6}>
                        <span>{item.name}</span>
                        <Tag>{item.key}</Tag>
                        {item.archived ? <Tag color="default">已归档</Tag> : null}
                      </Space>
                    }
                    description={
                      <Space size={12} wrap>
                        <span>共 {item.runCount} 次</span>
                        <span>
                          上次：
                          {item.lastRunStatus
                            ? RUN_STATUS_META[item.lastRunStatus as Run['status']]?.text
                            : '-'}
                        </span>
                        <span>{formatTime(item.lastRunAt)}</span>
                      </Space>
                    }
                  />
                </List.Item>
              )}
            />
          </Card>
        </Col>
      </Row>
    </PageContainer>
  );
};

export default Dashboard;
