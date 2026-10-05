import { credentialApi, systemApi } from '@/services/system';
import { projectApi } from '@/services/project';
import type {
  CommandInput,
  Credential,
  EnvVar,
  ProjectBrief,
  ProjectInput,
  ProjectKind,
} from '@/services/types';
import {
  PROJECT_KIND_META,
  PROJECT_KIND_OPTIONS,
  SECRET_PLACEHOLDER,
  SHELL_OPTIONS,
  CONCURRENCY_OPTIONS,
} from '@/utils/constants';
import { ArrowLeftOutlined, SaveOutlined } from '@ant-design/icons';
import {
  PageContainer,
  ProForm,
  ProFormDigit,
  ProFormRadio,
  ProFormSelect,
  ProFormSwitch,
  ProFormText,
  ProFormTextArea,
} from '@ant-design/pro-components';
import { history, useParams } from '@umijs/max';
import { Alert, Button, Card, Form, Space, Spin, message } from 'antd';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import ScriptListEditor from '@/components/ScriptListEditor';
import EnvListEditor from '@/components/EnvListEditor';

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
  /** 项目类型：app 应用项目 | lib 构建项目 */
  kind?: ProjectKind;
  /** 依赖的构建项目 key（含依赖执行时先构建它们） */
  dependencies?: string[];
  useGit?: boolean;
  gitUrl?: string;
  gitBranch?: string;
  gitCredentialId?: number | null;
  gitSubdir?: string;
  gitDepth?: number;
  gitClean?: boolean;
  gitSubmodule?: boolean;
  commands?: any[];
  envs?: any[];
}

/** 新建 / 编辑项目，一个表单一次提交（commands、envs 内联，见 API 文档 §4.4） */
const ProjectEdit: React.FC = () => {
  const params = useParams<{ key?: string }>();
  const editKey = params?.key;
  const isEdit = !!editKey;

  const [form] = Form.useForm<FormValues>();
  const [loading, setLoading] = useState(isEdit);
  const [saving, setSaving] = useState(false);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [libProjects, setLibProjects] = useState<ProjectBrief[]>([]);
  const [dependencyPlan, setDependencyPlan] = useState<{ ok: boolean; error?: string; order: string[] }>({
    ok: true,
    order: [],
  });
  const [doctorWarning, setDoctorWarning] = useState<string>('');
  const [messageApi, contextHolder] = message.useMessage();
  const useGit = Form.useWatch('useGit', form);
  const kind = Form.useWatch('kind', form);
  const originalEnvsRef = useRef<EnvVar[]>([]);

  const load = useCallback(async () => {
    if (!isEdit) return;
    setLoading(true);
    try {
      const project = await projectApi.detail(editKey!);
      originalEnvsRef.current = project.envs || [];
      const values: FormValues = {
        key: project.key,
        name: project.name,
        description: project.description,
        workdir: project.workdir,
        shell: project.shell,
        timeoutSec: project.timeoutSec,
        concurrency: project.concurrency,
        hookEnabled: project.hookEnabled,
        allowEnvOverride: project.allowEnvOverride,
        kind: project.kind || 'app',
        dependencies: project.dependencies || [],
        useGit: project.useGit,
        gitUrl: project.gitUrl,
        gitBranch: project.gitBranch,
        gitCredentialId: project.gitCredentialId ?? undefined,
        gitSubdir: project.gitSubdir,
        gitDepth: project.gitDepth,
        gitClean: project.gitClean,
        gitSubmodule: project.gitSubmodule,
        commands: (project.commands || []).map((c) => ({
          id: c.id,
          name: c.name,
          script: c.script,
          enabled: c.enabled,
          failStrategy: c.failStrategy,
          workdirOverride: c.workdirOverride,
          timeoutSec: c.timeoutSec,
          shellOverride: c.shellOverride,
        })),
        envs: (project.envs || []).map((e) => ({
          id: e.id,
          key: e.key,
          value: e.isSecret ? SECRET_PLACEHOLDER : e.value,
          isSecret: e.isSecret,
        })),
      };
      form.setFieldsValue(values);
      setDependencyPlan({
        ok: project.dependencyPlan?.ok !== false,
        error: project.dependencyPlan?.error,
        order: (project.dependencyPlan?.order || []).map((d) => d.key),
      });
    } catch (e) {
      // 统一错误提示
    } finally {
      setLoading(false);
    }
  }, [editKey, form, isEdit]);

  useEffect(() => {
    load();
  }, [load]);

  // 凭据列表 + 可选的依赖项目（构建项目）+ 环境自检提示
  useEffect(() => {
    credentialApi
      .list()
      .then((list) => setCredentials(list || []))
      .catch(() => undefined);
    // 只有「构建项目」能被选为依赖，所以这里只拉 kind=lib
    projectApi
      .list({ page: 1, pageSize: 200, kind: 'lib' })
      .then((res) => setLibProjects(res?.list || []))
      .catch(() => undefined);
    systemApi
      .doctor()
      .then((doc) => {
        const git = (doc?.checks || []).find((c) => c.name === 'git');
        if (git && !git.found) {
          setDoctorWarning(git.hint || '服务端未找到 git 命令，Git 准备阶段会失败。');
        }
      })
      .catch(() => undefined);
  }, []);

  const credentialOptions = useMemo(
    () =>
      (credentials || []).map((c) => ({
        label: `${c.name}（${c.type === 'ssh_key' ? 'SSH Key' : 'HTTPS Token'}）`,
        value: c.id,
      })),
    [credentials],
  );

  /** 依赖下拉：只列构建项目，排除自己 */
  const dependencyOptions = useMemo(
    () =>
      (libProjects || [])
        .filter((p) => !isEdit || p.key !== editKey)
        .map((p) => ({
          label: `${p.name}（${p.key}）${p.archived ? ' · 已归档' : ''}`,
          value: p.key,
          disabled: p.archived,
        })),
    [libProjects, isEdit, editKey],
  );

  const handleSubmit = async (values: FormValues) => {
    setSaving(true);
    try {
      // 命令按当前顺序重排 sortOrder；保留 id 的走更新，没有 id 的走新建
      const commands: CommandInput[] = (values.commands || [])
        .filter((c) => c && (c.name || c.script))
        .map((c, index) => ({
          id: c.id,
          name: c.name || `命令 ${index + 1}`,
          script: c.script || '',
          sortOrder: index + 1,
          enabled: c.enabled !== false,
          failStrategy: c.failStrategy || 'abort',
          workdirOverride: c.workdirOverride || '',
          timeoutSec: Number(c.timeoutSec || 0),
          shellOverride: c.shellOverride || '',
        }));

      // Secret 变量若仍是占位符则沿用原值
      const envs: EnvVar[] = (values.envs || [])
        .filter((e) => e && e.key)
        .map((e) => {
          let value: string = e.value ?? '';
          if (e.isSecret && value === SECRET_PLACEHOLDER) {
            const original = originalEnvsRef.current.find((o) => o.id === e.id);
            value = original?.value ?? '';
          }
          return {
            id: e.id,
            key: e.key,
            value,
            isSecret: !!e.isSecret,
          };
        });

      const payload: ProjectInput = {
        key: values.key,
        name: values.name,
        description: values.description || '',
        workdir: values.workdir,
        shell: values.shell as any,
        timeoutSec: Number(values.timeoutSec || 0),
        concurrency: values.concurrency as any,
        hookEnabled: values.hookEnabled !== false,
        useGit: !!values.useGit,
        kind: (values.kind || 'app') as ProjectKind,
        dependencies: values.dependencies || [],
        gitUrl: values.useGit ? values.gitUrl || '' : '',
        gitBranch: values.gitBranch || 'main',
        gitCredentialId: values.useGit ? (values.gitCredentialId ?? null) : null,
        gitSubdir: values.gitSubdir || '',
        gitDepth: Number(values.gitDepth || 0),
        gitClean: !!values.gitClean,
        gitSubmodule: !!values.gitSubmodule,
        allowEnvOverride: values.allowEnvOverride !== false,
        commands,
        envs,
      };

      if (isEdit) {
        await projectApi.update(editKey!, payload);
        messageApi.success('保存成功');
        history.push(`/projects/${editKey}`);
      } else {
        await projectApi.create(payload);
        messageApi.success('创建成功');
        history.push(`/projects/${payload.key}`);
      }
    } catch (e) {
      // 统一错误提示
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <PageContainer title={isEdit ? `编辑项目 ${editKey}` : '新建项目'}>
        <div style={{ textAlign: 'center', padding: '60px 0' }}>
          <Spin />
        </div>
      </PageContainer>
    );
  }

  return (
    <PageContainer
      title={isEdit ? `编辑项目：${editKey}` : '新建项目'}
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
          searchConfig: { submitText: isEdit ? '保存' : '创建项目' },
          render: (props, doms) => (
            <Space>
              <Button
                type="primary"
                icon={<SaveOutlined />}
                loading={saving}
                onClick={() => props.form?.submit()}
              >
                {isEdit ? '保存' : '创建项目'}
              </Button>
              <Button onClick={() => history.back()}>取消</Button>
            </Space>
          ),
        }}
        onFinish={async (values) => {
          await handleSubmit(values);
          return true;
        }}
        initialValues={{
          shell: 'auto',
          concurrency: 'queue',
          hookEnabled: true,
          allowEnvOverride: true,
          kind: 'app',
          dependencies: [],
          useGit: false,
          gitBranch: 'main',
          gitDepth: 0,
          gitClean: false,
          gitSubmodule: false,
          timeoutSec: 0,
          commands: [],
          envs: [],
        }}
      >
        <Card title="基本信息" style={{ marginBottom: 16 }}>
          <ProFormText
            name="key"
            label="项目 Key"
            width="md"
            tooltip="唯一标识，用于 URL / CLI / Hook 引用，创建后不建议修改"
            disabled={isEdit}
            placeholder="例如 my-app（字母数字与 - _）"
            rules={[
              { required: true, message: '请填写项目 Key' },
              {
                pattern: /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/,
                message: '只能包含字母、数字、- 和 _，且以字母或数字开头',
              },
            ]}
          />
          <ProFormText
            name="name"
            label="项目名称"
            width="md"
            rules={[{ required: true, message: '请填写项目名称' }]}
          />
          <ProFormTextArea name="description" label="描述" width="xl" fieldProps={{ rows: 2 }} />
          <ProFormText
            name="workdir"
            label="工作目录"
            width="xl"
            placeholder="例如 /srv/workspaces/my-app"
            tooltip="绝对路径，支持 ${WORKSPACE_ROOT} 之类的变量由 shell 展开"
            rules={[{ required: true, message: '请填写工作目录' }]}
          />
          <ProFormSelect
            name="shell"
            label="Shell"
            width="sm"
            options={SHELL_OPTIONS}
            tooltip="auto 会按平台选择 bash -lc / sh -c / pwsh / cmd"
          />
          <ProFormDigit
            name="timeoutSec"
            label="默认超时（秒）"
            width="sm"
            min={0}
            tooltip="0 表示不限制"
          />
          <ProFormSelect
            name="concurrency"
            label="并发策略"
            width="md"
            options={CONCURRENCY_OPTIONS}
          />
          <ProFormSwitch
            name="hookEnabled"
            label="启用 Hook 触发"
            tooltip="关闭后该项目的 Hook 地址不可用"
          />
          <ProFormSwitch
            name="allowEnvOverride"
            label="允许触发时覆盖环境变量"
            tooltip="关闭后 Hook / CLI / 网页触发的 env 参数会被忽略"
          />
        </Card>

        <Card title="项目类型与依赖构建" style={{ marginBottom: 16 }}>
          <ProFormRadio.Group
            name="kind"
            label="项目类型"
            width="md"
            options={PROJECT_KIND_OPTIONS}
            tooltip="应用项目=常规的构建/部署单元；构建项目=产出被依赖的产物（底层库），只有构建项目能作为别的项目的依赖"
            rules={[{ required: true, message: '请选择项目类型' }]}
          />
          <Alert
            style={{ marginBottom: 12 }}
            type="info"
            showIcon
            message={PROJECT_KIND_META[(kind as ProjectKind) || 'app'].text}
            description={
              (kind || 'app') === 'lib'
                ? '构建项目：被别的项目依赖时先构建它，它自己也可以依赖更底层的构建项目（多模块/父 pom 场景）。命令通常是 mvn install / go build 之类产生产物的步骤。'
                : '应用项目：可声明依赖的构建项目，执行时先按顺序构建它们（网页上点「执行」默认含依赖）。'
            }
          />
          <ProFormSelect
            name="dependencies"
            label="依赖的构建项目"
            width="xl"
            mode="multiple"
            allowClear
            options={dependencyOptions}
            placeholder="可多选；只列出「构建项目」"
            tooltip="含依赖执行时，先按依赖顺序构建这些项目，全部成功后才执行本项目。直接依赖会自动递归展开；构建项目之间也可以互相依赖（例如 tio-boot-admin 依赖 tio-boot）。"
            fieldProps={{ optionFilterProp: 'label' }}
          />
          {isEdit && dependencyPlan.order.length > 0 ? (
            dependencyPlan.ok ? (
              <Alert
                style={{ marginBottom: 12 }}
                type="success"
                showIcon
                message="构建顺序（含依赖执行时）"
                description={dependencyPlan.order.join('  →  ') + `  →  ${editKey}`}
              />
            ) : (
              <Alert
                style={{ marginBottom: 12 }}
                type="error"
                showIcon
                message="依赖配置当前不可用"
                description={dependencyPlan.error}
              />
            )
          ) : null}
        </Card>

        <Card title="Git 准备阶段" style={{ marginBottom: 16 }}>
          <ProFormSwitch
            name="useGit"
            label="执行前拉取代码"
            tooltip="打开后，每次执行会先 clone/fetch 再跑用户命令"
          />
          {useGit ? (
            <>
              {doctorWarning ? (
                <Alert
                  type="warning"
                  showIcon
                  style={{ marginBottom: 16 }}
                  message="环境提示"
                  description={doctorWarning}
                />
              ) : null}
              <ProFormText
                name="gitUrl"
                label="仓库地址"
                width="xl"
                placeholder="https://gitee.com/xxx/yyy.git 或 git@github.com:xxx/yyy.git"
                rules={[{ required: true, message: '启用 Git 后必须填写仓库地址' }]}
              />
              <ProFormText
                name="gitBranch"
                label="分支 / Tag / Commit"
                width="sm"
                placeholder="main"
                rules={[{ required: true, message: '请填写分支' }]}
              />
              <ProFormSelect
                name="gitCredentialId"
                label="Git 凭据"
                width="md"
                allowClear
                options={credentialOptions}
                placeholder="公开仓库可留空"
                tooltip="私有仓库请先在「设置 → 凭据管理」中创建凭据"
              />
              <ProFormText
                name="gitSubdir"
                label="仓库子目录"
                width="sm"
                placeholder="monorepo 场景，例如 apps/web"
              />
              <ProFormDigit
                name="gitDepth"
                label="浅克隆深度"
                width="sm"
                min={0}
                tooltip="0 表示完整克隆"
              />
              <ProFormSwitch
                name="gitClean"
                label="拉取前 git clean -fdx"
                tooltip="危险：会删除工作目录中所有未跟踪文件"
              />
              <ProFormSwitch name="gitSubmodule" label="递归拉取子模块" />
            </>
          ) : (
            <Alert
              type="info"
              showIcon
              message="未启用 Git 准备阶段"
              description="关闭时平台不拉代码，需要自己把 git pull 之类的命令写进「命令」里。"
            />
          )}
        </Card>

        <Card title="环境变量" style={{ marginBottom: 16 }}>
          <Alert
            style={{ marginBottom: 12 }}
            type="info"
            showIcon
            message="优先级：系统环境 → 项目变量 → Secret 变量 → 内置变量 → 触发时传入的变量（若允许覆盖）"
          />
          <EnvListEditor />
        </Card>

        <Card title="命令列表（按顺序执行）">
          <ScriptListEditor listName="commands" itemNoun="命令" ownerNoun="项目" />
        </Card>
      </ProForm>
    </PageContainer>
  );
};

export default ProjectEdit;
