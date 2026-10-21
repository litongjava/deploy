import { t } from '@/i18n';
import { SECRET_PLACEHOLDER } from '@/utils/constants';
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons';
import { Button, Col, Form, Input, Row, Space, Switch, Tooltip } from 'antd';
import React from 'react';

export interface EnvDraft {
  id?: number;
  key: string;
  value: string;
  isSecret: boolean;
}

const newEnv = (): EnvDraft => ({ key: '', value: '', isSecret: false });

/**
 * 环境变量编辑器。
 * isSecret 勾选后输入框变成密码框；编辑已存在的 Secret 变量时后端返回空值，
 * 前端显示占位符 `******`，提交时若仍是占位符则保持原值（见 API 文档 §4.3）。
 */
const EnvListEditor: React.FC<{ readonly?: boolean }> = ({ readonly }) => {
  return (
    <Form.List name="envs">
      {(fields, { add, remove }, { errors }) => (
        <div>
          {fields.map((field) => (
            <Row key={field.key} gutter={8} align="middle" style={{ marginBottom: 8 }}>
              <Form.Item name={[field.name, 'id']} hidden>
                <Input />
              </Form.Item>
              <Col xs={24} sm={9}>
                <Form.Item
                  noStyle
                  name={[field.name, 'key']}
                  rules={[
                    { required: true, message: t('env.enterAVariableName') },
                    {
                      pattern: /^[A-Za-z_][A-Za-z0-9_]*$/,
                      message: t('env.variableNamesMayContainOnly'),
                    },
                  ]}
                >
                  <Input placeholder={t('env.variableNamePlaceholder')} />
                </Form.Item>
              </Col>
              <Col xs={18} sm={10}>
                <Form.Item noStyle shouldUpdate>
                  {({ getFieldValue }) => {
                    const isSecret = getFieldValue(['envs', field.name, 'isSecret']);
                    return (
                      <Form.Item noStyle name={[field.name, 'value']}>
                        {isSecret ? (
                          <Input.Password
                            placeholder={
                              readonly
                                ? SECRET_PLACEHOLDER
                                : t('env.ciphertextShowingMeansUnchanged', {
                                    SECRET_PLACEHOLDER: SECRET_PLACEHOLDER,
                                  })
                            }
                            visibilityToggle={false}
                          />
                        ) : (
                          <Input placeholder={t('env.variableValue')} />
                        )}
                      </Form.Item>
                    );
                  }}
                </Form.Item>
              </Col>
              <Col xs={6} sm={5}>
                <Space>
                  <Form.Item noStyle name={[field.name, 'isSecret']} valuePropName="checked">
                    <Switch size="small" disabled={readonly} />
                  </Form.Item>
                  <Tooltip title={t('env.whenCheckedThisVariableIs')}>
                    <span style={{ color: '#999', fontSize: 12 }}>Secret</span>
                  </Tooltip>
                  {readonly ? null : (
                    <Button
                      type="text"
                      danger
                      size="small"
                      icon={<MinusCircleOutlined />}
                      onClick={() => remove(field.name)}
                    />
                  )}
                </Space>
              </Col>
            </Row>
          ))}
          {readonly ? null : (
            <Button type="dashed" block icon={<PlusOutlined />} onClick={() => add(newEnv())}>
              {t('env.addEnvironmentVariable')}{' '}
            </Button>
          )}
          <Form.ErrorList errors={errors} />
        </div>
      )}
    </Form.List>
  );
};

export default EnvListEditor;
