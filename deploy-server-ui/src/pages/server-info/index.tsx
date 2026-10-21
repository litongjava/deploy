import { t } from '@/i18n';
import { systemApi } from '@/services/system';
import type { ServerCoreUsage, ServerDisk, ServerInfo } from '@/services/types';
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
  if (days > 0) return t('server.durationDaysHours', { days: days, hours: hours });
  if (hours > 0) return t('server.durationHoursMins', { hours: hours, mins: mins });
  return t('server.durationMins', { mins: mins });
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

/**
 * 每个逻辑核心一个迷你进度条，排在整机仪表盘右边。
 *
 * 为什么需要「最多显示 N 个 + 其余折叠」：这台机器的核数可以是 4，也可以是 128，
 * 全画出来会把卡片撑成一列长条、把内存卡片挤得没法看。所以只显示最忙的若干个
 * （最忙的正是用户想看的），剩下的用一个提示说明。
 */
const MAX_CORE_CELLS = 12;

const CoreUsageGrid: React.FC<{ cores?: ServerCoreUsage[]; available: boolean }> = ({
  cores,
  available,
}) => {
  const list = cores || [];
  if (list.length === 0) {
    return (
      <Text type="secondary" style={{ fontSize: 12 }}>
        {available ? t('server.perCoreUsageIsNot') : t('server.samplingNow')}
      </Text>
    );
  }

  // 按使用率降序取最忙的几个；序号相同的稳定顺序，避免每 5 秒重排导致画面抖动。
  const sorted = [...list].sort((a, b) => b.usagePercent - a.usagePercent || a.index - b.index);
  const shown = sorted.slice(0, MAX_CORE_CELLS);
  const hidden = list.length - shown.length;
  const busiest = sorted[0];

  return (
    <Space direction="vertical" size={6} style={{ width: '100%' }}>
      <Space size={6} wrap>
        <Text strong style={{ fontSize: 12 }}>
          {t('server.perCoreUsage')}{' '}
        </Text>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {t('server.sortedByCurrentUsage')}
          {hidden > 0
            ? t('server.showingOnlyTheBusiestOf', { length: shown.length, p1: list.length })
            : ''}
        </Text>
      </Space>
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(78px, 1fr))',
          gap: '6px 10px',
        }}
      >
        {shown.map((c) => (
          <Tooltip
            key={c.index}
            title={t('server.windowBusy', {
              indexLabel: c.indexLabel,
              p1: c.busySec.toFixed(2),
              p2: c.totalSec.toFixed(2),
            })}
          >
            <div style={{ minWidth: 0 }}>
              <div
                style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  fontSize: 11,
                  lineHeight: '14px',
                  color: 'rgba(0,0,0,0.45)',
                }}
              >
                <span>{c.indexLabel}</span>
                <span>{c.usagePercent}%</span>
              </div>
              <Progress
                percent={c.usagePercent}
                size="small"
                showInfo={false}
                strokeColor={usageStroke(c.usagePercent)}
                style={{ marginBottom: 0 }}
              />
            </div>
          </Tooltip>
        ))}
      </div>
      {busiest && busiest.usagePercent >= DANGER_PERCENT ? (
        <Text type="danger" style={{ fontSize: 12 }}>
          {t('server.nearlySaturated', {
            label: busiest.indexLabel,
            percent: busiest.usagePercent,
          })}
        </Text>
      ) : null}
    </Space>
  );
};

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
          {t('server.autoRefresh')}{' '}
        </Text>
        <Switch size="small" checked={autoRefresh} onChange={setAutoRefresh} />
      </Space>
      <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
        {t('common.refresh')}{' '}
      </Button>
    </Space>
  );

  return (
    <PageContainer
      title={t('server.server')}
      subTitle={
        info ? `${info.hostname || '-'} · ${info.osName || `${info.os}/${info.arch}`}` : undefined
      }
      extra={refreshButton}
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {(info?.warnings || []).length > 0 ? (
          <Alert
            type="warning"
            showIcon
            message={t('server.someInformationCouldNotBe')}
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
              {t('server.hostInformation')}
            </Space>
          }
          loading={loading && !info}
          extra={
            info?.time ? (
              <Text type="secondary" style={{ fontSize: 12 }}>
                {t('server.dataTime', { time: new Date(info.time).toLocaleTimeString() })}
              </Text>
            ) : null
          }
        >
          <Descriptions size="small" column={{ xs: 1, sm: 2, md: 3 }} bordered>
            <Descriptions.Item label={t('common.hostname')}>
              {info?.hostname || '-'}
            </Descriptions.Item>
            <Descriptions.Item label={t('common.operatingSystem')}>
              {info?.osName || '-'}
              {info?.os ? (
                <Text type="secondary" style={{ marginLeft: 6, fontSize: 12 }}>
                  {info.os}/{info.arch}
                </Text>
              ) : null}
            </Descriptions.Item>
            <Descriptions.Item label={t('server.kernelVersion')}>
              {info?.kernel || '-'}
            </Descriptions.Item>
            <Descriptions.Item label={t('server.duration')}>
              <Tooltip title={t('server.continuousUptimeOfTheServer')}>
                <span>{formatUptime(info?.uptimeSec)}</span>
              </Tooltip>
            </Descriptions.Item>
            <Descriptions.Item label={t('server.loadAverage')}>
              {info?.load?.available ? (
                <Space size={10}>
                  <span>
                    {t('server.loadAvg1m')} {info.load.avg1.toFixed(2)}
                  </span>
                  <span>
                    {t('server.loadAvg5m')} {info.load.avg5.toFixed(2)}
                  </span>
                  <span>
                    {t('server.loadAvg15m')} {info.load.avg15.toFixed(2)}
                  </span>
                </Space>
              ) : (
                <Text type="secondary">{t('server.notProvidedByThisSystem')}</Text>
              )}
            </Descriptions.Item>
            <Descriptions.Item label={t('server.cpu')}>
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
              <Row gutter={[16, 8]} align="middle" wrap={false}>
                <Col flex="132px" style={{ textAlign: 'center' }}>
                  {cpu?.available ? (
                    <Progress
                      type="dashboard"
                      percent={cpu.usagePercent}
                      size={124}
                      strokeColor={usageStroke(cpu.usagePercent)}
                      format={(p) => `${p}%`}
                    />
                  ) : (
                    <Progress
                      type="dashboard"
                      percent={0}
                      size={124}
                      status="normal"
                      format={() => t('server.sampling')}
                    />
                  )}
                  <div>
                    <Text type="secondary" style={{ fontSize: 12 }}>
                      {t('server.overallUsage')}{' '}
                    </Text>
                  </div>
                </Col>
                <Col flex="auto" style={{ minWidth: 0 }}>
                  <CoreUsageGrid cores={cpu?.coreUsage} available={!!cpu?.available} />
                </Col>
              </Row>
              <Row gutter={16} style={{ marginTop: 12 }} align="bottom">
                <Col flex="none">
                  <Statistic
                    title={t('server.logicalCores')}
                    value={cpu?.cores || 0}
                    suffix={
                      cpu?.physicalCores
                        ? t('server.physicalCores', { physicalCores: cpu.physicalCores })
                        : undefined
                    }
                  />
                </Col>
                <Col flex="auto" style={{ minWidth: 0 }}>
                  <Space direction="vertical" size={4} style={{ width: '100%' }}>
                    <Text type="secondary" style={{ fontSize: 12 }}>
                      {t('server.usageWindowHint', {
                        sec: cpu?.sampleWindowSec || 0,
                      })}
                    </Text>
                    {cpu?.available ? (
                      <Text type="secondary" style={{ fontSize: 12 }}>
                        {t('server.cpuTimeTotals', {
                          user: cpu.userSec.toFixed(1),
                          system: cpu.systemSec.toFixed(1),
                          idle: cpu.idleSec.toFixed(1),
                        })}
                        {cpu.iowaitSec > 0
                          ? t('server.ioWait', { p0: cpu.iowaitSec.toFixed(1) })
                          : ''}
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
                  {t('server.memory')}
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
                  <Statistic title={t('server.total')} value={formatBytes(mem?.totalBytes)} />
                </Col>
                <Col span={8}>
                  <Statistic
                    title={t('server.used')}
                    value={formatBytes(mem?.usedBytes)}
                    valueStyle={{ color: usageStroke(mem?.usagePercent || 0) }}
                  />
                </Col>
                <Col span={8}>
                  <Statistic
                    title={t('common.available')}
                    value={formatBytes(mem?.availableBytes)}
                  />
                </Col>
              </Row>
              <Descriptions size="small" column={1} style={{ marginTop: 12 }}>
                <Descriptions.Item label={t('server.freeExcludingCache')}>
                  {formatBytes(mem?.freeBytes)}
                </Descriptions.Item>
                <Descriptions.Item label={mem?.swapLabel || t('server.swap')}>
                  {mem?.swapTotalBytes
                    ? `${formatBytes(mem.swapUsedBytes)} / ${formatBytes(mem.swapTotalBytes)}`
                    : t('common.disabled')}
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
              {t('server.disk')}
            </Space>
          }
          loading={loading && !info}
          extra={
            info?.disks?.length ? (
              <Text type="secondary" style={{ fontSize: 12 }}>
                {t('server.filesystems', { count: info.disks.length })}
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
              emptyText: <Empty description={t('server.noDisksDetectedContainersMay')} />,
            }}
            columns={[
              {
                title: t('server.mountPointVolume'),
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
                title: t('server.usage'),
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
                    {row.status === 'warn' ? (
                      <Tag color="warning">{t('server.notEnoughSpace')}</Tag>
                    ) : null}
                  </Space>
                ),
              },
              {
                title: t('server.usedTotal'),
                dataIndex: 'usedBytes',
                width: 200,
                render: (_: number, row) => (
                  <span>
                    {formatBytes(row.usedBytes)} / {formatBytes(row.totalBytes)}
                  </span>
                ),
              },
              {
                title: t('common.available'),
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
