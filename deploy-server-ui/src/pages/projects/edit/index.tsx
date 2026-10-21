import EnvListEditor from '@/components/EnvListEditor';
import ScriptListEditor from '@/components/ScriptListEditor';
import { t } from '@/i18n';
import { projectApi } from '@/services/project';
import { credentialApi, systemApi } from '@/services/system';
import type {
  CommandInput,
  Credential,
  EnvVar,
  ProjectBrief,
  ProjectInput,
  ProjectKind,
} from '@/services/types';
import {
  CONCURRENCY_OPTIONS,
  PROJECT_KIND_META,
  PROJECT_KIND_OPTIONS,
  SECRET_PLACEHOLDER,
  SHELL_OPTIONS,
  defaultWorkdir,
} from '@/utils/constants';
import { inferProjectFromGitUrl } from '@/utils/repoInfer';
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
  /** 依赖的项目 key（含依赖执行时先构建它们） */
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
  const [dependencyCandidates, setDependencyCandidates] = useState<ProjectBrief[]>([]);
  const [dependencyPlan, setDependencyPlan] = useState<{
    ok: boolean;
    error?: string;
    order: string[];
  }>({
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
   * （Key = 规整后的仓库名，名称 = 仓库名，工作目录 = `/srv/workspaces/<key>`），
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
    const finalKey = keyEditedRef.current ? manualKey : (inferred?.key ?? manualKey);

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

  // 凭据列表 + 可选的依赖项目 + 环境自检提示
  useEffect(() => {
    credentialApi
      .list()
      .then((list) => setCredentials(list || []))
      .catch(() => undefined);
    // 任何项目都能被依赖（应用项目与构建项目都可以），所以拉全量、不做 kind 过滤；
    // 已归档的仍在列表里但置灰，便于看出「原来依赖的那个被归档了」。
    projectApi
      .list({ page: 1, pageSize: 200, archived: 'all' })
      .then((res) => setDependencyCandidates(res?.list || []))
      .catch(() => undefined);
    systemApi
      .doctor()
      .then((doc) => {
        const git = (doc?.checks || []).find((c) => c.name === 'git');
        if (git && !git.found) {
          setDoctorWarning(git.hint || t('project.gitCommandWasNotFound'));
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

  /** 依赖下拉：任何项目都可以是依赖，排除自己 */
  const dependencyOptions = useMemo(
    () =>
      (dependencyCandidates || [])
        .filter((p) => !isEdit || p.key !== editKey)
        .map((p) => ({
          label: `${p.name}（${p.key}）${p.archived ? t('project.archived') : ''}`,
          value: p.key,
          disabled: p.archived,
        })),
    [dependencyCandidates, isEdit, editKey],
  );

  const handleSubmit = async (values: FormValues) => {
    setSaving(true);
    try {
      // 命令按当前顺序重排 sortOrder；保留 id 的走更新，没有 id 的走新建
      const commands: CommandInput[] = (values.commands || [])
        .filter((c) => c && (c.name || c.script))
        .map((c, index) => ({
          id: c.id,
          name: c.name || t('project.command', { p0: index + 1 }),
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
        messageApi.success(t('common.saveSuccess'));
        history.push(`/projects/${editKey}`);
      } else {
        await projectApi.create(payload);
        messageApi.success(t('common.created'));
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
      <PageContainer
        title={isEdit ? t('project.editTitle', { editKey: editKey }) : t('common.newProject')}
      >
        <div style={{ textAlign: 'center', padding: '60px 0' }}>
          <Spin />
        </div>
      </PageContainer>
    );
  }

  return (
    <PageContainer
      title={isEdit ? t('project.editTitleColon', { editKey: editKey }) : t('common.newProject')}
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
          searchConfig: { submitText: isEdit ? t('common.save') : t('project.createProject') },
          render: (props, doms) => (
            <Space>
              <Button
                type="primary"
                icon={<SaveOutlined />}
                loading={saving}
                onClick={() => props.form?.submit()}
              >
                {isEdit ? t('common.save') : t('project.createProject')}
              </Button>
              <Button onClick={() => history.back()}>{t('common.cancel')}</Button>
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
        <Card title={t('project.gitPreparationStage')} style={{ marginBottom: 16 }}>
          <ProFormSwitch
            name="useGit"
            label={t('project.pullCodeBeforeRunning')}
            tooltip={t('project.whenOnEachRunClones')}
          />
          {useGit ? (
            <>
              {doctorWarning ? (
                <Alert
                  type="warning"
                  showIcon
                  style={{ marginBottom: 16 }}
                  message={t('project.environmentNotice')}
                  description={doctorWarning}
                />
              ) : null}
              <Row gutter={24}>
                <Col xs={24} xl={16}>
                  <ProFormText
                    name="gitUrl"
                    label={t('common.repositoryURL')}
                    tooltip={t('project.onceFilledTheProjectKey')}
                    placeholder={t('project.httpsGiteeComXxxYyy')}
                    rules={[{ required: true, message: t('project.repositoryURLIsRequiredWhen') }]}
                  />
                </Col>
                <Col xs={24} md={12} xl={8}>
                  <ProFormText
                    name="gitBranch"
                    label={t('project.branchTagCommit')}
                    placeholder="main"
                    rules={[{ required: true, message: t('project.enterABranch') }]}
                  />
                </Col>
              </Row>
              <Row gutter={24}>
                <Col xs={24} md={12} xl={8}>
                  <ProFormSelect
                    name="gitCredentialId"
                    label={t('project.gitCredential')}
                    allowClear
                    options={credentialOptions}
                    placeholder={t('project.leaveEmptyForPublicRepositories')}
                    tooltip={t('project.privateRepositoriesCreateACredential')}
                  />
                </Col>
                <Col xs={24} md={12} xl={8}>
                  <ProFormText
                    name="gitSubdir"
                    label={t('project.repositorySubdirectory')}
                    placeholder={t('project.monoreposEGAppsWeb')}
                  />
                </Col>
                <Col xs={24} md={12} xl={8}>
                  <ProFormDigit
                    name="gitDepth"
                    label={t('project.shallowCloneDepth')}
                    min={0}
                    tooltip={t('project.cloneDepthHint')}
                  />
                </Col>
              </Row>
              <Row gutter={24}>
                <Col xs={24} md={12} xl={8}>
                  <ProFormSwitch
                    name="gitClean"
                    label={t('project.runGitCleanFdxBefore')}
                    tooltip={t('project.dangerDeletesAllUntrackedFiles')}
                  />
                </Col>
                <Col xs={24} md={12} xl={8}>
                  <ProFormSwitch
                    name="gitSubmodule"
                    label={t('project.recursivelyFetchSubmodules')}
                  />
                </Col>
              </Row>
            </>
          ) : (
            <Alert
              type="info"
              showIcon
              message={t('project.gitPreparationStageIsDisabled')}
              description={t('project.whenOffThePlatformDoes')}
            />
          )}
        </Card>

        <Card title={t('common.basicInformation')} style={{ marginBottom: 16 }}>
          {/* PC（≥1200px）一行三个、平板（≥768px）一行两个、手机一列一个，互相不挤 */}
          <Row gutter={24}>
            <Col xs={24} md={12} xl={8}>
              <ProFormText
                name="key"
                label={t('project.projectKey')}
                tooltip={t('project.uniqueIdentifierUsedInUrls')}
                disabled={isEdit}
                placeholder={t('project.exampleProjectKey')}
                rules={[
                  { required: true, message: t('project.enterTheProjectKey') },
                  {
                    pattern: /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/,
                    message: t('project.mayContainOnlyLettersDigits'),
                  },
                ]}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormText
                name="name"
                label={t('project.projectName')}
                rules={[{ required: true, message: t('project.enterTheProjectName') }]}
              />
            </Col>
            <Col xs={24} md={24} xl={8}>
              <ProFormText
                name="workdir"
                label={t('common.workingDirectory')}
                placeholder={t('project.exampleWorkdir')}
                tooltip={t('project.workdirHint')}
                rules={[{ required: true, message: t('common.enterTheWorkingDirectory') }]}
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
              <ProFormSelect
                name="shell"
                label="Shell"
                options={SHELL_OPTIONS}
                tooltip={t('project.shellAutoHint')}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormDigit
                name="timeoutSec"
                label={t('common.defaultTimeoutSeconds')}
                min={0}
                tooltip={t('project.zeroMeansNoLimit')}
              />
            </Col>
          </Row>
          <Row gutter={24}>
            <Col xs={24} md={12} xl={8}>
              <ProFormTextArea
                name="description"
                label={t('common.description')}
                fieldProps={{ rows: 2 }}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormSwitch
                name="hookEnabled"
                label={t('common.enableHookTrigger')}
                tooltip={t('project.whenOffThisProjectHook')}
              />
            </Col>
            <Col xs={24} md={12} xl={8}>
              <ProFormSwitch
                name="allowEnvOverride"
                label={t('common.allowEnvOverrideOnTrigger')}
                tooltip={t('project.whenOffEnvParametersFrom')}
              />
            </Col>
          </Row>
        </Card>

        <Card title={t('project.projectTypeAndDependencyBuilds')} style={{ marginBottom: 16 }}>
          <Row gutter={24}>
            <Col xs={24} xl={8}>
              <ProFormRadio.Group
                name="kind"
                label={t('project.projectType')}
                options={PROJECT_KIND_OPTIONS}
                tooltip={t('project.typeHint')}
                rules={[{ required: true, message: t('project.selectAProjectType') }]}
              />
            </Col>
            <Col xs={24} xl={16}>
              <ProFormSelect
                name="dependencies"
                label={t('project.dependentProjects')}
                mode="multiple"
                allowClear
                options={dependencyOptions}
                placeholder={t('project.multipleSelectionsAllProjectsAre')}
                tooltip={t('project.whenRunningWithDependenciesThese')}
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
                ? t('project.buildProjectProducesArtifactsOther')
                : t('project.applicationProjectCanDeclareDependent')
            }
          />
          {isEdit && dependencyPlan.order.length > 0 ? (
            dependencyPlan.ok ? (
              <Alert
                style={{ marginBottom: 12 }}
                type="success"
                showIcon
                message={t('project.buildOrderWhenRunningWith')}
                description={dependencyPlan.order.join('  →  ') + `  →  ${editKey}`}
              />
            ) : (
              <Alert
                style={{ marginBottom: 12 }}
                type="error"
                showIcon
                message={t('project.dependencyUnavailable')}
                description={dependencyPlan.error}
              />
            )
          ) : null}
        </Card>

        <Card title={t('common.environmentVariables')} style={{ marginBottom: 16 }}>
          <Alert
            style={{ marginBottom: 12 }}
            type="info"
            showIcon
            message={t('project.envPriority')}
          />
          <EnvListEditor />
        </Card>

        <Card title={t('project.commandsExecutedInOrder')}>
          <ScriptListEditor
            listName="commands"
            itemNoun={t('common.command')}
            ownerNoun={t('common.project')}
          />
        </Card>
      </ProForm>
    </PageContainer>
  );
};

export default ProjectEdit;
