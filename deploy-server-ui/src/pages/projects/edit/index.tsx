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
  defaultWorkdir,
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
import { Alert, Button, Card, Col, Form, Row, Space, Spin, message } from 'antd';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import ScriptListEditor from '@/components/ScriptListEditor';
import { inferProjectFromGitUrl } from '@/utils/repoInfer';
import EnvListEditor from '@/components/EnvListEditor';
import { t } from '@/i18n';

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
  const keyValue = Form.useWatch('key', form);
  const originalEnvsRef = useRef<EnvVar[]>([]);

  /**
   * 「工作目录 / 项目名称 / 项目 Key」是否被用户手动填过。
   *
   * 新建时这三个字段会依次尝试自动生成：先看 Git 仓库地址能不能推出仓库名
   * （Key = 规整后的仓库名，名称 = 仓库名，工作目录 = `/srv/worksapce/<key>`），
   * 推不出来（还没填地址、或地址里没有仓库名）再回落到「Key 手填 → 其余跟着 Key 走」。
   * 用户一旦自己填过某字段，之后就不再覆盖它；清空后恢复自动生成。
   */
  const keyEditedRef = useRef(false);
  const workdirEditedRef = useRef(false);
  const nameEditedRef = useRef(false);

  const gitUrlValue = Form.useWatch('gitUrl', form);

  /**
   * 自动预填（只在新建时生效，且逐字段尊重「用户是否手动改过」）：
   *
   * - Key：优先用仓库地址推出来的（规整成合法 Key）；推不出来就留用户自己填的。
   * - 名称：优先用仓库名；推不出来就用 Key。**只在用户没手填过名称时**才替换。
   * - 工作目录：始终是 `<默认根目录>/<最终 Key>`——注意用的是**最终** Key
   *   （可能是用户手填的），否则「手填 Key + 改了仓库地址」会把目录指到另一个仓库上。
   *
   * 依赖 `keyValue` 会在上一次 effect 之后更新，所以这里读到的就是当前生效的 Key。
   */
  useEffect(() => {
    if (isEdit) return;
    const inferred = inferProjectFromGitUrl(gitUrlValue);
    const manualKey = (keyValue ?? '').trim();
    const finalKey = keyEditedRef.current ? manualKey : inferred?.key ?? manualKey;

    const patch: Partial<FormValues> = {};
    if (!keyEditedRef.current && inferred) patch.key = inferred.key;
    if (!nameEditedRef.current) {
      patch.name = inferred?.name ?? finalKey;
    }
    if (!workdirEditedRef.current) {
      patch.workdir = defaultWorkdir(finalKey);
    }
    if (Object.keys(patch).length > 0) form.setFieldsValue(patch);
  }, [form, isEdit, keyValue, gitUrlValue]);

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
          setDoctorWarning(git.hint || t('服务端未找到 git 命令，Git 准备阶段会失败。'));
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
          label: `${p.name}（${p.key}）${p.archived ? t(' · 已归档') : ''}`,
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
          name: c.name || t('命令 {p0}', { p0: index + 1 }),
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
        messageApi.success(t('保存成功'));
        history.push(`/projects/${editKey}`);
      } else {
        await projectApi.create(payload);
        messageApi.success(t('创建成功'));
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
      <PageContainer title={isEdit ? t('编辑项目 {editKey}', { editKey: editKey }) : t('新建项目')}>
        <div style={{ textAlign: 'center', padding: '60px 0' }}>
          <Spin />
        </div>
      </PageContainer>
    );
  }

  return (
    <PageContainer
      title={isEdit ? t('编辑项目：{editKey}', { editKey: editKey }) : t('新建项目')}
      onBack={() => history.back()}
      extra={[
        <Button key="back" icon={<ArrowLeftOutlined />} onClick={() => history.back()}>
           {t('返回')} </Button>,
      ]}
    >
      {contextHolder}
      <ProForm<FormValues>
        form={form}
        submitter={{
          searchConfig: { submitText: isEdit ? t('保存') : t('创建项目') },
          render: (props, doms) => (
            <Space>
              <Button
                type="primary"
                icon={<SaveOutlined />}
                loading={saving}
                onClick={() => props.form?.submit()}
              >
                {isEdit ? t('保存') : t('创建项目')}
              </Button>
              <Button onClick={() => history.back()}>{t('取消')}</Button>
            </Space>
          ),
        }}
        onFinish={async (values) => {
          await handleSubmit(values);
          return true;
        }}
        onValuesChange={(changed) => {
          // 只有用户自己改过的字段才算手动值；上面的 setFieldsValue 不会触发这里。
          if (Object.prototype.hasOwnProperty.call(changed, 'key')) {
            keyEditedRef.current = !!String(changed.key ?? '').trim();
          }
          if (Object.prototype.hasOwnProperty.call(changed, 'workdir')) {
            workdirEditedRef.current = !!String(changed.workdir ?? '').trim();
          }
          if (Object.prototype.hasOwnProperty.call(changed, 'name')) {
            nameEditedRef.current = !!String(changed.name ?? '').trim();
          }
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
        <Card title={t('Git 准备阶段')} style={{ marginBottom: 16 }}>
          <ProFormSwitch
            name="useGit"
            label={t('执行前拉取代码')}
            tooltip={t('打开后，每次执行会先 clone/fetch 再跑用户命令')}
          />
          {useGit ? (
            <>
              {doctorWarning ? (
                <Alert
                  type="warning"
                  showIcon
                  style={{ marginBottom: 16 }}
                  message={t('环境提示')}
                  description={doctorWarning}
                />
              ) : null}
              <Row gutter={24}>
                <Col xs={24} xl={16}>
                  <ProFormText
                    name="gitUrl"
                    label={t('仓库地址')}
                    tooltip={t('填好后会按仓库名自动预填「项目 Key / 项目名称 / 工作目录」；这三项一旦手动改过就不再覆盖')}
                    placeholder={t('https://gitee.com/xxx/yyy.git 或 git@github.com:xxx/yyy.git')}
                    rules={[{ required: true, message: t('启用 Git 后必须填写仓库地址') }]}
                  />
                </Col>
                <Col xs={24} md={12} xl={8}>
                  <ProFormText
                    name="gitBranch"
                    label={t('分支 / Tag / Commit')}
                    placeholder="main"
                    rules={[{ required: true, message: t('请填写分支') }]}
                  />
                </Col>
              </Row>
              <Row gutter={24}>
                <Col xs={24} md={12} xl={8}>
                  <ProFormSelect
                    name="gitCredentialId"
                    label={t('Git 凭据')}
                    allowClear
                    options={credentialOptions}
                    placeholder={t('公开仓库可留空')}
                    tooltip={t('私有仓库请先在「设置 → 凭据管理」中创建凭据')}
                  />
                </Col>
                <Col xs={24} md={12} xl={8}>
                  <ProFormText
                    name="gitSubdir"
                    label={t('仓库子目录')}
                    placeholder={t('monorepo 场景，例如 apps/web')}
                  />
                </Col>
                <Col xs={24} md={12} xl={8}>
                  <ProFormDigit
                    name="gitDepth"
                    label={t('浅克隆深度')}
                    min={0}
                    tooltip={t('0 表示完整克隆')}
                  />
                </Col>
              </Row>
              <Row gutter={24}>
                <Col xs={24} md={12} xl={8}>
                  <ProFormSwitch
                    name="gitClean"
                    label={t('拉取前 git clean -fdx')}
                    tooltip={t('危险：会删除工作目录中所有未跟踪文件')}
                  />
                </Col>
                <Col xs={24} md={12} xl={8}>
                  <ProFormSwitch name="gitSubmodule" label={t('递归拉取子模块')} />
                </Col>
              </Row>
            </>
          ) : (
            <Alert
              type="info"
              showIcon
              message={t('未启用 Git 准备阶段')}
              description={t('关闭时平台不拉代码，需要自己把 git pull 之类的命令写进「命令」里。')}
            />
          )}
        </Card>

        <Card title={t('基本信息')} style={{ marginBottom: 16 }}>
          {/* PC（≥1200px）一行三个、平板（≥768px）一行两个、手机一列一个，互相不挤 */}
          <Row gutter={24}>
            <Col xs={24} md={12} xl={8}>
              <ProFormText
                name="key"
                label={t('项目 Key')}
                tooltip={t('唯一标识，用于 URL / CLI / Hook 引用，创建后不建议修改；新建时会按上方「仓库地址」自动推断')}
                disabled={isEdit}
                placeholder={t('例如 my-app（字母数字与 - _）')}
                rules={[
                  { required: true, message: t('请填写项目 Key') },
                  {
                    pattern: /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/,
                    message: t('只能包含字母、数字、- 和 _，且以字母或数字开头'),
                  },
                ]}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormText
                name="name"
                label={t('项目名称')}
                rules={[{ required: true, message: t('请填写项目名称') }]}
              />
            </Col>
            <Col xs={24} md={24} xl={8}>
              <ProFormText
                name="workdir"
                label={t('工作目录')}
                placeholder={t('例如 /srv/worksapce/tio-boot')}
                tooltip={t('绝对路径。新建时按项目 Key 自动填成 /srv/worksapce/<key>，可手动改；支持 ${WORKSPACE_ROOT} 之类的变量由 shell 展开')}
                rules={[{ required: true, message: t('请填写工作目录') }]}
              />
            </Col>
          </Row>
          <Row gutter={24}>
            <Col xs={24} md={12} xl={8}>
              <ProFormSelect
                name="concurrency"
                label={t('并发策略')}
                options={CONCURRENCY_OPTIONS}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormSelect
                name="shell"
                label="Shell"
                options={SHELL_OPTIONS}
                tooltip={t('auto 会按平台选择 bash -lc / sh -c / pwsh / cmd')}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormDigit
                name="timeoutSec"
                label={t('默认超时（秒）')}
                min={0}
                tooltip={t('0 表示不限制')}
              />
            </Col>
          </Row>
          <Row gutter={24}>
            <Col xs={24} md={12} xl={8}>
              <ProFormTextArea name="description" label={t('描述')} fieldProps={{ rows: 2 }} />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormSwitch
                name="hookEnabled"
                label={t('启用 Hook 触发')}
                tooltip={t('关闭后该项目的 Hook 地址不可用')}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormSwitch
                name="allowEnvOverride"
                label={t('允许触发时覆盖环境变量')}
                tooltip={t('关闭后 Hook / CLI / 网页触发的 env 参数会被忽略')}
              />
            </Col>
          </Row>
        </Card>

        <Card title={t('项目类型与依赖构建')} style={{ marginBottom: 16 }}>
          <Row gutter={24}>
            <Col xs={24} xl={8}>
              <ProFormRadio.Group
                name="kind"
                label={t('项目类型')}
                options={PROJECT_KIND_OPTIONS}
                tooltip={t('应用项目=常规的构建/部署单元；构建项目=产出被依赖的产物（底层库），只有构建项目能作为别的项目的依赖')}
                rules={[{ required: true, message: t('请选择项目类型') }]}
              />
            </Col>
            <Col xs={24} xl={16}>
              <ProFormSelect
                name="dependencies"
                label={t('依赖的构建项目')}
                mode="multiple"
                allowClear
                options={dependencyOptions}
                placeholder={t('可多选；只列出「构建项目」')}
                tooltip={t('含依赖执行时，先按依赖顺序构建这些项目，全部成功后才执行本项目。直接依赖会自动递归展开；构建项目之间也可以互相依赖（例如 tio-boot-admin 依赖 tio-boot）。')}
                fieldProps={{ optionFilterProp: 'label' }}
              />
            </Col>
          </Row>
          <Alert
            style={{ marginBottom: 12 }}
            type="info"
            showIcon
            message={PROJECT_KIND_META[(kind as ProjectKind) || 'app'].text}
            description={
              (kind || 'app') === 'lib'
                ? t('构建项目：被别的项目依赖时先构建它，它自己也可以依赖更底层的构建项目（多模块/父 pom 场景）。命令通常是 mvn install / go build 之类产生产物的步骤。')
                : t('应用项目：可声明依赖的构建项目，执行时先按顺序构建它们（网页上点「执行」默认含依赖）。')
            }
          />
          {isEdit && dependencyPlan.order.length > 0 ? (
            dependencyPlan.ok ? (
              <Alert
                style={{ marginBottom: 12 }}
                type="success"
                showIcon
                message={t('构建顺序（含依赖执行时）')}
                description={dependencyPlan.order.join('  →  ') + `  →  ${editKey}`}
              />
            ) : (
              <Alert
                style={{ marginBottom: 12 }}
                type="error"
                showIcon
                message={t('依赖配置当前不可用')}
                description={dependencyPlan.error}
              />
            )
          ) : null}
        </Card>


        <Card title={t('环境变量')} style={{ marginBottom: 16 }}>
          <Alert
            style={{ marginBottom: 12 }}
            type="info"
            showIcon
            message={t('优先级：系统环境 → 项目变量 → Secret 变量 → 内置变量 → 触发时传入的变量（若允许覆盖）')}
          />
          <EnvListEditor />
        </Card>

        <Card title={t('命令列表（按顺序执行）')}>
          <ScriptListEditor listName="commands" itemNoun={t('命令')} ownerNoun={t('项目')} />
        </Card>
      </ProForm>
    </PageContainer>
  );
};

export default ProjectEdit;
