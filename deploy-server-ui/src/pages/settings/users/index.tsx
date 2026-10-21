import { t } from '@/i18n';
import type { User } from '@/services/types';
import { userApi } from '@/services/user';
import { formatTime } from '@/utils/constants';
import { PlusOutlined } from '@ant-design/icons';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PageContainer, ProTable } from '@ant-design/pro-components';
import { useAccess, useModel } from '@umijs/max';
import { Button, Form, Input, Modal, Popconfirm, Result, Select, Space, Tag, message } from 'antd';
import React, { useRef, useState } from 'react';

const ROLE_OPTIONS = [
  { label: t('settings.users.statusAdmin'), value: 'admin' },
  { label: t('settings.users.statusUser'), value: 'user' },
];

const STATUS_OPTIONS = [
  { label: t('settings.users.statusActive'), value: 'active' },
  { label: t('settings.users.statusDisabled'), value: 'disabled' },
];

const Users: React.FC = () => {
  const access = useAccess();
  const { initialState } = useModel('@@initialState');
  const actionRef = useRef<ActionType>();
  const [messageApi, contextHolder] = message.useMessage();
  const [editing, setEditing] = useState<User | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [resetTarget, setResetTarget] = useState<User | null>(null);
  const [createForm] = Form.useForm();
  const [editForm] = Form.useForm();
  const [resetForm] = Form.useForm();

  if (!access.canAdmin) {
    return (
      <PageContainer title={t('settings.users.userManagement')}>
        <Result
          status="403"
          title="403"
          subTitle={t('settings.users.onlyAdministratorsCanAccessUser')}
        />
      </PageContainer>
    );
  }

  const createUser = async () => {
    const values = await createForm.validateFields();
    try {
      await userApi.create(values);
      messageApi.success(t('settings.users.userCreated'));
      setCreateOpen(false);
      createForm.resetFields();
      actionRef.current?.reload();
    } catch (e) {
      // 统一提示
    }
  };

  const updateUser = async () => {
    const values = await editForm.validateFields();
    try {
      await userApi.update(editing!.id, values);
      messageApi.success(t('common.saved'));
      setEditing(null);
      actionRef.current?.reload();
    } catch (e) {
      // 统一提示
    }
  };

  const resetPassword = async () => {
    const values = await resetForm.validateFields();
    try {
      await userApi.resetPassword(resetTarget!.id, values.newPassword);
      messageApi.success(t('settings.users.passwordReset'));
      setResetTarget(null);
      resetForm.resetFields();
    } catch (e) {
      // 统一提示
    }
  };

  const removeUser = async (id: number) => {
    try {
      await userApi.remove(id);
      messageApi.success(t('common.deleted'));
      actionRef.current?.reload();
    } catch (e) {
      // 统一提示
    }
  };

  const columns: ProColumns<User>[] = [
    {
      title: t('settings.users.keyword'),
      dataIndex: 'keyword',
      hideInTable: true,
      fieldProps: { placeholder: t('settings.users.usernameNickname') },
    },
    {
      title: t('common.username'),
      dataIndex: 'username',
      hideInSearch: true,
      render: (_, row) => (
        <Space size={6}>
          <span>{row.username}</span>
          {row.id === initialState?.currentUser?.id ? (
            <Tag color="blue">{t('settings.users.self')}</Tag>
          ) : null}
        </Space>
      ),
    },
    { title: t('common.nickname'), dataIndex: 'nickname', hideInSearch: true },
    {
      title: t('common.email'),
      dataIndex: 'email',
      hideInSearch: true,
      render: (_, r) => r.email || '-',
    },
    {
      title: t('common.role'),
      dataIndex: 'role',
      hideInSearch: true,
      width: 110,
      render: (_, row) =>
        row.role === 'admin' ? (
          <Tag color="gold">{t('common.administrator')}</Tag>
        ) : (
          <Tag>{t('common.user')}</Tag>
        ),
    },
    {
      title: t('common.status'),
      dataIndex: 'status',
      hideInSearch: true,
      width: 100,
      render: (_, row) =>
        row.status === 'active' ? (
          <Tag color="green">{t('common.active')}</Tag>
        ) : (
          <Tag color="red">{t('common.disable')}</Tag>
        ),
    },
    {
      title: t('common.lastSignIn'),
      dataIndex: 'lastLoginAt',
      hideInSearch: true,
      width: 170,
      render: (_, row) => formatTime(row.lastLoginAt),
    },
    {
      title: t('common.actions'),
      valueType: 'option',
      width: 200,
      fixed: 'right',
      render: (_, row) => [
        <a
          key="edit"
          onClick={() => {
            setEditing(row);
            editForm.setFieldsValue({
              nickname: row.nickname,
              email: row.email,
              role: row.role,
              status: row.status,
            });
          }}
        >
          {t('common.edit')}{' '}
        </a>,
        <a
          key="reset"
          onClick={() => {
            setResetTarget(row);
            resetForm.resetFields();
          }}
        >
          {t('settings.users.resetPassword')}{' '}
        </a>,
        row.id === initialState?.currentUser?.id ? null : (
          <Popconfirm
            key="del"
            title={t('settings.users.deleteUser')}
            description={t('settings.users.delete', { username: row.username })}
            okText={t('common.delete')}
            okButtonProps={{ danger: true }}
            cancelText={t('common.cancel')}
            onConfirm={() => removeUser(row.id)}
          >
            <a style={{ color: '#ff4d4f' }}>{t('common.delete')}</a>
          </Popconfirm>
        ),
      ],
    },
  ];

  return (
    <PageContainer title={t('settings.users.userManagement')}>
      {contextHolder}
      <ProTable<User>
        rowKey="id"
        actionRef={actionRef}
        columns={columns}
        cardBordered
        search={{ labelWidth: 'auto' }}
        options={{ density: false }}
        pagination={{ pageSize: 20, showSizeChanger: true }}
        request={async (params) => {
          const { current, pageSize, keyword } = params as any;
          const res = await userApi.list({
            page: current || 1,
            pageSize: pageSize || 20,
            keyword: keyword || undefined,
          });
          return { data: res?.list || [], total: res?.total || 0, success: true };
        }}
        toolBarRender={() => [
          <Button
            key="create"
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => setCreateOpen(true)}
          >
            {t('settings.users.newUser')}{' '}
          </Button>,
        ]}
      />

      <Modal
        open={createOpen}
        title={t('settings.users.newUser')}
        onCancel={() => setCreateOpen(false)}
        onOk={createUser}
        okText={t('common.create')}
        cancelText={t('common.cancel')}
        destroyOnClose
      >
        <Form form={createForm} layout="vertical" preserve={false} initialValues={{ role: 'user' }}>
          <Form.Item
            name="username"
            label={t('common.username')}
            rules={[
              { required: true, message: t('settings.users.enterTheUsername') },
              { pattern: /^[a-zA-Z0-9_.-]{2,32}$/, message: t('common.usernameRule') },
            ]}
          >
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="password"
            label={t('settings.users.initialPassword')}
            rules={[
              { required: true, message: t('settings.users.enterTheInitialPassword') },
              { min: 8, message: t('common.passwordMin8') },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item name="nickname" label={t('common.nickname')}>
            <Input />
          </Form.Item>
          <Form.Item
            name="email"
            label={t('common.email')}
            rules={[{ type: 'email', message: t('settings.users.invalidEmailFormat') }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="role" label={t('common.role')} rules={[{ required: true }]}>
            <Select options={ROLE_OPTIONS} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={!!editing}
        title={t('settings.users.editUser', { p0: editing?.username || '' })}
        onCancel={() => setEditing(null)}
        onOk={updateUser}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
        destroyOnClose
      >
        <Form form={editForm} layout="vertical" preserve={false}>
          <Form.Item name="nickname" label={t('common.nickname')}>
            <Input />
          </Form.Item>
          <Form.Item
            name="email"
            label={t('common.email')}
            rules={[{ type: 'email', message: t('settings.users.invalidEmailFormat') }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="role" label={t('common.role')} rules={[{ required: true }]}>
            <Select options={ROLE_OPTIONS} />
          </Form.Item>
          <Form.Item name="status" label={t('common.status')} rules={[{ required: true }]}>
            <Select options={STATUS_OPTIONS} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={!!resetTarget}
        title={t('settings.users.resetPasswordTitle', { p0: resetTarget?.username || '' })}
        onCancel={() => setResetTarget(null)}
        onOk={resetPassword}
        okText={t('common.reset')}
        cancelText={t('common.cancel')}
        destroyOnClose
      >
        <Form form={resetForm} layout="vertical" preserve={false}>
          <Form.Item
            name="newPassword"
            label={t('common.newPassword')}
            rules={[
              { required: true, message: t('settings.users.enterTheNewPassword') },
              { min: 8, message: t('common.passwordMin8') },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
};

export default Users;
