import EnvListEditor from '@/components/EnvListEditor';
import ScriptListEditor from '@/components/ScriptListEditor';
import { t } from '@/i18n';
import { taskApi } from '@/services/task';
import type { EnvVar, FailStrategy, TaskInput, TaskStepDraft } from '@/services/types';
import {
  CONCURRENCY_OPTIONS,
  PROJECT_KEY_PATTERN,
  SECRET_PLACEHOLDER,
  SHELL_OPTIONS,
  defaultWorkdir,
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
import { Button, Col, Row, Space, Spin, message } from 'antd';
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
  const keyValue = ProForm.useWatch('key', form);

  /**
   * 「工作目录 / 项目名称」是否被用户手动填过。
   *
   * 没手动填过时它们跟着「任务 Key」自动生成（工作目录 = `/srv/workspaces/<key>`，
   * 名称 = Key，只在新建时生效）；用户一旦自己填了内容就不再覆盖，清空后恢复自动生成。
   */
  const workdirEditedRef = useRef(false);
  const nameEditedRef = useRef(false);

  useEffect(() => {
    if (isEdit) return;
    const patch: Partial<FormValues> = {};
    if (!workdirEditedRef.current) patch.workdir = defaultWorkdir(keyValue);
    if (!nameEditedRef.current) patch.name = (keyValue ?? '').trim();
    if (Object.keys(patch).length > 0) form.setFieldsValue(patch);
  }, [form, isEdit, keyValue]);

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
          name: s.name || t('task.step', { p0: index + 1 }),
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
        messageApi.success(t('common.saveSuccess'));
        history.push(`/tasks/${editKey}`);
      } else {
        await taskApi.create(payload);
        messageApi.success(t('common.created'));
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
      <PageContainer title={isEdit ? t('task.editTitle', { editKey: editKey }) : t('task.newTask')}>
        <div style={{ textAlign: 'center', padding: '60px 0' }}>
          <Spin />
        </div>
      </PageContainer>
    );
  }

  return (
    <PageContainer
      title={isEdit ? t('task.editTitleColon', { editKey: editKey }) : t('task.newTask')}
      onBack={() => history.back()}
      extra={[
        <Button key="back" icon={<ArrowLeftOutlined />} onClick={() => history.back()}>
          {t('common.back')}{' '}
        </Button>,
      ]}
    >
      {contextHolder}
      <ProForm<FormValues>
        form={form}
        submitter={{
          searchConfig: { submitText: isEdit ? t('common.save') : t('task.createTask') },
          render: (props) => (
            <Space>
              <Button
                type="primary"
                icon={<SaveOutlined />}
                loading={saving}
                onClick={() => props.form?.submit()}
              >
                {isEdit ? t('common.save') : t('task.createTask')}
              </Button>
              <Button onClick={() => history.back()}>{t('common.cancel')}</Button>
            </Space>
          ),
        }}
        onFinish={handleSubmit}
        onValuesChange={(changed) => {
          // 只有用户自己改过的字段才算手动值；上面的 setFieldsValue 不会触发这里。
          if (Object.prototype.hasOwnProperty.call(changed, 'workdir')) {
            workdirEditedRef.current = !!String(changed.workdir ?? '').trim();
          }
          if (Object.prototype.hasOwnProperty.call(changed, 'name')) {
            nameEditedRef.current = !!String(changed.name ?? '').trim();
          }
        }}
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
        <ProCard
          title={t('common.basicInformation')}
          bordered
          headerBordered
          style={{ marginBottom: 16 }}
        >
          {/* 与项目表单同一套栅格：PC（≥1200px）一行三个、平板（≥768px）一行两个、手机一列一个 */}
          <Row gutter={24}>
            <Col xs={24} md={12} xl={8}>
              <ProFormText
                name="key"
                label={t('task.taskKey')}
                disabled={isEdit}
                rules={[
                  { required: true, message: t('task.enterTheTaskKey') },
                  { pattern: PROJECT_KEY_PATTERN, message: t('task.mayContainOnlyLettersDigits') },
                ]}
                placeholder={t('task.exampleKey')}
                tooltip={t('task.taskKeyHint')}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormText
                name="name"
                label={t('task.taskName')}
                rules={[{ required: true, message: t('task.enterTheTaskName') }]}
                placeholder={t('task.exampleDescription')}
              />
            </Col>
            <Col xs={24} md={24} xl={8}>
              <ProFormText
                name="workdir"
                label={t('common.workingDirectory')}
                rules={[{ required: true, message: t('common.enterTheWorkingDirectory') }]}
                placeholder={t('task.exampleWorkdir')}
                tooltip={t('task.workdirAutoHint')}
              />
            </Col>
          </Row>
          <Row gutter={24}>
            <Col xs={24} md={12} xl={8}>
              <ProFormSelect
                name="concurrency"
                label={t('common.concurrencyPolicy')}
                options={CONCURRENCY_OPTIONS}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormSelect name="shell" label="Shell" options={SHELL_OPTIONS} />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormDigit
                name="timeoutSec"
                label={t('common.defaultTimeoutSeconds')}
                min={0}
                tooltip={t('task.timeoutZeroHint')}
              />
            </Col>
          </Row>
          <Row gutter={24}>
            <Col xs={24} md={12} xl={8}>
              <ProFormTextArea
                name="description"
                label={t('common.description')}
                placeholder={t('task.whatThisTaskDoesAnd')}
                fieldProps={{ rows: 2 }}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormSwitch name="hookEnabled" label={t('common.enableHookTrigger')} />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormSwitch
                name="allowEnvOverride"
                label={t('common.allowEnvOverrideOnTrigger')}
                tooltip={t('task.whenTriggeredViaHookOr')}
              />
            </Col>
          </Row>
        </ProCard>

        <ProCard
          title={t('common.environmentVariables')}
          bordered
          headerBordered
          style={{ marginBottom: 16 }}
        >
          <EnvListEditor />
        </ProCard>

        <ProCard title={t('task.stepsExecutedInOrder')} bordered headerBordered>
          <ScriptListEditor
            listName="steps"
            itemNoun={t('common.step')}
            ownerNoun={t('common.task')}
          />
        </ProCard>
      </ProForm>
    </PageContainer>
  );
};

export default TasksEdit;
