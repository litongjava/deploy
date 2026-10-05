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
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import React, { useCallback, useEffect, useState } from 'react';

const { Text, Paragraph } = Typography;

/** 安装记录就是内置项目 system-toolchain 的运行历史 */
const INSTALL_PROJECT_KEY = 'system-toolchain';

interface FieldFormValues {
  [key: string]: string;
}

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

  /** 已改过的参数拼成一行摘要（没改过返回空串） */
  const paramsSummary = (c: ToolchainComponent) => {
    const changed = (c.fields || []).filter((f) => f.value);
    if (changed.length === 0) return '';
    return changed.map((f) => `${f.key}=${f.value}`).join('  ');
  };

  const runInstall = async (keys: string[], dryRun: boolean) => {
    setBusy(true);
    try {
      const res = await systemApi.installToolchains({ components: keys, dryRun, force: !dryRun });
      if (res?.nothingToDo) {
        messageApi.info('所选组件都已安装；要换版本请点该行的「编辑」改参数后「重装」');
        load();
        return;
      }
      messageApi.success(
        dryRun
          ? `已开始预演（${keys.length} 项），正在跳转日志…`
          : `已开始安装（${keys.length} 项），正在跳转日志…`,
      );
      history.push(`/runs/${res.runId}`);
    } catch (e) {
      // 统一错误提示
    } finally {
      setBusy(false);
    }
  };

  const confirm = (keys: string[], dryRun: boolean, label: string) => {
    const picked = (data?.components || []).filter((c) => keys.includes(c.key));
    Modal.confirm({
      title: dryRun ? `预演：${label}` : `确认安装：${label}`,
      width: 640,
      content: (
        <Space direction="vertical" size={6} style={{ width: '100%' }}>
          <div>
            {picked.map((c) => (
              <div key={c.key} style={{ marginBottom: 4 }}>
                · <Text strong>{c.name}</Text>
                <Text type="secondary">（{c.estimate}）</Text>
                {paramsSummary(c) ? (
                  <div style={{ fontSize: 12 }}>
                    <Text type="secondary">参数：{paramsSummary(c)}</Text>
                  </div>
                ) : null}
              </div>
            ))}
          </div>
          <Text type="secondary">
            {dryRun
              ? '预演只打印将会执行的动作（apt 装包、下载解压、写 profile.d），不做任何修改。'
              : '安装以 root 在服务端机器上执行：apt 装包、下载解压、写 /etc/profile.d。几分钟内完成，可在运行详情页看实时日志或取消。'}
          </Text>
        </Space>
      ),
      okText: dryRun ? '开始预演' : '开始安装',
      cancelText: '取消',
      onOk: () => runInstall(keys, dryRun),
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
      messageApi.success(`${editing.name} 的参数已保存，下次安装/重装生效`);
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
      messageApi.success('脚本已下载，可手动在服务器上执行');
    } catch (e: any) {
      messageApi.error(e?.message || '下载失败');
    }
  };

  const components = data?.components || [];
  const missing = data?.missing || [];
  const installedCount = components.length - missing.length;

  const columns = [
    {
      title: '组件',
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
      title: '状态',
      dataIndex: 'installed',
      width: 100,
      render: (installed: boolean) =>
        installed ? (
          <Tag icon={<CheckCircleFilled />} color="success">
            已安装
          </Tag>
        ) : (
          <Tag icon={<CloseCircleFilled />} color="error">
            缺失
          </Tag>
        ),
    },
    {
      title: '版本 / 位置',
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
      title: '安装参数',
      dataIndex: 'fields',
      width: 210,
      render: (_: unknown, row: ToolchainComponent) => {
        const changed = (row.fields || []).filter((f) => f.value);
        if (changed.length === 0) {
          return <Text type="secondary">默认</Text>;
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
    { title: '说明', dataIndex: 'note' },
    {
      title: '操作',
      valueType: 'option',
      width: 140,
      render: (_: unknown, row: ToolchainComponent) => [
        <Tooltip key="edit" title={canAdmin ? '改版本 / 下载地址 / 安装目录' : '仅管理员可改'}>
          <a
            onClick={() =>
              canAdmin ? openEdit(row) : messageApi.warning('只有管理员可以修改安装参数')
            }
          >
            <EditOutlined /> 编辑
          </a>
        </Tooltip>,
        <Tooltip
          key="install"
          title={canAdmin ? (row.installed ? '按当前参数重装' : '按当前参数安装') : '仅管理员可安装'}
        >
          <a
            onClick={() =>
              canAdmin
                ? confirm([row.key], false, `${row.installed ? '重装' : '安装'} ${row.name}`)
                : messageApi.warning('只有管理员可以安装工具链')
            }
          >
            <ThunderboltOutlined /> {row.installed ? '重装' : '安装'}
          </a>
        </Tooltip>,
      ],
    },
  ];

  const runColumns = [
    {
      title: '时间',
      dataIndex: 'createdAt',
      width: 180,
      render: (v: string, row: Run) => (
        <Space direction="vertical" size={0}>
          <span>{formatTime(v)}</span>
          <Text type="secondary" style={{ fontSize: 12 }}>
            运行 #{row.id} · 构建号 {row.buildNumber}
          </Text>
        </Space>
      ),
    },
    {
      title: '装的组件',
      dataIndex: 'params',
      width: 200,
      render: (_: unknown, row: Run) => <Text code>{(row.params || {}).components || '-'}</Text>,
    },
    {
      title: '参数',
      dataIndex: 'params',
      render: (_: unknown, row: Run) => {
        const entries = Object.entries(row.params || {}).filter(([k]) => k !== 'components');
        if (entries.length === 0) return <Text type="secondary">默认</Text>;
        return (
          <Text type="secondary" style={{ fontSize: 12 }}>
            {entries.map(([k, v]) => `${k}=${v}`).join('  ')}
          </Text>
        );
      },
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
      title: '耗时',
      dataIndex: 'durationMs',
      width: 90,
      render: (v: number) => formatDuration(v),
    },
    {
      title: '日志',
      width: 80,
      render: (_: unknown, row: Run) => <a onClick={() => history.push(`/runs/${row.id}`)}>查看</a>,
    },
  ];

  return (
    <Card
      title="工具链一键安装"
      style={{ marginTop: 16 }}
      extra={
        <Space>
          <Button icon={<DownloadOutlined />} onClick={download}>
            下载脚本
          </Button>
          <Button
            icon={<ReloadOutlined />}
            onClick={() => {
              load();
              loadRuns();
            }}
            loading={loading}
          >
            重新检测
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
          message="只有管理员可以安装或修改工具链参数"
          description="你可以查看各组件是否就绪、各自的版本与安装位置，以及历史安装记录。"
        />
      ) : null}

      {!supported ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={`当前服务端是 ${data?.platform}，一键安装只支持 Linux`}
          description="安装脚本基于 bash 与 apt/dnf；这里仍可以点「预演」看看它打算做什么，或下载脚本到 Linux 机器上手动执行。"
        />
      ) : null}

      {missing.length > 0 ? (
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={`${missing.length} 个组件缺失：${missing.join('、')}（已就绪 ${installedCount}/${components.length}）`}
          description="点「一键安装缺失」即可在服务端自动安装；也可以点某一行的「编辑」改版本 / 下载地址 / 安装目录，再点「安装」。安装过程是一次普通运行，可看实时日志、随时取消。"
        />
      ) : (
        <Alert
          style={{ marginBottom: 12 }}
          type="success"
          showIcon
          message="全部组件都已就绪"
          description="要换版本就点对应行的「编辑」改参数，然后点「重装」。"
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
          onClick={() => confirm(missing, false, `安装全部缺失（${missing.length} 项）`)}
        >
          一键安装缺失（{missing.length}）
        </Button>
        <Button
          loading={busy}
          disabled={!canAdmin || selected.length === 0}
          onClick={() => confirm(selected, false, `安装所选（${selected.length} 项）`)}
        >
          安装所选（{selected.length}）
        </Button>
        <Button
          icon={<ExperimentOutlined />}
          loading={busy}
          disabled={selected.length === 0}
          onClick={() => confirm(selected, true, `预演所选（${selected.length} 项）`)}
        >
          预演
        </Button>
        <Text type="secondary" style={{ fontSize: 12 }}>
          脚本位置：<Text code>{data?.scriptPath || '-'}</Text>
        </Text>
      </Space>

      <Card
        type="inner"
        title="安装记录"
        style={{ marginTop: 16 }}
        extra={<a onClick={loadRuns}>刷新记录</a>}
      >
        <Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
          每次安装 / 重装都是一次普通运行，记录里能看到用了哪些参数（版本、下载地址、安装目录）；
          具体「装到哪」见运行日志末尾的汇总表与上面「版本 / 位置」列。
        </Paragraph>
        <Table<Run>
          size="small"
          rowKey="id"
          loading={runsLoading}
          pagination={false}
          dataSource={runs}
          columns={runColumns as any}
          locale={{ emptyText: '还没有安装记录' }}
        />
      </Card>

      <Modal
        open={!!editing}
        title={editing ? `编辑安装参数：${editing.name}` : ''}
        onCancel={() => setEditing(null)}
        onOk={saveEdit}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        destroyOnClose
        width={680}
      >
        <Paragraph type="secondary" style={{ fontSize: 12 }}>
          留空表示用脚本默认值。保存后点该行的「{editing?.installed ? '重装' : '安装'}」即按新参数执行，
          参数会记录在那次安装的运行里。
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
                      默认 <Text code>{f.default}</Text>
                    </>
                  ) : null}
                </span>
              }
            >
              <Input placeholder={f.placeholder || f.default || ''} allowClear />
            </Form.Item>
          ))}
          {(editing?.fields || []).length === 0 ? (
            <Alert type="info" showIcon message="这个组件没有可配置的参数" />
          ) : null}
        </Form>
      </Modal>
    </Card>
  );
};

export default ToolchainPanel;
