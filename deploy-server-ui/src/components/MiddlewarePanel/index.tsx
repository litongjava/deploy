import { t } from '@/i18n';
import { downloadMiddlewareScript, systemApi } from '@/services/system';
import type { MiddlewareComponent, MiddlewareList } from '@/services/types';
import {
  CheckCircleFilled,
  CloseCircleFilled,
  DeleteOutlined,
  DownloadOutlined,
  EditOutlined,
  ExperimentOutlined,
  PlayCircleOutlined,
  PoweroffOutlined,
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
import React, { useCallback, useEffect, useState } from 'react';

const { Text, Paragraph } = Typography;

interface FieldFormValues {
  [key: string]: string;
}

/**
 * 把所有被选中间件支持的方式取并集。
 *
 * 为什么要并集而不是取交集：接口的 mode 是请求级的一个字段，没法按组件分别指定；
 * 取交集会让「docker 组件 + 只能 native 的组件」这种常见勾选组合一个方式都选不出来。
 * 组件不支持所选方式时交给后端报错（预演能先看到）。
 */
function collectModes(list: MiddlewareComponent[]): string[] {
  const modes: string[] = [];
  list.forEach((c) => {
    (c.modes?.length ? c.modes : ['docker']).forEach((m) => {
      if (!modes.includes(m)) modes.push(m);
    });
  });
  return modes.length > 0 ? modes : ['docker'];
}

/** 方式的说法统一放这里：界面上到处在用，散着写迟早会出现「docker / Docker 容器」两种口径 */
const MODE_LABEL: Record<string, string> = {
  docker: t('middleware.dockerContainer'),
  native: t('middleware.nativeInstallNative'),
};

/**
 * 非安装类操作的文案。
 *
 * 为什么单独抽一张表：这些动作都要二次确认，而确认框里的提示词必须点哪个说哪个，
 * 散在 onClick 里写容易复制粘贴出错（把「停止」的说明挂在「移除」上，是会让人删库的）。
 */
const ACTION_META: Record<string, { label: string; hint: string; danger?: boolean }> = {
  start: {
    label: t('middleware.start'),
    hint: t('middleware.startThisMiddlewareUsingThe'),
  },
  stop: {
    label: t('middleware.stop'),
    hint: t('middleware.stopTheProcessOrContainer'),
  },
  restart: {
    label: t('middleware.restart'),
    hint: t('middleware.stopThenStartToApply'),
  },
  remove: {
    label: t('middleware.remove'),
    hint: t('middleware.removeTheContainerOrUninstall'),
    danger: true,
  },
};

/**
 * 中间件安装面板（挂在「环境自检」页，接在工具链面板之后）。
 *
 * 与工具链面板同一套思路：后端把内嵌脚本落盘后当**普通 Run** 跑，点完跳到运行详情看实时日志。
 * 差别在于中间件是长期驻留的服务，所以除了装，还要能启停、重启、移除；
 * 并且 PostgreSQL / MySQL / Redis 这类既可以用容器跑，也可以装在本机，因此多了一个「方式」维度。
 */
const MiddlewarePanel: React.FC = () => {
  const access = useAccess();
  const [data, setData] = useState<MiddlewareList | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [editing, setEditing] = useState<MiddlewareComponent | null>(null);
  const [saving, setSaving] = useState(false);
  // 安装 / 预演要先选方式，所以用一个受控 Modal 而不是 Modal.confirm
  const [installOpen, setInstallOpen] = useState(false);
  const [installDryRun, setInstallDryRun] = useState(false);
  const [installKeys, setInstallKeys] = useState<string[]>([]);
  const [installMode, setInstallMode] = useState<string>('docker');
  const [messageApi, contextHolder] = message.useMessage();
  const [form] = Form.useForm<FieldFormValues>();

  const canAdmin = !!access.canAdmin;
  const supported = data?.supported !== false;
  const components = data?.components || [];
  const missing = data?.missing || [];
  const installedCount = components.length - missing.length;
  const projectKey = data?.projectKey || 'system-toolchain';

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await systemApi.middleware();
      setData(res);
      // 默认勾选缺失的：与工具链面板一致，「一键装缺失」是最常用的动作
      setSelected(res?.missing || []);
    } catch (e) {
      setData(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const pickedNames = (keys: string[]) =>
    components
      .filter((c) => keys.includes(c.key))
      .map((c) => c.name)
      .join('、');

  /** 真正提交一次操作；安装与启停共用，靠 action 区分 */
  const submit = async (keys: string[], action: string, dryRun: boolean, mode?: string) => {
    setBusy(true);
    try {
      // 不传 force：中间件的 force 语义是「覆盖已有安装」，误点一次就可能盖掉数据目录，
      // 交给后端默认（已安装则跳过 / 按需重建）更安全。
      const res = await systemApi.installMiddleware({
        components: keys,
        action,
        dryRun,
        mode: action === 'install' ? mode : undefined,
      });
      if (res?.nothingToDo) {
        messageApi.info(res.message || t('middleware.selectedMiddlewareHasNoAction'));
        load();
        return;
      }
      const verb = ACTION_META[action]?.label || t('common.actions');
      messageApi.success(
        action === 'install'
          ? t('middleware.itemsJumpingToLogs', {
              action: dryRun ? t('middleware.dryRunStarted') : t('middleware.installStarted'),
              count: keys.length,
            })
          : t('middleware.startedItemsJumpingToLogs', { verb: verb, length: keys.length }),
      );
      setInstallOpen(false);
      history.push(`/runs/${res.runId}`);
    } catch (e) {
      // 统一错误提示
    } finally {
      setBusy(false);
    }
  };

  /** 启停 / 移除这类破坏性动作：确认框里把组件名逐个列出来，别让人靠记忆核对勾选项 */
  const confirmAction = (keys: string[], action: string) => {
    const meta = ACTION_META[action] || { label: action, hint: '' };
    const picked = components.filter((c) => keys.includes(c.key));
    Modal.confirm({
      title: t('middleware.confirm', { label: meta.label, p1: pickedNames(keys) }),
      width: 640,
      okText: t('middleware.run', { label: meta.label }),
      cancelText: t('common.cancel'),
      okButtonProps: meta.danger ? { danger: true } : undefined,
      content: (
        <Space direction="vertical" size={6} style={{ width: '100%' }}>
          <div>
            {t('middleware.willRunOnThisMiddleware', { label: meta.label })}
            {picked.map((c) => c.name).join(', ')}
          </div>
          <div>
            {picked.map((c) => (
              <div key={c.key} style={{ marginBottom: 4 }}>
                · <Text strong>{c.name}</Text>
                <Text type="secondary">
                  （{c.key}
                  {c.detail ? ` · ${c.detail}` : ''}）
                </Text>
              </div>
            ))}
          </div>
          <Text type={meta.danger ? 'danger' : 'secondary'}>{meta.hint}</Text>
        </Space>
      ),
      onOk: () => submit(keys, action, false),
    });
  };

  const openInstall = (keys: string[], dryRun: boolean) => {
    if (keys.length === 0) return;
    const picked = components.filter((c) => keys.includes(c.key));
    const modes = collectModes(picked);
    const fallback = picked[0]?.defaultMode || 'docker';
    setInstallKeys(keys);
    setInstallDryRun(dryRun);
    setInstallMode(modes.includes(fallback) ? fallback : modes[0]);
    setInstallOpen(true);
  };

  const openEdit = (c: MiddlewareComponent) => {
    setEditing(c);
    const values: FieldFormValues = {};
    (c.fields || []).forEach((f) => {
      // 空值表示「用默认」，所以回填时也用空串，不要把 default 写成已保存的值
      values[f.key] = f.value || '';
    });
    form.setFieldsValue(values);
  };

  const saveEdit = async () => {
    if (!editing) return;
    const values = await form.validateFields();
    setSaving(true);
    try {
      await systemApi.updateMiddleware(editing.key, { fields: values });
      messageApi.success(t('middleware.parametersForSavedTheyTake', { name: editing.name }));
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
      const blob = await downloadMiddlewareScript();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'install-middlewares.sh';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      window.URL.revokeObjectURL(url);
      messageApi.success(t('common.scriptDownloaded'));
    } catch (e: any) {
      messageApi.error(e?.message || t('common.downloadFailed'));
    }
  };

  /** 安装 / 预演 Modal 里可选的 mode 列表（并集） */
  const installModes = collectModes(components.filter((c) => installKeys.includes(c.key)));

  const columns = [
    {
      title: t('common.name'),
      dataIndex: 'name',
      width: 200,
      render: (name: string, row: MiddlewareComponent) => (
        <Space direction="vertical" size={0}>
          <Text strong>{name}</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>
            {row.key}
            {row.category ? ` · ${row.category}` : ''}
          </Text>
        </Space>
      ),
    },
    { title: t('common.note'), dataIndex: 'note' },
    {
      title: t('middleware.method'),
      dataIndex: 'mode',
      width: 150,
      render: (_: unknown, row: MiddlewareComponent) => {
        const mode = row.mode || row.defaultMode || '';
        const effective = row.effectiveMode || '';
        return (
          <Space direction="vertical" size={0}>
            {mode === 'docker' ? (
              <Text>{MODE_LABEL.docker}</Text>
            ) : (
              <Tag color="geekblue">{MODE_LABEL[mode] || mode}</Tag>
            )}
            {effective && effective !== mode ? (
              <Text type="secondary" style={{ fontSize: 12 }}>
                {t('middleware.currentlyEffective', { mode: MODE_LABEL[effective] || effective })}
              </Text>
            ) : null}
          </Space>
        );
      },
    },
    {
      title: t('common.status'),
      dataIndex: 'installed',
      width: 150,
      render: (_: unknown, row: MiddlewareComponent) => (
        <Space direction="vertical" size={2}>
          {row.installed ? (
            <Tag icon={<CheckCircleFilled />} color="success">
              {t('common.installed')}{' '}
            </Tag>
          ) : (
            <Tag icon={<CloseCircleFilled />} color="error">
              {t('middleware.notInstalled')}{' '}
            </Tag>
          )}
          {row.running ? (
            <Tag icon={<PlayCircleOutlined />} color="processing">
              {t('common.running')}{' '}
            </Tag>
          ) : (
            <Tag>{t('middleware.notRun')}</Tag>
          )}
        </Space>
      ),
    },
    {
      title: t('middleware.version'),
      dataIndex: 'version',
      width: 140,
      render: (v: string, row: MiddlewareComponent) => (
        <Space direction="vertical" size={0}>
          <Text>{v || '-'}</Text>
          {row.detail ? (
            <Text type="secondary" style={{ fontSize: 12 }}>
              {row.detail}
            </Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: t('middleware.location'),
      dataIndex: 'path',
      width: 220,
      render: (v: string) => (
        <Text type="secondary" style={{ fontSize: 12, wordBreak: 'break-all' }}>
          {v || '-'}
        </Text>
      ),
    },
    {
      title: t('common.actions'),
      valueType: 'option',
      width: 150,
      render: (_: unknown, row: MiddlewareComponent) => [
        <Tooltip
          key="edit"
          title={
            canAdmin ? t('middleware.changePortPasswordDataDirectory') : t('common.adminOnlyEdit')
          }
        >
          <a
            onClick={() =>
              canAdmin
                ? openEdit(row)
                : messageApi.warning(t('middleware.onlyAdministratorsCanChangeMiddleware'))
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
                ? openInstall([row.key], false)
                : messageApi.warning(t('middleware.adminOnlyInstall'))
            }
          >
            <ThunderboltOutlined /> {row.installed ? t('common.reinstall') : t('common.install')}
          </a>
        </Tooltip>,
      ],
    },
  ];

  return (
    <Card
      title={t('middleware.middleware')}
      style={{ marginTop: 16 }}
      extra={
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
          {t('common.recheck')}{' '}
        </Button>
      }
    >
      {contextHolder}

      <Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 12 }}>
        {t('middleware.introBeforeDocker')}
        <Text code>docker</Text>
        {t('middleware.introBetween')}
        <Text code>native</Text>
        {t('middleware.introAfterNative')}
      </Paragraph>

      <Space size={6} wrap style={{ marginBottom: 12 }}>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {t('middleware.platform')}{' '}
        </Text>
        <Tag>{data?.platform || '-'}</Tag>
        <Text type="secondary" style={{ fontSize: 12 }}>
          Docker
        </Text>
        <Tag color={data?.dockerAvailable ? 'success' : 'warning'}>
          {data?.dockerAvailable
            ? data?.dockerVersion || t('common.available')
            : t('middleware.unavailable')}
        </Tag>
      </Space>

      {!canAdmin ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="info"
          showIcon
          message={t('middleware.onlyAdministratorsCanInstallStart')}
          description={t('middleware.canSeeWhetherEachMiddleware')}
        />
      ) : null}

      {/* data 为空只在首次加载 / 接口失败时出现，那种情况下报「没有 Docker」是误报，所以先判 data */}
      {data && !data.dockerAvailable ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={t('middleware.noUsableDockerWasDetected')}
          description={t('middleware.canInstallWithTheNative')}
        />
      ) : null}

      {!supported ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={t('common.linuxOnlyInstall', { platform: data?.platform })}
          description={t('middleware.canStillClickDryRun')}
        />
      ) : null}

      {/* 清单还没回来时不报「全部都已安装」：空数组会被误读成「环境已经齐了」 */}
      {components.length > 0 && missing.length > 0 ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={t('middleware.middlewareNotInstalledReady', {
            length: missing.length,
            p1: missing.join('、'),
            installedCount: installedCount,
            p3: components.length,
          })}
          description={t('middleware.clickInstallMissingToInstall')}
        />
      ) : null}
      {components.length > 0 && missing.length === 0 ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="success"
          showIcon
          message={t('middleware.allMiddlewareIsInstalled')}
          description={t('middleware.changeVersionOrPortClick')}
        />
      ) : null}

      <Space size={6} style={{ marginBottom: 6 }} wrap>
        <Text strong>{t('middleware.targets')}</Text>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {t('middleware.checkToUseTheButtons')}{' '}
        </Text>
      </Space>

      <Table<MiddlewareComponent>
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
          onClick={() => openInstall(missing, false)}
        >
          {t('common.installMissing', { count: missing.length })}
        </Button>
        <Button
          icon={<ThunderboltOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => openInstall(selected, false)}
        >
          {t('middleware.installSelected')}{' '}
        </Button>
        <Button
          icon={<PlayCircleOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => confirmAction(selected, 'start')}
        >
          {t('middleware.startSelected')}{' '}
        </Button>
        <Button
          icon={<PoweroffOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => confirmAction(selected, 'stop')}
        >
          {t('middleware.stopSelected')}{' '}
        </Button>
        <Button
          icon={<ReloadOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => confirmAction(selected, 'restart')}
        >
          {t('middleware.restartSelected')}{' '}
        </Button>
        <Button
          danger
          icon={<DeleteOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => confirmAction(selected, 'remove')}
        >
          {t('middleware.removeSelected')}{' '}
        </Button>
        <Button
          icon={<ExperimentOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => openInstall(selected, true)}
        >
          {t('middleware.dryRun')}{' '}
        </Button>
        <Button icon={<DownloadOutlined />} onClick={download}>
          {t('common.downloadScript')}{' '}
        </Button>
      </Space>

      <Paragraph type="secondary" style={{ fontSize: 12, marginTop: 12, marginBottom: 0 }}>
        {t('middleware.everyActionIsAnOrdinary')} <Text code>{projectKey}</Text>{' '}
        {t('middleware.viewHistory')}
        {data?.scriptPath ? (
          <>
            {' '}
            {t('common.scriptPath')}
            <Text code>{data.scriptPath}</Text>
          </>
        ) : null}
        {data?.hint ? ` ${data.hint}` : ''}
      </Paragraph>

      {/* ---------- 安装 / 预演：先选方式再提交 ---------- */}
      <Modal
        open={installOpen}
        title={
          installDryRun
            ? t('middleware.dryRunTitle', { p0: pickedNames(installKeys) })
            : t('middleware.install', { p0: pickedNames(installKeys) })
        }
        onCancel={() => setInstallOpen(false)}
        onOk={() => submit(installKeys, 'install', installDryRun, installMode)}
        confirmLoading={busy}
        okText={installDryRun ? t('common.startDryRun') : t('common.startInstall')}
        cancelText={t('common.cancel')}
        destroyOnClose
        width={620}
      >
        <Paragraph type="secondary" style={{ fontSize: 12 }}>
          {installDryRun
            ? t('middleware.dryRunOnlyPrintsThe')
            : t('middleware.installationRunsAsRootOn')}
        </Paragraph>
        <Form layout="vertical">
          <Form.Item
            label={t('middleware.installMethod')}
            extra={
              <span style={{ fontSize: 12 }}>{t('middleware.onlyMethodsSupportedByThe')} </span>
            }
          >
            <Radio.Group value={installMode} onChange={(e) => setInstallMode(e.target.value)}>
              {installModes.map((m) => (
                <Radio key={m} value={m}>
                  {MODE_LABEL[m] || m}
                </Radio>
              ))}
            </Radio.Group>
          </Form.Item>
        </Form>
        <Space direction="vertical" size={2} style={{ width: '100%' }}>
          {components
            .filter((c) => installKeys.includes(c.key))
            .map((c) => (
              <Text key={c.key} type="secondary" style={{ fontSize: 12 }}>
                {t('middleware.supportsDefault', {
                  name: c.name,
                  modes: (c.modes?.length ? c.modes : ['docker']).join(' / '),
                  mode: MODE_LABEL[c.defaultMode] || c.defaultMode || 'docker',
                })}
                {c.estimate ? t('middleware.estimate', { estimate: c.estimate }) : ''}
              </Text>
            ))}
        </Space>
      </Modal>

      {/* ---------- 参数编辑：字段完全由后端下发 ---------- */}
      <Modal
        open={!!editing}
        title={editing ? t('middleware.editParameters', { name: editing.name }) : ''}
        onCancel={() => setEditing(null)}
        onOk={saveEdit}
        confirmLoading={saving}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
        destroyOnClose
        width={680}
      >
        <Paragraph type="secondary" style={{ fontSize: 12 }}>
          {t('middleware.leaveEmptyToUseThe', {
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
              message={t('middleware.middlewareHasNoConfigurableParameters')}
            />
          ) : null}
        </Form>
      </Modal>
    </Card>
  );
};

export default MiddlewarePanel;
