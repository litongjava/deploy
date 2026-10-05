import EnvListEditor from '@/components/EnvListEditor';
import ScriptListEditor from '@/components/ScriptListEditor';
import { taskApi } from '@/services/task';
import type { EnvVar, FailStrategy, TaskInput, TaskStepDraft } from '@/services/types';
import {
  CONCURRENCY_OPTIONS,
  PROJECT_KEY_PATTERN,
  SECRET_PLACEHOLDER,
  SHELL_OPTIONS,
} from '@/utils/constants';
import { ArrowLeftOutlined, SaveOutlined } from '@ant-design/icons';
import {
  PageContainer,
  ProCard,
  ProForm,
  ProFormDigit,
  ProFormSelect,
  ProFormSwitch,
  ProFormText,
  ProFormTextArea,
} from '@ant-design/pro-components';
import { history, useParams } from '@umijs/max';
import { Button, Space, Spin, message } from 'antd';
import React, { useEffect, useRef, useState } from 'react';

/**
 * 新建 / 编辑任务。
 *
 * 与项目编辑页相比，刻意少了「项目类型与依赖构建」和「Git 准备阶段」两张卡片：
 * 任务不参与构建流水线，也不拉代码。其余（基本信息、环境变量、步骤列表）
 * 与项目完全同构，两个列表编辑器也是同一个组件。
 */

interface FormValues {
  key: string;
  name: string;
  description?: string;
  workdir: string;
  shell?: string;
  timeoutSec?: number;
  concurrency?: string;
  hookEnabled?: boolean;
  allowEnvOverride?: boolean;
  steps?: any[];
  envs?: any[];
}

const TasksEdit: React.FC = () => {
  const params = useParams<{ key?: string }>();
  const isEdit = !!params?.key;
  const editKey = params?.key;

  const [form] = ProForm.useForm<FormValues>();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [messageApi, contextHolder] = message.useMessage();
  // Secret 变量的原值：编辑时后端只回空串，前端显示占位符，
  // 提交时若仍是占位符就沿用原值，避免「没改也被清空」。
  const originalEnvsRef = useRef<EnvVar[]>([]);

  useEffect(() => {
    if (!isEdit) return;
    let cancelled = false;
    (async () => {
      setLoading(true);
      try {
        const task = await taskApi.detail(editKey!);
        if (cancelled) return;
        originalEnvsRef.current = task.envs || [];
        form.setFieldsValue({
          key: task.key,
          name: task.name,
          description: task.description,
          workdir: task.workdir,
          shell: task.shell,
          timeoutSec: task.timeoutSec,
          concurrency: task.concurrency,
          hookEnabled: task.hookEnabled,
          allowEnvOverride: task.allowEnvOverride,
          steps: (task.steps || []).map((s) => ({ ...s })),
          envs: (task.envs || []).map((e) => ({
            ...e,
            value: e.isSecret ? SECRET_PLACEHOLDER : e.value,
          })),
        });
      } catch (e) {
        // 统一错误提示
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [editKey, isEdit, form]);

  const handleSubmit = async (values: FormValues) => {
    setSaving(true);
    try {
      // 步骤按当前顺序重排 sortOrder；空行（只点了新增）直接丢掉。
      const steps: TaskStepDraft[] = (values.steps || [])
        .filter((s) => s && (s.name || s.script))
        .map((s, index) => ({
          name: s.name || `步骤 ${index + 1}`,
          script: s.script || '',
          sortOrder: index + 1,
          enabled: s.enabled !== false,
          failStrategy: (s.failStrategy || 'abort') as FailStrategy,
          workdirOverride: s.workdirOverride || '',
          timeoutSec: Number(s.timeoutSec || 0),
          shellOverride: s.shellOverride || '',
        }));

      // Secret 变量若仍是占位符则沿用原密文（服务端按 key 匹配）。
      const envs: EnvVar[] = (values.envs || [])
        .filter((e) => e && e.key)
        .map((e) => {
          let value: string = e.value ?? '';
          if (e.isSecret && value === SECRET_PLACEHOLDER) {
            const original = originalEnvsRef.current.find((o) => o.key === e.key);
            value = original?.value ?? SECRET_PLACEHOLDER;
          }
          return { key: e.key, value, isSecret: !!e.isSecret };
        });

      const payload: TaskInput = {
        key: values.key,
        name: values.name,
        description: values.description || '',
        workdir: values.workdir,
        shell: values.shell,
        timeoutSec: Number(values.timeoutSec || 0),
        concurrency: values.concurrency,
        hookEnabled: values.hookEnabled !== false,
        allowEnvOverride: values.allowEnvOverride !== false,
        steps,
        envs,
      };

      if (isEdit) {
        await taskApi.update(editKey!, payload);
        messageApi.success('保存成功');
        history.push(`/tasks/${editKey}`);
      } else {
        await taskApi.create(payload);
        messageApi.success('创建成功');
        history.push(`/tasks/${payload.key}`);
      }
    } catch (e) {
      // 统一错误提示
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <PageContainer title={isEdit ? `编辑任务 ${editKey}` : '新建任务'}>
        <div style={{ textAlign: 'center', padding: '60px 0' }}>
          <Spin />
        </div>
      </PageContainer>
    );
  }

  return (
    <PageContainer
      title={isEdit ? `编辑任务：${editKey}` : '新建任务'}
      onBack={() => history.back()}
      extra={[
        <Button key="back" icon={<ArrowLeftOutlined />} onClick={() => history.back()}>
          返回
        </Button>,
      ]}
    >
      {contextHolder}
      <ProForm<FormValues>
        form={form}
        submitter={{
          searchConfig: { submitText: isEdit ? '保存' : '创建任务' },
          render: (props) => (
            <Space>
              <Button
                type="primary"
                icon={<SaveOutlined />}
                loading={saving}
                onClick={() => props.form?.submit()}
              >
                {isEdit ? '保存' : '创建任务'}
              </Button>
              <Button onClick={() => history.back()}>取消</Button>
            </Space>
          ),
        }}
        onFinish={handleSubmit}
        initialValues={{
          shell: 'auto',
          timeoutSec: 0,
          concurrency: 'queue',
          hookEnabled: true,
          allowEnvOverride: true,
          steps: [],
          envs: [],
        }}
      >
        <ProCard title="基本信息" bordered headerBordered style={{ marginBottom: 16 }}>
          <ProFormText
            name="key"
            label="任务 Key"
            disabled={isEdit}
            rules={[
              { required: true, message: '请填写任务 Key' },
              { pattern: PROJECT_KEY_PATTERN, message: '只能包含字母、数字、- 和 _，且不以符号开头' },
            ]}
            placeholder="例如 bench-tio-boot（字母数字与 - _）"
            tooltip="任务 Key 会作为工作目录占位符 ${TASK_KEY} 的值，创建后不可修改"
          />
          <ProFormText
            name="name"
            label="任务名称"
            rules={[{ required: true, message: '请填写任务名称' }]}
            placeholder="例如：tio-boot 压测"
          />
          <ProFormTextArea
            name="description"
            label="描述"
            placeholder="这个任务是干什么的、什么时候该跑"
            fieldProps={{ rows: 2 }}
          />
          <ProFormText
            name="workdir"
            label="工作目录"
            rules={[{ required: true, message: '请填写工作目录' }]}
            placeholder="例如 /srv/deploy/workspaces/bench"
            tooltip="支持 ${WORKSPACE_ROOT} / ${TASK_KEY} / ${TASK_NAME} 占位符；目录不存在会自动创建"
          />
          <ProFormSelect name="shell" label="Shell" options={SHELL_OPTIONS} />
          <ProFormDigit
            name="timeoutSec"
            label="默认超时（秒）"
            min={0}
            tooltip="0 表示不限制；每一步也可以单独设超时"
          />
          <ProFormSelect name="concurrency" label="并发策略" options={CONCURRENCY_OPTIONS} />
          <ProFormSwitch name="hookEnabled" label="启用 Hook 触发" />
          <ProFormSwitch
            name="allowEnvOverride"
            label="允许触发时覆盖环境变量"
            tooltip="通过 Hook 或接口触发时，请求体里的 env 是否能覆盖任务自身与环境变量"
          />
        </ProCard>

        <ProCard title="环境变量" bordered headerBordered style={{ marginBottom: 16 }}>
          <EnvListEditor />
        </ProCard>

        <ProCard title="步骤列表（按顺序执行）" bordered headerBordered>
          <ScriptListEditor listName="steps" itemNoun="步骤" ownerNoun="任务" />
        </ProCard>
      </ProForm>
    </PageContainer>
  );
};

export default TasksEdit;
