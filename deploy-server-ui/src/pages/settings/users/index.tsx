import { userApi } from '@/services/user';
import type { User } from '@/services/types';
import { formatTime } from '@/utils/constants';
import { PlusOutlined } from '@ant-design/icons';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { PageContainer, ProTable } from '@ant-design/pro-components';
import { useAccess, useModel } from '@umijs/max';
import {
  Button,
  Form,
  Input,
  Modal,
  Popconfirm,
  Result,
  Select,
  Space,
  Tag,
  message,
} from 'antd';
import React, { useRef, useState } from 'react';
import { t } from '@/i18n';

const ROLE_OPTIONS = [
  { label: t('管理员（admin）'), value: 'admin' },
  { label: t('普通用户（user）'), value: 'user' },
];

const STATUS_OPTIONS = [
  { label: t('正常（active）'), value: 'active' },
  { label: t('禁用（disabled）'), value: 'disabled' },
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
      <PageContainer title={t('用户管理')}>
        <Result status="403" title="403" subTitle={t('只有管理员可以访问用户管理。')} />
      </PageContainer>
    );
  }

  const createUser = async () => {
    const values = await createForm.validateFields();
    try {
      await userApi.create(values);
      messageApi.success(t('用户已创建'));
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
      messageApi.success(t('已保存'));
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
      messageApi.success(t('密码已重置'));
      setResetTarget(null);
      resetForm.resetFields();
    } catch (e) {
      // 统一提示
    }
  };

  const removeUser = async (id: number) => {
    try {
      await userApi.remove(id);
      messageApi.success(t('已删除'));
      actionRef.current?.reload();
    } catch (e) {
      // 统一提示
    }
  };

  const columns: ProColumns<User>[] = [
    {
      title: t('关键词'),
      dataIndex: 'keyword',
      hideInTable: true,
      fieldProps: { placeholder: t('用户名 / 昵称') },
    },
    {
      title: t('用户名'),
      dataIndex: 'username',
      hideInSearch: true,
      render: (_, row) => (
        <Space size={6}>
          <span>{row.username}</span>
          {row.id === initialState?.currentUser?.id ? <Tag color="blue">{t('自己')}</Tag> : null}
        </Space>
      ),
    },
    { title: t('昵称'), dataIndex: 'nickname', hideInSearch: true },
    { title: t('邮箱'), dataIndex: 'email', hideInSearch: true, render: (_, r) => r.email || '-' },
    {
      title: t('角色'),
      dataIndex: 'role',
      hideInSearch: true,
      width: 110,
      render: (_, row) =>
        row.role === 'admin' ? <Tag color="gold">{t('管理员')}</Tag> : <Tag>{t('普通用户')}</Tag>,
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      hideInSearch: true,
      width: 100,
      render: (_, row) =>
        row.status === 'active' ? <Tag color="green">{t('正常')}</Tag> : <Tag color="red">{t('禁用')}</Tag>,
    },
    {
      title: t('最近登录'),
      dataIndex: 'lastLoginAt',
      hideInSearch: true,
      width: 170,
      render: (_, row) => formatTime(row.lastLoginAt),
    },
    {
      title: t('操作'),
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
           {t('编辑')} </a>,
        <a
          key="reset"
          onClick={() => {
            setResetTarget(row);
            resetForm.resetFields();
          }}
        >
           {t('重置密码')} </a>,
        row.id === initialState?.currentUser?.id ? null : (
          <Popconfirm
            key="del"
            title={t('删除用户')}
            description={t('确认删除 {username}？', { username: row.username })}
            okText={t('删除')}
            okButtonProps={{ danger: true }}
            cancelText={t('取消')}
            onConfirm={() => removeUser(row.id)}
          >
            <a style={{ color: '#ff4d4f' }}>{t('删除')}</a>
          </Popconfirm>
        ),
      ],
    },
  ];

  return (
    <PageContainer title={t('用户管理')}>
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
             {t('新建用户')} </Button>,
        ]}
      />

      <Modal
        open={createOpen}
        title={t('新建用户')}
        onCancel={() => setCreateOpen(false)}
        onOk={createUser}
        okText={t('创建')}
        cancelText={t('取消')}
        destroyOnClose
      >
        <Form form={createForm} layout="vertical" preserve={false} initialValues={{ role: 'user' }}>
          <Form.Item
            name="username"
            label={t('用户名')}
            rules={[
              { required: true, message: t('请填写用户名') },
              { pattern: /^[a-zA-Z0-9_.-]{2,32}$/, message: t('2-32 位字母、数字、_ . -') },
            ]}
          >
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="password"
            label={t('初始密码')}
            rules={[
              { required: true, message: t('请填写初始密码') },
              { min: 8, message: t('密码长度至少 8 位') },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item name="nickname" label={t('昵称')}>
            <Input />
          </Form.Item>
          <Form.Item name="email" label={t('邮箱')} rules={[{ type: 'email', message: t('邮箱格式不正确') }]}>
            <Input />
          </Form.Item>
          <Form.Item name="role" label={t('角色')} rules={[{ required: true }]}>
            <Select options={ROLE_OPTIONS} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={!!editing}
        title={t('编辑用户：{p0}', { p0: editing?.username || '' })}
        onCancel={() => setEditing(null)}
        onOk={updateUser}
        okText={t('保存')}
        cancelText={t('取消')}
        destroyOnClose
      >
        <Form form={editForm} layout="vertical" preserve={false}>
          <Form.Item name="nickname" label={t('昵称')}>
            <Input />
          </Form.Item>
          <Form.Item name="email" label={t('邮箱')} rules={[{ type: 'email', message: t('邮箱格式不正确') }]}>
            <Input />
          </Form.Item>
          <Form.Item name="role" label={t('角色')} rules={[{ required: true }]}>
            <Select options={ROLE_OPTIONS} />
          </Form.Item>
          <Form.Item name="status" label={t('状态')} rules={[{ required: true }]}>
            <Select options={STATUS_OPTIONS} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={!!resetTarget}
        title={t('重置密码：{p0}', { p0: resetTarget?.username || '' })}
        onCancel={() => setResetTarget(null)}
        onOk={resetPassword}
        okText={t('重置')}
        cancelText={t('取消')}
        destroyOnClose
      >
        <Form form={resetForm} layout="vertical" preserve={false}>
          <Form.Item
            name="newPassword"
            label={t('新密码')}
            rules={[
              { required: true, message: t('请填写新密码') },
              { min: 8, message: t('密码长度至少 8 位') },
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
