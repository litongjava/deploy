import { systemApi } from '@/services/system';
import type { ServerDisk, ServerInfo } from '@/services/types';
import { formatBytes } from '@/utils/constants';
import {
  CloudServerOutlined,
  DatabaseOutlined,
  HddOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import { PageContainer } from '@ant-design/pro-components';
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Empty,
  Progress,
  Row,
  Space,
  Statistic,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import React, { useCallback, useEffect, useRef, useState } from 'react';

const { Text } = Typography;

/** 自动刷新间隔。后端快照每 3 秒更新一次，5 秒轮询既跟得上也不至于白跑。 */
const REFRESH_MS = 5000;
/** 使用率告警阈值，与后端 diskStatus 的判断保持一致。 */
const WARN_PERCENT = 75;
const DANGER_PERCENT = 90;

/** 把秒数说成人话。天/小时/分逐级降级，最多两级，避免出现「1天2小时3分4秒」。 */
function formatUptime(sec?: number | null): string {
  if (!sec || sec <= 0) return '-';
  const days = Math.floor(sec / 86400);
  const hours = Math.floor((sec % 86400) / 3600);
  const mins = Math.floor((sec % 3600) / 60);
  if (days > 0) return `${days} 天 ${hours} 小时`;
  if (hours > 0) return `${hours} 小时 ${mins} 分`;
  return `${mins} 分`;
}

/** 使用率 → 进度条状态。绿→橙→红，阈值与后端告警口径统一。 */
function usageStatus(percent: number): 'success' | 'normal' | 'exception' {
  if (percent >= DANGER_PERCENT) return 'exception';
  if (percent >= WARN_PERCENT) return 'normal';
  return 'success';
}

function usageStroke(percent: number): string | undefined {
  if (percent >= DANGER_PERCENT) return '#ff4d4f';
  if (percent >= WARN_PERCENT) return '#faad14';
  return '#52c41a';
}

const ServerInfoPage: React.FC = () => {
  const [info, setInfo] = useState<ServerInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [autoRefresh, setAutoRefresh] = useState(true);
  // 首次加载用 loading 遮一下；后续轮询静默更新，否则每 5 秒整页闪一次很晃眼。
  const loadedOnce = useRef(false);

  const load = useCallback(async () => {
    if (!loadedOnce.current) setLoading(true);
    try {
      const data = await systemApi.info();
      setInfo(data);
    } catch (e) {
      // 错误提示由 requestErrorConfig 统一处理；这里保留上一次的数据，
      // 一次网络抖动就把整页清空会让用户以为是服务器挂了。
    } finally {
      loadedOnce.current = true;
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!autoRefresh) return undefined;
    const timer = setInterval(load, REFRESH_MS);
    return () => clearInterval(timer);
  }, [autoRefresh, load]);

  const cpu = info?.cpu;
  const mem = info?.memory;

  const refreshButton = (
    <Space key="actions" size="middle">
      <Space size={4}>
        <Text type="secondary" style={{ fontSize: 12 }}>
          自动刷新
        </Text>
        <Switch size="small" checked={autoRefresh} onChange={setAutoRefresh} />
      </Space>
      <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
        刷新
      </Button>
    </Space>
  );

  return (
    <PageContainer
      title="服务器"
      subTitle={info ? `${info.hostname || '-'} · ${info.osName || `${info.os}/${info.arch}`}` : undefined}
      extra={refreshButton}
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {(info?.warnings || []).length > 0 ? (
          <Alert
            type="warning"
            showIcon
            message="部分信息采集失败"
            description={
              <ul style={{ margin: 0, paddingLeft: 18, listStyle: 'disc' }}>
                {(info?.warnings || []).map((w) => (
                  <li key={w}>{w}</li>
                ))}
              </ul>
            }
          />
        ) : null}

        {/* ---------- 基本信息 ---------- */}
        <Card
          title={
            <Space size={6}>
              <CloudServerOutlined />
              主机信息
            </Space>
          }
          loading={loading && !info}
          extra={
            info?.time ? (
              <Text type="secondary" style={{ fontSize: 12 }}>
                数据时间 {new Date(info.time).toLocaleTimeString()}
              </Text>
            ) : null
          }
        >
          <Descriptions size="small" column={{ xs: 1, sm: 2, md: 3 }} bordered>
            <Descriptions.Item label="主机名">{info?.hostname || '-'}</Descriptions.Item>
            <Descriptions.Item label="操作系统">
              {info?.osName || '-'}
              {info?.os ? (
                <Text type="secondary" style={{ marginLeft: 6, fontSize: 12 }}>
                  {info.os}/{info.arch}
                </Text>
              ) : null}
            </Descriptions.Item>
            <Descriptions.Item label="内核版本">{info?.kernel || '-'}</Descriptions.Item>
            <Descriptions.Item label="运行时长">
              <Tooltip title="服务器操作系统的连续运行时间（不是本服务的运行时间）">
                <span>{formatUptime(info?.uptimeSec)}</span>
              </Tooltip>
            </Descriptions.Item>
            <Descriptions.Item label="平均负载">
              {info?.load?.available ? (
                <Space size={10}>
                  <span>1 分钟 {info.load.avg1.toFixed(2)}</span>
                  <span>5 分钟 {info.load.avg5.toFixed(2)}</span>
                  <span>15 分钟 {info.load.avg15.toFixed(2)}</span>
                </Space>
              ) : (
                <Text type="secondary">当前系统不提供（Windows 无此概念）</Text>
              )}
            </Descriptions.Item>
            <Descriptions.Item label="处理器">
              {cpu?.model || '-'}
              {cpu?.mhz ? (
                <Text type="secondary" style={{ marginLeft: 6, fontSize: 12 }}>
                  {cpu.mhz} MHz
                </Text>
              ) : null}
            </Descriptions.Item>
          </Descriptions>
        </Card>

        {/* ---------- CPU / 内存 ---------- */}
        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}>
            <Card
              title={
                <Space size={6}>
                  <DatabaseOutlined />
                  CPU
                </Space>
              }
              loading={loading && !info}
            >
              <Row gutter={16} align="middle">
                <Col flex="160px" style={{ textAlign: 'center' }}>
                  {cpu?.available ? (
                    <Progress
                      type="dashboard"
                      percent={cpu.usagePercent}
                      size={140}
                      strokeColor={usageStroke(cpu.usagePercent)}
                      format={(p) => `${p}%`}
                    />
                  ) : (
                    <Progress type="dashboard" percent={0} size={140} status="normal" format={() => '采样中'} />
                  )}
                </Col>
                <Col flex="auto">
                  <Space direction="vertical" size={6} style={{ width: '100%' }}>
                    <Statistic
                      title="逻辑核心"
                      value={cpu?.cores || 0}
                      suffix={
                        cpu?.physicalCores
                          ? `（物理 ${cpu.physicalCores} 核）`
                          : undefined
                      }
                    />
                    <Text type="secondary" style={{ fontSize: 12 }}>
                      使用率按最近 {cpu?.sampleWindowSec || 0} 秒的采样窗口计算，
                      不是开机至今的平均值；服务刚启动时会短暂显示「采样中」。
                    </Text>
                    {cpu?.available ? (
                      <Text type="secondary" style={{ fontSize: 12 }}>
                        窗口内各状态累计（所有核心求和）：用户 {cpu.userSec.toFixed(1)}s ·
                        系统 {cpu.systemSec.toFixed(1)}s · 空闲 {cpu.idleSec.toFixed(1)}s
                        {cpu.iowaitSec > 0 ? ` · IO 等待 ${cpu.iowaitSec.toFixed(1)}s` : ''}
                      </Text>
                    ) : null}
                  </Space>
                </Col>
              </Row>
            </Card>
          </Col>

          <Col xs={24} lg={12}>
            <Card
              title={
                <Space size={6}>
                  <DatabaseOutlined />
                  内存
                </Space>
              }
              loading={loading && !info}
            >
              <Progress
                percent={mem?.usagePercent || 0}
                strokeColor={usageStroke(mem?.usagePercent || 0)}
                status={usageStatus(mem?.usagePercent || 0)}
              />
              <Row gutter={16} style={{ marginTop: 16 }}>
                <Col span={8}>
                  <Statistic title="总量" value={formatBytes(mem?.totalBytes)} />
                </Col>
                <Col span={8}>
                  <Statistic
                    title="已用"
                    value={formatBytes(mem?.usedBytes)}
                    valueStyle={{ color: usageStroke(mem?.usagePercent || 0) }}
                  />
                </Col>
                <Col span={8}>
                  <Statistic title="可用" value={formatBytes(mem?.availableBytes)} />
                </Col>
              </Row>
              <Descriptions size="small" column={1} style={{ marginTop: 12 }}>
                <Descriptions.Item label="空闲（不含缓存）">
                  {formatBytes(mem?.freeBytes)}
                </Descriptions.Item>
                <Descriptions.Item label={mem?.swapLabel || '交换分区'}>
                  {mem?.swapTotalBytes
                    ? `${formatBytes(mem.swapUsedBytes)} / ${formatBytes(mem.swapTotalBytes)}`
                    : '未启用'}
                </Descriptions.Item>
              </Descriptions>
            </Card>
          </Col>
        </Row>

        {/* ---------- 磁盘 ---------- */}
        <Card
          title={
            <Space size={6}>
              <HddOutlined />
              磁盘
            </Space>
          }
          loading={loading && !info}
          extra={
            info?.disks?.length ? (
              <Text type="secondary" style={{ fontSize: 12 }}>
                共 {info.disks.length} 个文件系统
              </Text>
            ) : null
          }
        >
          <Table<ServerDisk>
            rowKey={(row) => `${row.device}|${row.path}`}
            size="small"
            pagination={false}
            dataSource={info?.disks || []}
            locale={{
              emptyText: <Empty description="没有识别到磁盘（容器环境可能看不到宿主机文件系统）" />,
            }}
            columns={[
              {
                title: '挂载点 / 卷',
                dataIndex: 'path',
                width: 180,
                render: (v: string, row) => (
                  <Space direction="vertical" size={0}>
                    <Text code>{v || '-'}</Text>
                    {row.device ? (
                      <Text type="secondary" style={{ fontSize: 12 }}>
                        {row.device}
                        {row.fstype ? ` · ${row.fstype}` : ''}
                      </Text>
                    ) : null}
                  </Space>
                ),
              },
              {
                title: '使用率',
                dataIndex: 'usagePercent',
                width: 260,
                render: (v: number, row) => (
                  <Space size={8} style={{ width: '100%' }}>
                    <Progress
                      percent={v}
                      size="small"
                      style={{ width: 150, marginBottom: 0 }}
                      strokeColor={usageStroke(v)}
                      status={usageStatus(v)}
                    />
                    {row.status === 'warn' ? <Tag color="warning">空间不足</Tag> : null}
                  </Space>
                ),
              },
              {
                title: '已用 / 总量',
                dataIndex: 'usedBytes',
                width: 200,
                render: (_: number, row) => (
                  <span>
                    {formatBytes(row.usedBytes)} / {formatBytes(row.totalBytes)}
                  </span>
                ),
              },
              {
                title: '可用',
                dataIndex: 'freeBytes',
                width: 120,
                render: (v: number) => formatBytes(v),
              },
            ]}
          />
        </Card>
      </Space>
    </PageContainer>
  );
};

export default ServerInfoPage;
