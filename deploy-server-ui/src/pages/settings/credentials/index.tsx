import { credentialApi, systemApi } from '@/services/system';
import type { Credential, CredentialType, SshKeyInfo } from '@/services/types';
import { CREDENTIAL_TYPE_OPTIONS, formatTime } from '@/utils/constants';
import { PlusOutlined } from '@ant-design/icons';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PageContainer, ProTable } from '@ant-design/pro-components';
import { useAccess } from '@umijs/max';
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Tag,
  Typography,
  message,
} from 'antd';
import React, { useEffect, useRef, useState } from 'react';
import { t } from '@/i18n';

const { Text, Paragraph } = Typography;

const Credentials: React.FC = () => {
  const actionRef = useRef<ActionType>();
  const [messageApi, contextHolder] = message.useMessage();
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Credential | null>(null);
  const [form] = Form.useForm();
  const [saving, setSaving] = useState(false);
  const [testOpen, setTestOpen] = useState(false);
  const [testUrl, setTestUrl] = useState('');
  const [testBranch, setTestBranch] = useState('');
  const [testing, setTesting] = useState(false);
  const type: CredentialType = Form.useWatch('type', form) || 'https_token';
  const access = useAccess();
  // 服务器自身 ~/.ssh 下的公钥（与数据库里的 git 凭据是两回事）。
  const [sshKey, setSshKey] = useState<SshKeyInfo | null>(null);
  const [sshKeyType, setSshKeyType] = useState<'ed25519' | 'rsa'>('ed25519');
  const [genLoading, setGenLoading] = useState(false);
  const sshKeyExists = (sshKey?.keys || []).some((k) => k.type === sshKeyType);

  const loadSshKey = async () => {
    try {
      setSshKey(await systemApi.sshKey());
    } catch (e) {
      // 统一提示（接口不可用时不影响凭据列表）
    }
  };

  useEffect(() => {
    loadSshKey();
  }, []);

  const generateSshKey = async () => {
    setGenLoading(true);
    try {
      const data = await systemApi.generateSshKey({ type: sshKeyType });
      setSshKey(data);
      messageApi.success(t('已生成 SSH 公钥'));
    } catch (e) {
      // 统一提示（同类型密钥已存在时后端返回 40900）
    } finally {
      setGenLoading(false);
    }
  };

  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      messageApi.success(t('已复制'));
      return;
    } catch (e) {
      // 降级方案：clipboard API 在非 HTTPS 下不可用
      const el = document.createElement('textarea');
      el.value = text;
      document.body.appendChild(el);
      el.select();
      try {
        document.execCommand('copy');
        messageApi.success(t('已复制'));
      } catch (err) {
        messageApi.error(t('复制失败，请手动选择文本'));
      }
      document.body.removeChild(el);
    }
  };

  const openCreate = () => {
    setEditing(null);
    form.resetFields();
    form.setFieldsValue({ type: 'https_token' });
    setOpen(true);
  };

  const openEdit = (row: Credential) => {
    setEditing(row);
    form.resetFields();
    form.setFieldsValue({
      name: row.name,
      type: row.type,
      username: row.username,
      secret: '',
      passphrase: '',
    });
    setOpen(true);
  };

  const submit = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      if (editing) {
        await credentialApi.update(editing.id, values);
        messageApi.success(t('已保存'));
      } else {
        await credentialApi.create(values);
        messageApi.success(t('凭据已创建'));
      }
      setOpen(false);
      actionRef.current?.reload();
    } catch (e) {
      // 统一提示
    } finally {
      setSaving(false);
    }
  };

  const testGit = async () => {
    if (!testUrl) {
      messageApi.warning(t('请先填写仓库地址'));
      return;
    }
    setTesting(true);
    try {
      const res = await credentialApi.testGit({
        gitUrl: testUrl,
        credentialId: editing?.id,
        branch: testBranch || undefined,
      });
      if (res?.ok) {
        messageApi.success(t('连接成功：{detail}', { detail: res.message || t('git ls-remote 可用') }));
        setTestOpen(false);
      } else {
        messageApi.error(t('连接失败：{detail}', { detail: res?.message || t('未知原因') }));
      }
    } catch (e) {
      // 统一提示
    } finally {
      setTesting(false);
    }
  };

  const remove = async (id: number) => {
    try {
      await credentialApi.remove(id);
      messageApi.success(t('已删除'));
      actionRef.current?.reload();
    } catch (e) {
      // 统一提示（被引用时后端返回 40900）
    }
  };

  const columns: ProColumns<Credential>[] = [
    {
      title: t('名称'),
      dataIndex: 'name',
      hideInSearch: true,
      render: (_, row) => <Text strong>{row.name}</Text>,
    },
    {
      title: t('类型'),
      dataIndex: 'type',
      hideInSearch: true,
      width: 160,
      render: (_, row) =>
        row.type === 'ssh_key' ? (
          <Tag color="purple">{t('SSH 私钥')}</Tag>
        ) : (
          <Tag color="blue">HTTPS Token</Tag>
        ),
    },
    {
      title: t('用户名'),
      dataIndex: 'username',
      hideInSearch: true,
      render: (_, row) => row.username || '-',
    },
    {
      title: t('创建时间'),
      dataIndex: 'createdAt',
      hideInSearch: true,
      width: 180,
      render: (_, row) => formatTime(row.createdAt),
    },
    {
      title: t('操作'),
      valueType: 'option',
      width: 140,
      render: (_, row) => [
        <a key="edit" onClick={() => openEdit(row)}>
           {t('编辑')} </a>,
        <Popconfirm
          key="del"
          title={t('删除凭据')}
          description={t('如果该凭据被项目引用，后端会拒绝删除。')}
          okText={t('删除')}
          okButtonProps={{ danger: true }}
          cancelText={t('取消')}
          onConfirm={() => remove(row.id)}
        >
          <a style={{ color: '#ff4d4f' }}>{t('删除')}</a>
        </Popconfirm>,
      ],
    },
  ];

  return (
    <PageContainer title={t('凭据管理')}>
      {contextHolder}
      <Alert
        style={{ marginBottom: 16 }}
        type="info"
        showIcon
        message={t('用于 Git 私有仓库拉取')}
        description={t('凭据内容在服务端加密存储，接口永不返回 secret；项目通过「Git 凭据」下拉引用，不在项目里明文存密码。')}
      />
      <Card
        size="small"
        style={{ marginBottom: 16 }}
        title={t('服务器 SSH 公钥')}
        extra={
          <Space>
            <Select
              size="small"
              value={sshKeyType}
              onChange={(v) => setSshKeyType(v)}
              style={{ width: 150 }}
              options={[
                { label: 'ed25519', value: 'ed25519' },
                { label: 'rsa (4096)', value: 'rsa' },
              ]}
            />
            <Popconfirm
              title={t('生成 SSH Key')}
              description={t('将在服务器的 ~/.ssh 目录写入新密钥；同类型密钥已存在时不会覆盖。')}
              okText={t('生成')}
              cancelText={t('取消')}
              disabled={!access.canAdmin || sshKeyExists}
              onConfirm={generateSshKey}
            >
              <Button
                type="primary"
                loading={genLoading}
                disabled={!access.canAdmin || sshKeyExists}
              >
                {t('生成 SSH Key')}
              </Button>
            </Popconfirm>
          </Space>
        }
      >
        <Paragraph type="secondary" style={{ marginBottom: 12 }}>
          {t('这是部署进程所属用户的 SSH 公钥，用于在服务器上手工 git clone（如 gitee / github）。把公钥添加到对应平台的 SSH Key 后即可免密拉取；只读公钥，私钥不会返回。')}
        </Paragraph>
        {sshKey?.keys?.length ? (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            {sshKey.keys.map((k) => (
              <div key={k.type}>
                <Space wrap size="small">
                  <Tag color="purple">{k.type}</Tag>
                  <Text type="secondary">{k.keyPath}</Text>
                  {k.fingerprint ? (
                    <Text type="secondary">
                      {t('指纹')}: {k.fingerprint}
                    </Text>
                  ) : null}
                  <a onClick={() => copy(k.publicKey)}>{t('复制')}</a>
                </Space>
                <Paragraph
                  style={{
                    fontFamily: 'Menlo, Consolas, monospace',
                    wordBreak: 'break-all',
                    marginTop: 4,
                    marginBottom: 0,
                  }}
                >
                  {k.publicKey}
                </Paragraph>
              </div>
            ))}
          </Space>
        ) : (
          <Text type="secondary">{t('未找到 SSH 公钥')}</Text>
        )}
      </Card>
      <ProTable<Credential>
        rowKey="id"
        actionRef={actionRef}
        columns={columns}
        cardBordered
        search={false}
        options={{ density: false }}
        pagination={false}
        request={async () => {
          const list = await credentialApi.list();
          return { data: list || [], success: true };
        }}
        toolBarRender={() => [
          <Button key="create" type="primary" icon={<PlusOutlined />} onClick={openCreate}>
             {t('新建凭据')} </Button>,
        ]}
      />

      <Modal
        open={open}
        title={editing ? t('编辑凭据：{name}', { name: editing.name }) : t('新建凭据')}
        onCancel={() => setOpen(false)}
        onOk={submit}
        confirmLoading={saving}
        okText={t('保存')}
        cancelText={t('取消')}
        destroyOnClose
      >
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item name="name" label={t('名称')} rules={[{ required: true, message: t('请填写名称') }]}>
            <Input placeholder={t('例如 gitee-deploy')} />
          </Form.Item>
          <Form.Item name="type" label={t('类型')} rules={[{ required: true }]}>
            <Select options={CREDENTIAL_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item
            name="username"
            label={type === 'ssh_key' ? t('用户名（可选）') : t('用户名')}
            rules={
              type === 'https_token'
                ? [{ required: true, message: t('请填写 Git 用户名') }]
                : undefined
            }
            tooltip={type === 'ssh_key' ? t('SSH 方式一般填 git') : t('HTTPS 方式通常填 Git 平台的账号名')}
          >
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="secret"
            label={type === 'ssh_key' ? t('私钥内容') : t('Token / 密码')}
            rules={
              editing
                ? undefined
                : [{ required: true, message: t('请填写密文内容（编辑时留空表示不修改）') }]
            }
            tooltip={editing ? t('留空表示保持原值不变') : t('仅提交给服务端，前端与后续接口都不再返回')}
          >
            {type === 'ssh_key' ? (
              <Input.TextArea
                rows={6}
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                style={{ fontFamily: 'Menlo, Consolas, monospace' }}
              />
            ) : (
              <Input.Password placeholder={editing ? t('留空表示不修改') : t('例如 ghp_xxxx')} />
            )}
          </Form.Item>
          {type === 'ssh_key' ? (
            <Form.Item name="passphrase" label={t('私钥口令（可选）')} tooltip={t('留空表示私钥无口令')}>
              <Input.Password placeholder={editing ? t('留空表示不修改') : t('可选')} />
            </Form.Item>
          ) : null}
          {editing ? (
            <Space>
              <Button
                size="small"
                onClick={() => {
                  setTestUrl('');
                  setTestBranch('');
                  setTestOpen(true);
                }}
              >
                 {t('测试连接')} </Button>
              <Text type="secondary" style={{ fontSize: 12 }}>
                 {t('使用已保存的凭据内容执行 git ls-remote（表单里未保存的修改不会生效）')} </Text>
            </Space>
          ) : null}
        </Form>
      </Modal>

      <Modal
        open={testOpen}
        title={t('测试 Git 连接')}
        onCancel={() => setTestOpen(false)}
        onOk={testGit}
        confirmLoading={testing}
        okText={t('开始测试')}
        cancelText={t('取消')}
      >
        <Form layout="vertical">
          <Form.Item label={t('仓库地址')} required>
            <Input
              value={testUrl}
              onChange={(e) => setTestUrl(e.target.value)}
              placeholder="https://gitee.com/xxx/yyy.git"
            />
          </Form.Item>
          <Form.Item label={t('分支（可选）')}>
            <Input
              value={testBranch}
              onChange={(e) => setTestBranch(e.target.value)}
              placeholder="main"
            />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
};

export default Credentials;
