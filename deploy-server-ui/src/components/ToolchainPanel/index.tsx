import { t } from '@/i18n';
import { runApi } from '@/services/run';
import { downloadToolchainScript, systemApi } from '@/services/system';
import type { Run, ToolchainComponent, ToolchainList } from '@/services/types';
import { RUN_STATUS_META, formatDuration, formatTime } from '@/utils/constants';
import {
  CheckCircleFilled,
  CloseCircleFilled,
  DownloadOutlined,
  EditOutlined,
  ExperimentOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { history, useAccess } from '@umijs/max';
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Radio,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import React, { useCallback, useEffect, useRef, useState } from 'react';

const { Text, Paragraph } = Typography;

/** 安装记录就是内置项目 system-toolchain 的运行历史 */
const INSTALL_PROJECT_KEY = 'system-toolchain';

interface FieldFormValues {
  [key: string]: string;
}

type Mirror = 'off' | 'cn';

/** 已改过的参数拼成一行摘要（没改过返回空串） */
const paramsSummary = (c: ToolchainComponent) => {
  const changed = (c.fields || []).filter((f) => f.value);
  if (changed.length === 0) return '';
  return changed.map((f) => `${f.key}=${f.value}`).join('  ');
};

/**
 * 每个组件自己的安装源说明。
 *
 * 以前这里是一段所有组件共用的「Maven settings.xml / Cargo 配置」提示，勾
 * nvm 或 Go 时也会看到，属于串台。现在按组件和所选安装源给出真正相关的说明：
 * 包管理器组件（cc/java/maven/certbot）不走安装源，只有下载/镜像类组件才受影响。
 */
const componentHint = (key: string, mirror: Mirror): string => {
  switch (key) {
    case 'cc':
      return t('toolchain.hint.cc');
    case 'java':
      return t('toolchain.hint.java');
    case 'maven':
      return mirror === 'cn' ? t('toolchain.hint.maven.cn') : t('toolchain.hint.maven.off');
    case 'nvm':
      return mirror === 'cn' ? t('toolchain.hint.nvm.cn') : t('toolchain.hint.nvm.off');
    case 'node':
      return mirror === 'cn' ? t('toolchain.hint.node.cn') : t('toolchain.hint.node.off');
    case 'pnpm':
      return mirror === 'cn' ? t('toolchain.hint.pnpm.cn') : t('toolchain.hint.pnpm.off');
    case 'go':
      return mirror === 'cn' ? t('toolchain.hint.go.cn') : t('toolchain.hint.go.off');
    case 'rust':
      return mirror === 'cn' ? t('toolchain.hint.rust.cn') : t('toolchain.hint.rust.off');
    case 'certbot':
      return t('toolchain.hint.certbot');
    default:
      return '';
  }
};

interface InstallConfirmContentProps {
  picked: ToolchainComponent[];
  dryRun: boolean;
  onMirrorChange: (mirror: Mirror) => void;
}

/** 确认弹窗正文：安装源切换后，各组件下面的提示随之更新。 */
const InstallConfirmContent: React.FC<InstallConfirmContentProps> = ({
  picked,
  dryRun,
  onMirrorChange,
}) => {
  const [mirror, setMirror] = useState<Mirror>('off');

  return (
    <Space direction="vertical" size={6} style={{ width: '100%' }}>
      <Text strong>{t('toolchain.source')}</Text>
      <Radio.Group
        value={mirror}
        onChange={(e) => {
          setMirror(e.target.value);
          onMirrorChange(e.target.value);
        }}
      >
        <Space direction="vertical">
          <Radio value="off">{t('toolchain.source.official')}</Radio>
          <Radio value="cn">{t('toolchain.source.cn')}</Radio>
        </Space>
      </Radio.Group>
      <div>
        {picked.map((c) => {
          const hint = componentHint(c.key, mirror);
          return (
            <div key={c.key} style={{ marginBottom: 8 }}>
              <div>
                · <Text strong>{c.name}</Text>
                <Text type="secondary">（{c.estimate}）</Text>
              </div>
              {paramsSummary(c) ? (
                <div style={{ fontSize: 12 }}>
                  <Text type="secondary">
                    {t('toolchain.parametersLabel')}
                    {paramsSummary(c)}
                  </Text>
                </div>
              ) : null}
              {hint ? (
                <div style={{ fontSize: 12, marginTop: 2 }}>
                  <Text type="secondary">{hint}</Text>
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
      <Text type="secondary">
        {dryRun ? t('toolchain.footer.dryRun') : t('toolchain.footer.install')}
      </Text>
    </Space>
  );
};

/**
 * 工具链一键安装面板（挂在「环境自检」页）。
 *
 * - 安装：后端把内嵌脚本落盘后当**普通 Run** 跑，点完跳到运行详情看实时日志
 * - 参数：每个组件的版本 / 下载地址 / 安装目录都能改（存 settings 表），
 *   改完可以只重装这一个组件；参数会作为运行参数记录在那次安装的运行里
 * - 记录：下方列出最近的安装运行（时间 / 组件 / 参数 / 状态 / 日志）
 */
const ToolchainPanel: React.FC = () => {
  const access = useAccess();
  const [data, setData] = useState<ToolchainList | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [editing, setEditing] = useState<ToolchainComponent | null>(null);
  const [saving, setSaving] = useState(false);
  const [runs, setRuns] = useState<Run[]>([]);
  const [runsLoading, setRunsLoading] = useState(false);
  const [messageApi, contextHolder] = message.useMessage();
  const [form] = Form.useForm<FieldFormValues>();

  const canAdmin = !!access.canAdmin;
  const supported = data?.supported !== false;

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await systemApi.toolchains();
      setData(res);
      // 默认勾选「缺失的」：一键安装缺失是最常用的动作
      setSelected(res?.missing || []);
    } catch (e) {
      setData(null);
    } finally {
      setLoading(false);
    }
  }, []);

  const loadRuns = useCallback(async () => {
    setRunsLoading(true);
    try {
      const res = await runApi.list({ page: 1, pageSize: 8, projectKey: INSTALL_PROJECT_KEY });
      setRuns(res?.list || []);
    } catch (e) {
      setRuns([]);
    } finally {
      setRunsLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    loadRuns();
  }, [load, loadRuns]);

  /** 本次确认框里选中的安装源；弹窗内容是受控组件，值回写到这个 ref 供 onOk 使用 */
  const mirrorRef = useRef<Mirror>('off');

  const runInstall = async (keys: string[], dryRun: boolean, mirror: Mirror) => {
    setBusy(true);
    try {
      const res = await systemApi.installToolchains({
        components: keys,
        dryRun,
        force: !dryRun,
        mirror,
      });
      if (res?.nothingToDo) {
        messageApi.info(t('toolchain.allSelectedComponentsAreInstalled'));
        load();
        return;
      }
      messageApi.success(
        dryRun
          ? t('toolchain.dryRunStartedItemsJumping', { length: keys.length })
          : t('toolchain.installStartedItemsJumpingTo', { length: keys.length }),
      );
      history.push(`/runs/${res.runId}`);
    } finally {
      // 失败时异常继续抛给 Modal.confirm：弹窗不关，允许重试
      setBusy(false);
    }
  };

  const confirm = (keys: string[], dryRun: boolean, label: string) => {
    mirrorRef.current = 'off';
    const picked = (data?.components || []).filter((c) => keys.includes(c.key));
    Modal.confirm({
      title: dryRun
        ? t('toolchain.dryRunTitle', { label: label })
        : t('toolchain.confirmInstall', { label: label }),
      width: 640,
      content: (
        <InstallConfirmContent
          picked={picked}
          dryRun={dryRun}
          onMirrorChange={(m) => {
            mirrorRef.current = m;
          }}
        />
      ),
      okText: dryRun ? t('common.startDryRun') : t('common.startInstall'),
      cancelText: t('common.cancel'),
      onOk: () => runInstall(keys, dryRun, mirrorRef.current),
    });
  };

  const openEdit = (c: ToolchainComponent) => {
    setEditing(c);
    const values: FieldFormValues = {};
    (c.fields || []).forEach((f) => {
      values[f.key] = f.value || '';
    });
    form.setFieldsValue(values);
  };

  const saveEdit = async () => {
    if (!editing) return;
    const values = await form.validateFields();
    setSaving(true);
    try {
      await systemApi.updateToolchain(editing.key, { fields: values });
      messageApi.success(t('toolchain.parametersForSavedTheyTake', { name: editing.name }));
      setEditing(null);
      load();
    } catch (e) {
      // 统一提示
    } finally {
      setSaving(false);
    }
  };

  const download = async () => {
    try {
      const blob = await downloadToolchainScript();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'install-toolchains.sh';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      window.URL.revokeObjectURL(url);
      messageApi.success(t('common.scriptDownloaded'));
    } catch (e: any) {
      messageApi.error(e?.message || t('common.downloadFailed'));
    }
  };

  const components = data?.components || [];
  const missing = data?.missing || [];
  const installedCount = components.length - missing.length;

  const columns = [
    {
      title: t('toolchain.components'),
      dataIndex: 'name',
      render: (name: string, row: ToolchainComponent) => (
        <Space direction="vertical" size={0}>
          <Text strong>{name}</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>
            {row.key} · {row.estimate}
          </Text>
        </Space>
      ),
    },
    {
      title: t('common.status'),
      dataIndex: 'installed',
      width: 100,
      render: (installed: boolean) =>
        installed ? (
          <Tag icon={<CheckCircleFilled />} color="success">
            {t('common.installed')}{' '}
          </Tag>
        ) : (
          <Tag icon={<CloseCircleFilled />} color="error">
            {t('toolchain.missing')}{' '}
          </Tag>
        ),
    },
    {
      title: t('toolchain.versionLocation'),
      dataIndex: 'version',
      width: 250,
      render: (_: string, row: ToolchainComponent) => (
        <Space direction="vertical" size={0}>
          <Text>{row.version || '-'}</Text>
          {row.path ? (
            <Text type="secondary" style={{ fontSize: 12, wordBreak: 'break-all' }}>
              {row.path}
            </Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: t('toolchain.installParameters'),
      dataIndex: 'fields',
      width: 210,
      render: (_: unknown, row: ToolchainComponent) => {
        const changed = (row.fields || []).filter((f) => f.value);
        if (changed.length === 0) {
          return <Text type="secondary">{t('common.default')}</Text>;
        }
        return (
          <Space direction="vertical" size={0}>
            {changed.map((f) => (
              <Text key={f.key} style={{ fontSize: 12 }}>
                {f.key}=<Text code>{f.value}</Text>
              </Text>
            ))}
          </Space>
        );
      },
    },
    { title: t('common.note'), dataIndex: 'note' },
    {
      title: t('common.actions'),
      valueType: 'option',
      width: 140,
      render: (_: unknown, row: ToolchainComponent) => [
        <Tooltip
          key="edit"
          title={
            canAdmin ? t('toolchain.changeVersionDownloadURLInstall') : t('common.adminOnlyEdit')
          }
        >
          <a
            onClick={() =>
              canAdmin
                ? openEdit(row)
                : messageApi.warning(t('toolchain.onlyAdministratorsCanChangeInstall'))
            }
          >
            <EditOutlined /> {t('common.edit')}
          </a>
        </Tooltip>,
        <Tooltip
          key="install"
          title={
            canAdmin
              ? row.installed
                ? t('common.reinstallWithCurrentParameters')
                : t('common.installWithCurrentParameters')
              : t('common.adminOnlyInstall')
          }
        >
          <a
            onClick={() =>
              canAdmin
                ? confirm(
                    [row.key],
                    false,
                    `${row.installed ? t('common.reinstall') : t('common.install')} ${row.name}`,
                  )
                : messageApi.warning(t('toolchain.onlyAdministratorsCanInstallThe'))
            }
          >
            <ThunderboltOutlined /> {row.installed ? t('common.reinstall') : t('common.install')}
          </a>
        </Tooltip>,
      ],
    },
  ];

  const runColumns = [
    {
      title: t('toolchain.time'),
      dataIndex: 'createdAt',
      width: 180,
      render: (v: string, row: Run) => (
        <Space direction="vertical" size={0}>
          <span>{formatTime(v)}</span>
          <Text type="secondary" style={{ fontSize: 12 }}>
            {t('toolchain.runBuild', { id: row.id, buildNumber: row.buildNumber })}
          </Text>
        </Space>
      ),
    },
    {
      title: t('toolchain.componentsInstalled'),
      dataIndex: 'params',
      width: 200,
      render: (_: unknown, row: Run) => <Text code>{(row.params || {}).components || '-'}</Text>,
    },
    {
      title: t('toolchain.parameters'),
      dataIndex: 'params',
      render: (_: unknown, row: Run) => {
        const entries = Object.entries(row.params || {}).filter(([k]) => k !== 'components');
        if (entries.length === 0) return <Text type="secondary">{t('common.default')}</Text>;
        return (
          <Text type="secondary" style={{ fontSize: 12 }}>
            {entries.map(([k, v]) => `${k}=${v}`).join('  ')}
          </Text>
        );
      },
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
      width: 90,
      render: (v: number) => formatDuration(v),
    },
    {
      title: t('common.logs'),
      width: 80,
      render: (_: unknown, row: Run) => (
        <a onClick={() => history.push(`/runs/${row.id}`)}>{t('toolchain.view')}</a>
      ),
    },
  ];

  return (
    <Card
      title={t('toolchain.oneClickToolchainInstall')}
      style={{ marginTop: 16 }}
      extra={
        <Space>
          <Button icon={<DownloadOutlined />} onClick={download}>
            {t('common.downloadScript')}{' '}
          </Button>
          <Button
            icon={<ReloadOutlined />}
            onClick={() => {
              load();
              loadRuns();
            }}
            loading={loading}
          >
            {t('common.recheck')}{' '}
          </Button>
        </Space>
      }
    >
      {contextHolder}

      {!canAdmin ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="info"
          showIcon
          message={t('toolchain.onlyAdministratorsCanInstallOr')}
          description={t('toolchain.readOnlyHint')}
        />
      ) : null}

      {!supported ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={t('common.linuxOnlyInstall', { platform: data?.platform })}
          description={t('toolchain.installScriptTargetsBashAnd')}
        />
      ) : null}

      {missing.length > 0 ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={t('toolchain.componentsMissingReady', {
            length: missing.length,
            p1: missing.join('、'),
            installedCount: installedCount,
            p3: components.length,
          })}
          description={t('toolchain.missingHint')}
        />
      ) : (
        <Alert
          style={{ marginBottom: 12 }}
          type="success"
          showIcon
          message={t('toolchain.allComponentsAreReady')}
          description={t('toolchain.reinstallHint')}
        />
      )}

      <Table<ToolchainComponent>
        size="small"
        rowKey="key"
        loading={loading}
        pagination={false}
        dataSource={components}
        columns={columns as any}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: (keys) => setSelected(keys as string[]),
          getCheckboxProps: () => ({ disabled: !canAdmin }),
        }}
      />

      <Space style={{ marginTop: 12 }} wrap>
        <Button
          type="primary"
          icon={<ThunderboltOutlined />}
          loading={busy}
          disabled={!canAdmin || missing.length === 0}
          onClick={() =>
            confirm(missing, false, t('toolchain.installAllMissing', { length: missing.length }))
          }
        >
          {t('common.installMissing', { count: missing.length })}
        </Button>
        <Button
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() =>
            confirm(
              selected,
              false,
              t('toolchain.installSelectedCount', { length: selected.length }),
            )
          }
        >
          {t('toolchain.installSelected', { count: selected.length })}
        </Button>
        <Button
          icon={<ExperimentOutlined />}
          loading={busy}
          disabled={selected.length === 0}
          onClick={() =>
            confirm(selected, true, t('toolchain.dryRunSelected', { length: selected.length }))
          }
        >
          {t('toolchain.dryRun')}{' '}
        </Button>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {t('common.scriptPath')}
          <Text code>{data?.scriptPath || '-'}</Text>
        </Text>
      </Space>

      <Card
        type="inner"
        title={t('toolchain.installHistory')}
        style={{ marginTop: 16 }}
        extra={<a onClick={loadRuns}>{t('toolchain.refreshRecords')}</a>}
      >
        <Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
          {t('toolchain.historyHint')}{' '}
        </Paragraph>
        <Table<Run>
          size="small"
          rowKey="id"
          loading={runsLoading}
          pagination={false}
          dataSource={runs}
          columns={runColumns as any}
          locale={{ emptyText: t('toolchain.noInstallHistoryYet') }}
        />
      </Card>

      <Modal
        open={!!editing}
        title={editing ? t('toolchain.editInstallParameters', { name: editing.name }) : ''}
        onCancel={() => setEditing(null)}
        onOk={saveEdit}
        confirmLoading={saving}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
        destroyOnClose
        width={680}
      >
        <Paragraph type="secondary" style={{ fontSize: 12 }}>
          {t('toolchain.emptyDefaultHint', {
            action: editing?.installed ? t('common.reinstall') : t('common.install'),
          })}
        </Paragraph>
        <Form form={form} layout="vertical" preserve={false}>
          {(editing?.fields || []).map((f) => (
            <Form.Item
              key={f.key}
              name={f.key}
              label={
                <Space size={6}>
                  <span>{f.label}</span>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {f.key}
                  </Text>
                </Space>
              }
              extra={
                <span style={{ fontSize: 12 }}>
                  {f.help}
                  {f.default ? (
                    <>
                      {' '}
                      {t('common.default')} <Text code>{f.default}</Text>
                    </>
                  ) : null}
                </span>
              }
            >
              <Input placeholder={f.placeholder || f.default || ''} allowClear />
            </Form.Item>
          ))}
          {(editing?.fields || []).length === 0 ? (
            <Alert
              type="info"
              showIcon
              message={t('toolchain.componentHasNoConfigurableParameters')}
            />
          ) : null}
        </Form>
      </Modal>
    </Card>
  );
};

export default ToolchainPanel;
