import { downloadToolchainScript, systemApi } from '@/services/system';
import type { ToolchainComponent, ToolchainList } from '@/services/types';
import {
  CheckCircleFilled,
  CloseCircleFilled,
  DownloadOutlined,
  ExperimentOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { history, useAccess } from '@umijs/max';
import { Alert, Button, Card, Modal, Space, Table, Tag, Typography, message } from 'antd';
import React, { useCallback, useEffect, useState } from 'react';

const { Text } = Typography;

/**
 * 工具链一键安装面板（系统设置页）。
 *
 * 后端把安装脚本落盘后当**普通 Run** 跑，所以这里点完只是跳到运行详情页看实时日志：
 * 日志、取消、重跑、下载全都复用，不需要另做一套进度条。
 * 安装动作仅限管理员，且必须先过确认框。
 */
const ToolchainPanel: React.FC = () => {
  const access = useAccess();
  const [data, setData] = useState<ToolchainList | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [messageApi, contextHolder] = message.useMessage();

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

  useEffect(() => {
    load();
  }, [load]);

  const runInstall = async (keys: string[], dryRun: boolean) => {
    setBusy(true);
    try {
      const res = await systemApi.installToolchains({
        components: keys,
        dryRun,
      });
      if (res?.nothingToDo) {
        messageApi.info('所选组件都已安装，无需操作');
        load();
        return;
      }
      messageApi.success(
        dryRun
          ? `已开始预演（${keys.length || '全部缺失'} 项），正在跳转日志…`
          : `已开始安装（${keys.length || '全部缺失'} 项），正在跳转日志…`,
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
      width: 560,
      content: (
        <Space direction="vertical" size={4} style={{ width: '100%' }}>
          <div>
            {picked.map((c) => (
              <div key={c.key}>
                · <Text strong>{c.name}</Text>
                <Text type="secondary">（{c.estimate}）</Text>
              </div>
            ))}
          </div>
          {dryRun ? (
            <Text type="secondary">
              预演只打印将会执行的动作（apt 装包、下载解压、写 /etc/profile.d），不做任何修改。
            </Text>
          ) : (
            <Text type="secondary">
              安装以 root 在服务端机器上执行：调用 apt 装包、下载 JDK / Go / Rust / Node
              并写入 /etc/profile.d。几分钟内完成，可在运行详情页看实时日志或取消。
            </Text>
          )}
        </Space>
      ),
      okText: dryRun ? '开始预演' : '开始安装',
      cancelText: '取消',
      onOk: () => runInstall(keys, dryRun),
    });
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
      width: 110,
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
      title: '版本 / 路径',
      dataIndex: 'version',
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
    { title: '说明', dataIndex: 'note' },
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
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
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
          message="只有管理员可以安装工具链"
          description="你可以查看各组件是否就绪；需要安装时请让管理员在「设置 → 系统设置 → 环境自检」里操作。"
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
          description="点「一键安装缺失」即可在服务端自动安装；安装过程是一次普通运行，可以看实时日志、随时取消。"
        />
      ) : (
        <Alert
          style={{ marginBottom: 12 }}
          type="success"
          showIcon
          message="全部组件都已就绪"
          description={data?.hint}
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
    </Card>
  );
};

export default ToolchainPanel;
