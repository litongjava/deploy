import { t } from '@/i18n';
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
      messageApi.success(t('settings.credentials.sshPublicKeyGenerated'));
    } catch (e) {
      // 统一提示（同类型密钥已存在时后端返回 40900）
    } finally {
      setGenLoading(false);
    }
  };

  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      messageApi.success(t('common.copied'));
      return;
    } catch (e) {
      // 降级方案：clipboard API 在非 HTTPS 下不可用
      const el = document.createElement('textarea');
      el.value = text;
      document.body.appendChild(el);
      el.select();
      try {
        document.execCommand('copy');
        messageApi.success(t('common.copied'));
      } catch (err) {
        messageApi.error(t('common.copyFailed'));
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
        messageApi.success(t('common.saved'));
      } else {
        await credentialApi.create(values);
        messageApi.success(t('settings.credentials.credentialCreated'));
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
      messageApi.warning(t('settings.credentials.enterTheRepositoryURLFirst'));
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
        messageApi.success(
          t('settings.credentials.connectionSucceeded', {
            detail: res.message || t('settings.credentials.gitLsRemoteWorks'),
          }),
        );
        setTestOpen(false);
      } else {
        messageApi.error(
          t('settings.credentials.connectionFailed', {
            detail: res?.message || t('settings.credentials.unknownReason'),
          }),
        );
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
      messageApi.success(t('common.deleted'));
      actionRef.current?.reload();
    } catch (e) {
      // 统一提示（被引用时后端返回 40900）
    }
  };

  const columns: ProColumns<Credential>[] = [
    {
      title: t('common.name'),
      dataIndex: 'name',
      hideInSearch: true,
      render: (_, row) => <Text strong>{row.name}</Text>,
    },
    {
      title: t('common.type'),
      dataIndex: 'type',
      hideInSearch: true,
      width: 160,
      render: (_, row) =>
        row.type === 'ssh_key' ? (
          <Tag color="purple">{t('common.sshPrivateKey')}</Tag>
        ) : (
          <Tag color="blue">HTTPS Token</Tag>
        ),
    },
    {
      title: t('common.username'),
      dataIndex: 'username',
      hideInSearch: true,
      render: (_, row) => row.username || '-',
    },
    {
      title: t('common.createdAt'),
      dataIndex: 'createdAt',
      hideInSearch: true,
      width: 180,
      render: (_, row) => formatTime(row.createdAt),
    },
    {
      title: t('common.actions'),
      valueType: 'option',
      width: 140,
      render: (_, row) => [
        <a key="edit" onClick={() => openEdit(row)}>
          {t('common.edit')}{' '}
        </a>,
        <Popconfirm
          key="del"
          title={t('settings.credentials.deleteCredential')}
          description={t('settings.credentials.ifAProjectReferencesThis')}
          okText={t('common.delete')}
          okButtonProps={{ danger: true }}
          cancelText={t('common.cancel')}
          onConfirm={() => remove(row.id)}
        >
          <a style={{ color: '#ff4d4f' }}>{t('common.delete')}</a>
        </Popconfirm>,
      ],
    },
  ];

  return (
    <PageContainer title={t('settings.credentials.credentials')}>
      {contextHolder}
      <Alert
        style={{ marginBottom: 16 }}
        type="info"
        showIcon
        message={t('settings.credentials.privateRepoHint')}
        description={t('settings.credentials.encryptedHint')}
      />
      <Card
        size="small"
        style={{ marginBottom: 16 }}
        title={t('settings.credentials.serverSSHPublicKey')}
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
              title={t('settings.credentials.generateSSHKey')}
              description={t('settings.credentials.newKeyWillBeWritten')}
              okText={t('settings.credentials.generate')}
              cancelText={t('common.cancel')}
              disabled={!access.canAdmin || sshKeyExists}
              onConfirm={generateSshKey}
            >
              <Button
                type="primary"
                loading={genLoading}
                disabled={!access.canAdmin || sshKeyExists}
              >
                {t('settings.credentials.generateSSHKey')}
              </Button>
            </Popconfirm>
          </Space>
        }
      >
        <Paragraph type="secondary" style={{ marginBottom: 12 }}>
          {t('settings.credentials.sshPublicKeyHint')}
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
                      {t('settings.credentials.fingerprint')}: {k.fingerprint}
                    </Text>
                  ) : null}
                  <a onClick={() => copy(k.publicKey)}>{t('common.copy')}</a>
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
          <Text type="secondary">{t('settings.credentials.noSSHPublicKeyFound')}</Text>
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
            {t('settings.credentials.newCredential')}{' '}
          </Button>,
        ]}
      />

      <Modal
        open={open}
        title={
          editing
            ? t('settings.credentials.editCredential', { name: editing.name })
            : t('settings.credentials.newCredential')
        }
        onCancel={() => setOpen(false)}
        onOk={submit}
        confirmLoading={saving}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
        destroyOnClose
      >
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item
            name="name"
            label={t('common.name')}
            rules={[{ required: true, message: t('common.enterAName') }]}
          >
            <Input placeholder={t('settings.credentials.namePlaceholder')} />
          </Form.Item>
          <Form.Item name="type" label={t('common.type')} rules={[{ required: true }]}>
            <Select options={CREDENTIAL_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item
            name="username"
            label={
              type === 'ssh_key' ? t('settings.credentials.usernameOptional') : t('common.username')
            }
            rules={
              type === 'https_token'
                ? [{ required: true, message: t('settings.credentials.enterTheGitUsername') }]
                : undefined
            }
            tooltip={
              type === 'ssh_key'
                ? t('settings.credentials.sshUsernameHint')
                : t('settings.credentials.httpsUsernameHint')
            }
          >
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="secret"
            label={
              type === 'ssh_key'
                ? t('settings.credentials.privateKeyContent')
                : t('settings.credentials.tokenPassword')
            }
            rules={
              editing
                ? undefined
                : [{ required: true, message: t('settings.credentials.ciphertextHint') }]
            }
            tooltip={
              editing
                ? t('settings.credentials.leaveEmptyToKeepThe')
                : t('settings.credentials.sentToTheServerOnly')
            }
          >
            {type === 'ssh_key' ? (
              <Input.TextArea
                rows={6}
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                style={{ fontFamily: 'Menlo, Consolas, monospace' }}
              />
            ) : (
              <Input.Password
                placeholder={
                  editing
                    ? t('settings.credentials.leaveEmptyToKeepUnchanged')
                    : t('settings.credentials.tokenPlaceholder')
                }
              />
            )}
          </Form.Item>
          {type === 'ssh_key' ? (
            <Form.Item
              name="passphrase"
              label={t('settings.credentials.passphrase')}
              tooltip={t('settings.credentials.leaveEmptyIfThePrivate')}
            >
              <Input.Password
                placeholder={
                  editing
                    ? t('settings.credentials.leaveEmptyToKeepUnchanged')
                    : t('common.optional')
                }
              />
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
                {t('settings.credentials.testConnection')}{' '}
              </Button>
              <Text type="secondary" style={{ fontSize: 12 }}>
                {t('settings.credentials.testHint')}{' '}
              </Text>
            </Space>
          ) : null}
        </Form>
      </Modal>

      <Modal
        open={testOpen}
        title={t('settings.credentials.testGitConnection')}
        onCancel={() => setTestOpen(false)}
        onOk={testGit}
        confirmLoading={testing}
        okText={t('settings.credentials.startTest')}
        cancelText={t('common.cancel')}
      >
        <Form layout="vertical">
          <Form.Item label={t('common.repositoryURL')} required>
            <Input
              value={testUrl}
              onChange={(e) => setTestUrl(e.target.value)}
              placeholder="https://gitee.com/xxx/yyy.git"
            />
          </Form.Item>
          <Form.Item label={t('settings.credentials.branchOptional')}>
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
