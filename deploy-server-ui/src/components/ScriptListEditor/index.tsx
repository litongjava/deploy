import { t } from '@/i18n';
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons';
import { ProFormSelect, ProFormSwitch } from '@ant-design/pro-components';
import { Button, Card, Col, Form, Input, InputNumber, Row, Space } from 'antd';
import React from 'react';

/**
 * 一步 shell 的草稿形态。项目里叫「命令」，任务里叫「步骤」，
 * 字段完全一样，所以两边共用这一个编辑器与同一个数据结构。
 */
export interface ScriptDraft {
  id?: number;
  name: string;
  script: string;
  enabled: boolean;
  failStrategy: 'abort' | 'continue';
  workdirOverride?: string;
  timeoutSec?: number;
  shellOverride?: string;
  sortOrder?: number;
}

export const newScriptDraft = (): ScriptDraft => ({
  name: '',
  script: '',
  enabled: true,
  failStrategy: 'abort',
  workdirOverride: '',
  timeoutSec: 0,
  shellOverride: '',
});

export type ScriptListEditorProps = {
  /**
   * Form.List 的字段名：项目用 commands，任务用 steps。
   * 之所以做成参数，是因为两者的后端字段名不同（项目是子表，任务是内联 JSON），
   * 但编辑体验与校验规则完全一致。
   */
  listName: string;
  /** 列表里每一项的叫法：命令 / 步骤 */
  itemNoun: string;
  /** 归属对象的叫法，用于「留空使用 XX 的工作目录」这类提示：项目 / 任务 */
  ownerNoun: string;
  /** 只读展示（详情页复用编辑器时用） */
  readonly?: boolean;
};

/**
 * 步骤/命令列表编辑器。
 *
 * 用 antd 的 Form.List 管理顺序（可上移/下移/删除/新增），
 * sortOrder 在提交时按数组下标重新生成，见两个编辑页的 handleSubmit。
 */
const ScriptListEditor: React.FC<ScriptListEditorProps> = ({
  listName,
  itemNoun,
  ownerNoun,
  readonly,
}) => {
  return (
    <Form.List name={listName}>
      {(fields, { add, remove, move }, { errors }) => (
        <div>
          {fields.length === 0 ? (
            <Card size="small" style={{ marginBottom: 12, textAlign: 'center', color: '#999' }}>
              {t('scriptList.noYetClickAddBelow', { noun: itemNoun })}
            </Card>
          ) : null}

          {fields.map((field, index) => (
            <Card
              key={field.key}
              size="small"
              style={{ marginBottom: 12 }}
              title={
                <Space>
                  <span>
                    {itemNoun} {index + 1}
                  </span>
                  <Form.Item noStyle shouldUpdate>
                    {({ getFieldValue }) => {
                      const enabled = getFieldValue([listName, field.name, 'enabled']);
                      return enabled === false ? (
                        <span style={{ color: '#999', fontSize: 12 }}>
                          {t('scriptList.disabledWillNotRun')}
                        </span>
                      ) : null;
                    }}
                  </Form.Item>
                </Space>
              }
              extra={
                readonly ? null : (
                  <Space>
                    <Button
                      size="small"
                      disabled={index === 0}
                      onClick={() => move(index, index - 1)}
                    >
                      {t('scriptList.moveUp')}{' '}
                    </Button>
                    <Button
                      size="small"
                      disabled={index === fields.length - 1}
                      onClick={() => move(index, index + 1)}
                    >
                      {t('scriptList.moveDown')}{' '}
                    </Button>
                    <Button
                      size="small"
                      danger
                      icon={<MinusCircleOutlined />}
                      onClick={() => remove(field.name)}
                    >
                      {t('common.delete')}{' '}
                    </Button>
                  </Space>
                )
              }
            >
              {/* 隐藏字段：项目命令有行 ID，编辑时要保留才能走「更新」而不是「新建」；
                  任务步骤没有 ID，这一项会一直是空的，提交时被忽略。 */}
              <Form.Item name={[field.name, 'id']} hidden>
                <Input />
              </Form.Item>

              <Row gutter={12}>
                <Col xs={24} md={12}>
                  <Form.Item
                    label={t('common.name')}
                    name={[field.name, 'name']}
                    rules={[
                      {
                        required: true,
                        message: t('scriptList.enterTheName', { itemNoun: itemNoun }),
                      },
                    ]}
                  >
                    <Input placeholder={t('scriptList.exampleScript')} />
                  </Form.Item>
                </Col>
                <Col xs={24} md={12}>
                  <Form.Item
                    label={t('common.failureStrategy')}
                    name={[field.name, 'failStrategy']}
                  >
                    <ProFormSelect
                      options={[
                        { label: t('common.failureAbort'), value: 'abort' },
                        { label: t('common.failureContinue'), value: 'continue' },
                      ]}
                      fieldProps={{ style: { width: '100%' } }}
                    />
                  </Form.Item>
                </Col>
              </Row>

              <Form.Item
                label={t('scriptList.script')}
                name={[field.name, 'script']}
                rules={[{ required: true, message: t('scriptList.enterTheScriptContent') }]}
                extra={t('scriptList.writtenInShBashSyntax')}
              >
                <Input.TextArea
                  autoSize={{ minRows: 3, maxRows: 16 }}
                  placeholder={'#!/bin/sh\ncd "$WORKDIR"\nls -la'}
                  style={{ fontFamily: 'Menlo, Consolas, monospace' }}
                />
              </Form.Item>

              <Row gutter={12}>
                <Col xs={24} md={10}>
                  {/* 这里必须让 ProFormSwitch 自己拿 name：它内部就是一个完整的
                      ProFormItem，再套一层 Form.Item 会变成两层，外层拿不到值，
                      开关就永远停在「关」，点开也写不回表单（见 tests/script-editor-switch.test.tsx）。 */}
                  <ProFormSwitch name={[field.name, 'enabled']} label={t('common.enable')} />
                </Col>
                <Col xs={24} md={14} />
              </Row>

              <Row gutter={12}>
                <Col xs={24} md={8}>
                  <Form.Item
                    label={t('scriptList.workingDirectoryOverride')}
                    name={[field.name, 'workdirOverride']}
                    tooltip={t('scriptList.inheritWorkdirHint', { ownerNoun: ownerNoun })}
                  >
                    <Input placeholder={t('common.optional')} />
                  </Form.Item>
                </Col>
                <Col xs={12} md={8}>
                  <Form.Item
                    label={t('scriptList.timeoutSeconds')}
                    name={[field.name, 'timeoutSec']}
                    tooltip={t('scriptList.timeoutZeroHint', { ownerNoun: ownerNoun })}
                  >
                    <InputNumber min={0} style={{ width: '100%' }} />
                  </Form.Item>
                </Col>
                <Col xs={12} md={8}>
                  <Form.Item
                    label={t('scriptList.shellOverride')}
                    name={[field.name, 'shellOverride']}
                    tooltip={t('scriptList.inheritSettingHint', { ownerNoun: ownerNoun })}
                  >
                    <Input placeholder={t('scriptList.optionalBashShPwshCmd')} />
                  </Form.Item>
                </Col>
              </Row>
            </Card>
          ))}

          {readonly ? null : (
            <Button
              type="dashed"
              block
              icon={<PlusOutlined />}
              onClick={() => add(newScriptDraft())}
            >
              {t('scriptList.add', { noun: itemNoun })}
            </Button>
          )}
          <Form.ErrorList errors={errors} />
        </div>
      )}
    </Form.List>
  );
};

export default ScriptListEditor;
