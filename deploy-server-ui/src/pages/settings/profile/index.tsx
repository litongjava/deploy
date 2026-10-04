import { authApi } from '@/services/auth';
import { tokenApi } from '@/services/token';
import type { ApiToken, ApiTokenCreated } from '@/services/types';
import { formatTime } from '@/utils/constants';
import { CopyOutlined, PlusOutlined } from '@ant-design/icons';
import {
  PageContainer,
  ProForm,
  ProFormText,
} from '@ant-design/pro-components';
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

  const copy = async (text: string, tip = '已复制') => {
    try {
      await navigator.clipboard.writeText(text);
      messageApi.success(tip);
    } catch (e) {
      messageApi.error('复制失败，请手动选择文本');
    }
  };

  const changePassword = async (values: any) => {
    setSaving(true);
    try {
      await authApi.changePassword({
        oldPassword: values.oldPassword,
        newPassword: values.newPassword,
      });
      messageApi.success('密码修改成功');
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
      messageApi.success('已删除');
      loadTokens();
    } catch (e) {
      // 统一提示
    }
  };

  return (
    <PageContainer title="个人设置">
      {contextHolder}

      {currentUser?.mustChangePassword ? (
        <Alert
          style={{ marginBottom: 16 }}
          type="warning"
          showIcon
          message="请先修改初始密码"
          description="管理员要求首次登录后修改密码，修改完成前部分功能会被限制。"
        />
      ) : null}

      <Card title="账号信息" style={{ marginBottom: 16 }}>
        <Descriptions column={{ xs: 1, sm: 2 }} size="small" bordered>
          <Descriptions.Item label="用户名">{currentUser?.username || '-'}</Descriptions.Item>
          <Descriptions.Item label="昵称">{currentUser?.nickname || '-'}</Descriptions.Item>
          <Descriptions.Item label="角色">
            {currentUser?.role === 'admin' ? <Tag color="gold">管理员</Tag> : <Tag>普通用户</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="邮箱">{currentUser?.email || '-'}</Descriptions.Item>
          <Descriptions.Item label="最近登录">
            {formatTime(currentUser?.lastLoginAt)}
          </Descriptions.Item>
          <Descriptions.Item label="创建时间">
            {formatTime(currentUser?.createdAt)}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title="修改密码" style={{ marginBottom: 16 }}>
        <ProForm
          form={pwdForm}
          style={{ maxWidth: 420 }}
          submitter={{
            searchConfig: { submitText: '修改密码' },
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
            label="当前密码"
            rules={[{ required: true, message: '请输入当前密码' }]}
          />
          <ProFormText.Password
            name="newPassword"
            label="新密码"
            tooltip="长度至少 8 位"
            rules={[
              { required: true, message: '请输入新密码' },
              { min: 8, message: '新密码长度至少 8 位' },
            ]}
          />
          <ProFormText.Password
            name="confirmPassword"
            label="确认新密码"
            dependencies={['newPassword']}
            rules={[
              { required: true, message: '请再次输入新密码' },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('newPassword') === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error('两次输入的密码不一致'));
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
            新建 Token
          </Button>
        }
      >
        <Alert
          style={{ marginBottom: 12 }}
          type="info"
          showIcon
          message="用于 CLI / 第三方调用 REST API"
          description={
            <span>
              请求头携带 <Text code>Authorization: dk_xxx</Text>，与登录 JWT 等价；
              服务端只保存哈希，明文仅在创建时显示一次。
            </span>
          }
        />
        <Table<ApiToken>
          rowKey="id"
          size="small"
          loading={loading}
          pagination={false}
          dataSource={tokens}
          locale={{ emptyText: <Empty description="还没有 API Token" /> }}
          columns={[
            { title: '名称', dataIndex: 'name' },
            { title: '前缀', dataIndex: 'prefix', render: (v) => <Text code>{v}…</Text> },
            {
              title: '过期时间',
              dataIndex: 'expiresAt',
              render: (v) => (v ? formatTime(v) : '永不过期'),
            },
            {
              title: '最后使用',
              dataIndex: 'lastUsedAt',
              render: (v) => (v ? formatTime(v) : <Text type="secondary">从未使用</Text>),
            },
            { title: '创建时间', dataIndex: 'createdAt', render: (v) => formatTime(v) },
            {
              title: '操作',
              width: 90,
              render: (_, row) => (
                <Popconfirm
                  title="删除后使用该 Token 的调用方会立即失效"
                  onConfirm={() => removeToken(row.id)}
                  okText="删除"
                  okButtonProps={{ danger: true }}
                  cancelText="取消"
                >
                  <a style={{ color: '#ff4d4f' }}>删除</a>
                </Popconfirm>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        open={!!plain}
        title="API Token 已创建（明文仅显示这一次）"
        onCancel={() => setPlain(null)}
        footer={[
          <Button key="ok" type="primary" onClick={() => setPlain(null)}>
            我已保存
          </Button>,
        ]}
        width={620}
      >
        <Alert
          style={{ marginBottom: 12 }}
          type="warning"
          showIcon
          message="请立即复制保存"
          description="关闭后无法再次查看明文，只能删除后重新创建。"
        />
        {plain ? (
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="名称">{plain.name}</Descriptions.Item>
            <Descriptions.Item label="明文 Token">
              <Space>
                <Text code copyable style={{ wordBreak: 'break-all' }}>
                  {plain.token}
                </Text>
                <Button
                  size="small"
                  icon={<CopyOutlined />}
                  onClick={() => copy(plain.token, 'Token 已复制')}
                >
                  复制
                </Button>
              </Space>
            </Descriptions.Item>
          </Descriptions>
        ) : null}
      </Modal>

      <Modal
        open={createOpen}
        title="新建 API Token"
        onCancel={() => setCreateOpen(false)}
        onOk={createToken}
        okText="创建"
        cancelText="取消"
        destroyOnClose
      >
        <Form form={tokenForm} layout="vertical" preserve={false}>
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: '请填写名称' }]}
          >
            <Input placeholder="例如：本地 CLI / GitHub Actions" />
          </Form.Item>
          <Form.Item name="expiresInDays" label="有效天数" tooltip="留空表示永不过期">
            <InputNumber min={1} style={{ width: '100%' }} placeholder="例如 90" />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
};

export default Profile;
