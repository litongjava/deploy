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
import { t } from '@/i18n';

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
  docker: t('Docker 容器'),
  native: t('本机安装（native）'),
};

/**
 * 非安装类操作的文案。
 *
 * 为什么单独抽一张表：这些动作都要二次确认，而确认框里的提示词必须点哪个说哪个，
 * 散在 onClick 里写容易复制粘贴出错（把「停止」的说明挂在「移除」上，是会让人删库的）。
 */
const ACTION_META: Record<string, { label: string; hint: string; danger?: boolean }> = {
  start: {
    label: t('启动'),
    hint: t('按当前方式启动这些中间件；已经在运行的会按后端的判断跳过。'),
  },
  stop: {
    label: t('停止'),
    hint: t('停止进程或容器。数据卷 / 数据目录都会被保留，之后可以再次启动。'),
  },
  restart: {
    label: t('重启'),
    hint: t('先停再起，用于改完参数后生效。数据卷 / 数据目录都会被保留。'),
  },
  remove: {
    label: t('移除'),
    hint: t('移除容器或卸载软件包，并删除配置。注意：数据卷与数据目录【不会被删除】，数据仍然留在磁盘上，需要彻底清理请登录服务器手动处理。'),
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
        messageApi.info(res.message || t('所选中间件没有需要执行的动作'));
        load();
        return;
      }
      const verb = ACTION_META[action]?.label || t('操作');
      messageApi.success(
        action === 'install'
          ? t('{action}（{count} 项），正在跳转日志…', {
              action: dryRun ? t('已开始预演') : t('已开始安装'),
              count: keys.length,
            })
          : t('已开始{verb}（{length} 项），正在跳转日志…', { verb: verb, length: keys.length }),
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
      title: t('确认{label}：{p1}', { label: meta.label, p1: pickedNames(keys) }),
      width: 640,
      okText: t('执行{label}', { label: meta.label }),
      cancelText: t('取消'),
      okButtonProps: meta.danger ? { danger: true } : undefined,
      content: (
        <Space direction="vertical" size={6} style={{ width: '100%' }}>
          <div>
            {t('将对这些中间件执行{label}：', { label: meta.label })}
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
      messageApi.success(t('{name} 的参数已保存，下次安装 / 启动生效', { name: editing.name }));
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
      messageApi.success(t('脚本已下载，可手动在服务器上执行'));
    } catch (e: any) {
      messageApi.error(e?.message || t('下载失败'));
    }
  };

  /** 安装 / 预演 Modal 里可选的 mode 列表（并集） */
  const installModes = collectModes(components.filter((c) => installKeys.includes(c.key)));

  const columns = [
    {
      title: t('名称'),
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
    { title: t('说明'), dataIndex: 'note' },
    {
      title: t('方式'),
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
                {t('当前生效：{mode}', { mode: MODE_LABEL[effective] || effective })}
              </Text>
            ) : null}
          </Space>
        );
      },
    },
    {
      title: t('状态'),
      dataIndex: 'installed',
      width: 150,
      render: (_: unknown, row: MiddlewareComponent) => (
        <Space direction="vertical" size={2}>
          {row.installed ? (
            <Tag icon={<CheckCircleFilled />} color="success">
               {t('已安装')} </Tag>
          ) : (
            <Tag icon={<CloseCircleFilled />} color="error">
               {t('未安装')} </Tag>
          )}
          {row.running ? (
            <Tag icon={<PlayCircleOutlined />} color="processing">
               {t('运行中')} </Tag>
          ) : (
            <Tag>{t('未运行')}</Tag>
          )}
        </Space>
      ),
    },
    {
      title: t('版本'),
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
      title: t('位置'),
      dataIndex: 'path',
      width: 220,
      render: (v: string) => (
        <Text type="secondary" style={{ fontSize: 12, wordBreak: 'break-all' }}>
          {v || '-'}
        </Text>
      ),
    },
    {
      title: t('操作'),
      valueType: 'option',
      width: 150,
      render: (_: unknown, row: MiddlewareComponent) => [
        <Tooltip key="edit" title={canAdmin ? t('改端口 / 密码 / 数据目录 / 版本') : t('仅管理员可改')}>
          <a
            onClick={() =>
              canAdmin ? openEdit(row) : messageApi.warning(t('只有管理员可以修改中间件参数'))
            }
          >
            <EditOutlined /> {t('编辑')}
          </a>
        </Tooltip>,
        <Tooltip
          key="install"
          title={canAdmin ? (row.installed ? t('按当前参数重装') : t('按当前参数安装')) : t('仅管理员可安装')}
        >
          <a
            onClick={() =>
              canAdmin
                ? openInstall([row.key], false)
                : messageApi.warning(t('只有管理员可以安装中间件'))
            }
          >
            <ThunderboltOutlined /> {row.installed ? t('重装') : t('安装')}
          </a>
        </Tooltip>,
      ],
    },
  ];

  return (
    <Card
      title={t('中间件')}
      style={{ marginTop: 16 }}
      extra={
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
           {t('重新检测')} </Button>
      }
    >
      {contextHolder}

      <Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 12 }}>
        {t('在这里安装和启停常用的中间件（数据库 / 缓存 / 搜索）。安装方式分两种：Docker 容器（')}
        <Text code>docker</Text>
        {t('，环境干净、卸载不留痕）与装在本机（')}
        <Text code>native</Text>
        {t('，适合不想引入 Docker 的机器）。每次操作都是一次普通运行，可看实时日志、随时取消。')}
      </Paragraph>

      <Space size={6} wrap style={{ marginBottom: 12 }}>
        <Text type="secondary" style={{ fontSize: 12 }}>
           {t('平台')} </Text>
        <Tag>{data?.platform || '-'}</Tag>
        <Text type="secondary" style={{ fontSize: 12 }}>
          Docker
        </Text>
        <Tag color={data?.dockerAvailable ? 'success' : 'warning'}>
          {data?.dockerAvailable ? data?.dockerVersion || t('可用') : t('不可用')}
        </Tag>
      </Space>

      {!canAdmin ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="info"
          showIcon
          message={t('只有管理员可以安装、启停或修改中间件参数')}
          description={t('你可以查看各中间件是否安装 / 运行、版本与位置，以及各自的参数。')}
        />
      ) : null}

      {/* data 为空只在首次加载 / 接口失败时出现，那种情况下报「没有 Docker」是误报，所以先判 data */}
      {data && !data.dockerAvailable ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={t('这台机器上没有检测到可用的 Docker')}
          description={t('可以改用 native 方式安装（部分中间件支持），或者先在上面的工具链面板 / 服务器上装好 Docker 再回来。以 docker 方式提交但没有 Docker 时，运行会直接失败。')}
        />
      ) : null}

      {!supported ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={t('当前服务端是 {platform}，一键安装只支持 Linux', { platform: data?.platform })}
          description={t('这里仍可以点「预演」看看它打算做什么，或下载脚本到 Linux 机器上手动执行。')}
        />
      ) : null}

      {/* 清单还没回来时不报「全部都已安装」：空数组会被误读成「环境已经齐了」 */}
      {components.length > 0 && missing.length > 0 ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={t('{length} 个中间件未安装：{p1}（已就绪 {installedCount}/{p3}）', { length: missing.length, p1: missing.join('、'), installedCount: installedCount, p3: components.length })}
          description={t('点「一键安装缺失」即可在服务端自动安装；也可以点某一行的「编辑」改端口 / 密码 / 数据目录 / 版本，再点「安装」。')}
        />
      ) : null}
      {components.length > 0 && missing.length === 0 ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="success"
          showIcon
          message={t('全部中间件都已安装')}
          description={t('要换版本或改端口就点对应行的「编辑」改参数，然后点「重装」。')}
        />
      ) : null}

      <Space size={6} style={{ marginBottom: 6 }} wrap>
        <Text strong>{t('操作对象')}</Text>
        <Text type="secondary" style={{ fontSize: 12 }}>
           {t('勾选后可用下面的按钮批量安装 / 启停 / 移除；每行右侧也可以只操作这一个')} </Text>
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
{t('一键安装缺失（{count}）', { count: missing.length })}
        </Button>
        <Button
          icon={<ThunderboltOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => openInstall(selected, false)}
        >
           {t('安装所选')} </Button>
        <Button
          icon={<PlayCircleOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => confirmAction(selected, 'start')}
        >
           {t('启动所选')} </Button>
        <Button
          icon={<PoweroffOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => confirmAction(selected, 'stop')}
        >
           {t('停止所选')} </Button>
        <Button
          icon={<ReloadOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => confirmAction(selected, 'restart')}
        >
           {t('重启所选')} </Button>
        <Button
          danger
          icon={<DeleteOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => confirmAction(selected, 'remove')}
        >
           {t('移除所选')} </Button>
        <Button
          icon={<ExperimentOutlined />}
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => openInstall(selected, true)}
        >
           {t('预演（dry-run）')} </Button>
        <Button icon={<DownloadOutlined />} onClick={download}>
           {t('下载脚本')} </Button>
      </Space>

      <Paragraph type="secondary" style={{ fontSize: 12, marginTop: 12, marginBottom: 0 }}>
        {t('每次操作都是一次普通运行，可在运行列表按项目')} <Text code>{projectKey}</Text>{' '}
        {t('查看历史。')}
        {data?.scriptPath ? (
          <>
            {' '}
{t('脚本位置：')}<Text code>{data.scriptPath}</Text>
          </>
        ) : null}
        {data?.hint ? ` ${data.hint}` : ''}
      </Paragraph>

      {/* ---------- 安装 / 预演：先选方式再提交 ---------- */}
      <Modal
        open={installOpen}
        title={
          installDryRun
            ? t('预演：{p0}', { p0: pickedNames(installKeys) })
            : t('安装：{p0}', { p0: pickedNames(installKeys) })
        }
        onCancel={() => setInstallOpen(false)}
        onOk={() => submit(installKeys, 'install', installDryRun, installMode)}
        confirmLoading={busy}
        okText={installDryRun ? t('开始预演') : t('开始安装')}
        cancelText={t('取消')}
        destroyOnClose
        width={620}
      >
        <Paragraph type="secondary" style={{ fontSize: 12 }}>
          {installDryRun
            ? t('预演只打印将会执行的动作（拉镜像 / 装包 / 写配置 / 起服务），不做任何修改。')
            : t('安装以 root 在服务端机器上执行：拉镜像或装包、写配置、起服务。几分钟内完成，可在运行详情页看实时日志或取消。')}
        </Paragraph>
        <Form layout="vertical">
          <Form.Item
            label={t('安装方式')}
            extra={
              <span style={{ fontSize: 12 }}>
                 {t('只列出所选中间件支持的方式；多个中间件的可选方式不一致时取并集，接口一次只能带一种方式。')} </span>
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
{t('{name}：支持 {modes}，默认 {mode}', {
                  name: c.name,
                  modes: (c.modes?.length ? c.modes : ['docker']).join(' / '),
                  mode: MODE_LABEL[c.defaultMode] || c.defaultMode || 'docker',
                })}
                {c.estimate ? t(' · 约 {estimate}', { estimate: c.estimate }) : ''}
              </Text>
            ))}
        </Space>
      </Modal>

      {/* ---------- 参数编辑：字段完全由后端下发 ---------- */}
      <Modal
        open={!!editing}
        title={editing ? t('编辑参数：{name}', { name: editing.name }) : ''}
        onCancel={() => setEditing(null)}
        onOk={saveEdit}
        confirmLoading={saving}
        okText={t('保存')}
        cancelText={t('取消')}
        destroyOnClose
        width={680}
      >
        <Paragraph type="secondary" style={{ fontSize: 12 }}>
{t('留空表示用默认值。保存后点该行的「{action}」或「重启所选」即按新参数执行，参数会记录在那次操作的运行里。', {
            action: editing?.installed ? t('重装') : t('安装'),
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
                      {t('默认')} <Text code>{f.default}</Text>
                    </>
                  ) : null}
                </span>
              }
            >
              <Input placeholder={f.placeholder || f.default || ''} allowClear />
            </Form.Item>
          ))}
          {(editing?.fields || []).length === 0 ? (
            <Alert type="info" showIcon message={t('这个中间件没有可配置的参数')} />
          ) : null}
        </Form>
      </Modal>
    </Card>
  );
};

export default MiddlewarePanel;
