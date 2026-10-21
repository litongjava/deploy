import LanguageSwitcher from '@/components/LanguageSwitcher';
import { t } from '@/i18n';
import { authApi } from '@/services/auth';
import { tokenApi } from '@/services/token';
import type { ApiToken, ApiTokenCreated } from '@/services/types';
import { formatTime } from '@/utils/constants';
import { CopyOutlined, PlusOutlined } from '@ant-design/icons';
import { PageContainer, ProForm, ProFormText } from '@ant-design/pro-components';
import { useModel } from '@umijs/max';
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import React, { useCallback, useEffect, useState } from 'react';

const { Text } = Typography;

const Profile: React.FC = () => {
  const { initialState, setInitialState } = useModel('@@initialState');
  const currentUser = initialState?.currentUser;

  const [tokens, setTokens] = useState<ApiToken[]>([]);
  const [loading, setLoading] = useState(false);
  const [plain, setPlain] = useState<ApiTokenCreated | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [messageApi, contextHolder] = message.useMessage();
  const [tokenForm] = Form.useForm();
  const [pwdForm] = Form.useForm();

  const loadTokens = useCallback(async () => {
    setLoading(true);
    try {
      const list = await tokenApi.list();
      setTokens(list || []);
    } catch (e) {
      setTokens([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadTokens();
  }, [loadTokens]);

  const copy = async (text: string, tip = t('common.copied')) => {
    try {
      await navigator.clipboard.writeText(text);
      messageApi.success(tip);
    } catch (e) {
      messageApi.error(t('common.copyFailed'));
    }
  };

  const changePassword = async (values: any) => {
    setSaving(true);
    try {
      await authApi.changePassword({
        oldPassword: values.oldPassword,
        newPassword: values.newPassword,
      });
      messageApi.success(t('settings.profile.passwordChanged'));
      pwdForm.resetFields();
      // 强制改密标记已在服务端清掉，刷新一下当前用户
      const me = await initialState?.fetchUserInfo?.();
      if (me) {
        setInitialState((s) => ({ ...s, currentUser: me }));
      }
    } catch (e) {
      // 统一提示
    } finally {
      setSaving(false);
    }
  };

  const createToken = async () => {
    const values = await tokenForm.validateFields();
    try {
      const res = await tokenApi.create({
        name: values.name,
        expiresInDays: values.expiresInDays || undefined,
      });
      setCreateOpen(false);
      tokenForm.resetFields();
      setPlain(res);
      loadTokens();
    } catch (e) {
      // 统一提示
    }
  };

  const removeToken = async (id: number) => {
    try {
      await tokenApi.remove(id);
      messageApi.success(t('common.deleted'));
      loadTokens();
    } catch (e) {
      // 统一提示
    }
  };

  return (
    <PageContainer title={t('common.profile')}>
      {contextHolder}

      {currentUser?.mustChangePassword ? (
        <Alert
          style={{ marginBottom: 16 }}
          type="warning"
          showIcon
          message={t('settings.profile.pleaseChangeTheInitialPassword')}
          description={t('settings.profile.mustChangePassword')}
        />
      ) : null}

      <Card title={t('settings.profile.accountInformation')} style={{ marginBottom: 16 }}>
        <Descriptions column={{ xs: 1, sm: 2 }} size="small" bordered>
          <Descriptions.Item label={t('common.username')}>
            {currentUser?.username || '-'}
          </Descriptions.Item>
          <Descriptions.Item label={t('common.nickname')}>
            {currentUser?.nickname || '-'}
          </Descriptions.Item>
          <Descriptions.Item label={t('common.role')}>
            {currentUser?.role === 'admin' ? (
              <Tag color="gold">{t('common.administrator')}</Tag>
            ) : (
              <Tag>{t('common.user')}</Tag>
            )}
          </Descriptions.Item>
          <Descriptions.Item label={t('common.email')}>
            {currentUser?.email || '-'}
          </Descriptions.Item>
          <Descriptions.Item label={t('common.lastSignIn')}>
            {formatTime(currentUser?.lastLoginAt)}
          </Descriptions.Item>
          <Descriptions.Item label={t('common.createdAt')}>
            {formatTime(currentUser?.createdAt)}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title={t('settings.profile.interfaceLanguage')} style={{ marginBottom: 16 }}>
        <Space direction="vertical" size={8}>
          <LanguageSwitcher />
          <Text type="secondary" style={{ fontSize: 12 }}>
            {t('settings.profile.pageReloadsAfterSwitchingThe')}
          </Text>
        </Space>
      </Card>

      <Card title={t('settings.profile.changePassword')} style={{ marginBottom: 16 }}>
        <ProForm
          form={pwdForm}
          style={{ maxWidth: 420 }}
          submitter={{
            searchConfig: { submitText: t('settings.profile.changePassword') },
            resetButtonProps: { style: { display: 'none' } },
            submitButtonProps: { loading: saving },
          }}
          onFinish={async (values) => {
            await changePassword(values);
            return true;
          }}
        >
          <ProFormText.Password
            name="oldPassword"
            label={t('settings.profile.currentPassword')}
            rules={[{ required: true, message: t('settings.profile.enterTheCurrentPassword') }]}
          />
          <ProFormText.Password
            name="newPassword"
            label={t('common.newPassword')}
            tooltip={t('settings.profile.least8Characters')}
            rules={[
              { required: true, message: t('settings.profile.enterTheNewPassword') },
              { min: 8, message: t('settings.profile.newPasswordMustBeAt') },
            ]}
          />
          <ProFormText.Password
            name="confirmPassword"
            label={t('settings.profile.confirmNewPassword')}
            dependencies={['newPassword']}
            rules={[
              { required: true, message: t('settings.profile.enterTheNewPasswordAgain') },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('newPassword') === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error(t('common.twoPasswordsDoNotMatch')));
                },
              }),
            ]}
          />
        </ProForm>
      </Card>

      <Card
        title="API Token"
        extra={
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
            {t('settings.profile.newToken')}{' '}
          </Button>
        }
      >
        <Alert
          style={{ marginBottom: 12 }}
          type="info"
          showIcon
          message={t('settings.profile.tokenUsageHint')}
          description={
            <span>
              {t('settings.profile.sendInTheRequestHeader')} <Text code>Authorization: dk_xxx</Text>
              {t('settings.profile.equivalentToTheLoginJWT')}
            </span>
          }
        />
        <Table<ApiToken>
          rowKey="id"
          size="small"
          loading={loading}
          pagination={false}
          dataSource={tokens}
          locale={{ emptyText: <Empty description={t('settings.profile.noAPITokenYet')} /> }}
          columns={[
            { title: t('common.name'), dataIndex: 'name' },
            {
              title: t('common.prefix'),
              dataIndex: 'prefix',
              render: (v) => <Text code>{v}…</Text>,
            },
            {
              title: t('common.expiresAt'),
              dataIndex: 'expiresAt',
              render: (v) => (v ? formatTime(v) : t('common.neverExpires')),
            },
            {
              title: t('common.lastUsed'),
              dataIndex: 'lastUsedAt',
              render: (v) =>
                v ? formatTime(v) : <Text type="secondary">{t('common.neverUsed')}</Text>,
            },
            { title: t('common.createdAt'), dataIndex: 'createdAt', render: (v) => formatTime(v) },
            {
              title: t('common.actions'),
              width: 90,
              render: (_, row) => (
                <Popconfirm
                  title={t('settings.profile.callersUsingThisTokenFail')}
                  onConfirm={() => removeToken(row.id)}
                  okText={t('common.delete')}
                  okButtonProps={{ danger: true }}
                  cancelText={t('common.cancel')}
                >
                  <a style={{ color: '#ff4d4f' }}>{t('common.delete')}</a>
                </Popconfirm>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        open={!!plain}
        title={t('settings.profile.APITokenCreatedPlaintextShown')}
        onCancel={() => setPlain(null)}
        footer={[
          <Button key="ok" type="primary" onClick={() => setPlain(null)}>
            {t('common.iHaveSavedIt')}{' '}
          </Button>,
        ]}
        width={620}
      >
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message={t('common.copyAndSaveItNow')}
          description={t('settings.profile.plaintextCannotBeViewedAgain')}
        />
        {plain ? (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label={t('common.name')}>{plain.name}</Descriptions.Item>
            <Descriptions.Item label={t('common.plaintextToken')}>
              <Space>
                <Text code copyable style={{ wordBreak: 'break-all' }}>
                  {plain.token}
                </Text>
                <Button
                  size="small"
                  icon={<CopyOutlined />}
                  onClick={() => copy(plain.token, t('common.tokenCopied'))}
                >
                  {t('common.copy')}{' '}
                </Button>
              </Space>
            </Descriptions.Item>
          </Descriptions>
        ) : null}
      </Modal>

      <Modal
        open={createOpen}
        title={t('settings.profile.newAPIToken')}
        onCancel={() => setCreateOpen(false)}
        onOk={createToken}
        okText={t('common.create')}
        cancelText={t('common.cancel')}
        destroyOnClose
      >
        <Form form={tokenForm} layout="vertical" preserve={false}>
          <Form.Item
            name="name"
            label={t('common.name')}
            rules={[{ required: true, message: t('common.enterAName') }]}
          >
            <Input placeholder={t('settings.profile.eGLocalCLIGithub')} />
          </Form.Item>
          <Form.Item
            name="expiresInDays"
            label={t('common.validForDays')}
            tooltip={t('common.leaveEmptyForNoExpiry')}
          >
            <InputNumber min={1} style={{ width: '100%' }} placeholder={t('common.example90')} />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
};

export default Profile;
