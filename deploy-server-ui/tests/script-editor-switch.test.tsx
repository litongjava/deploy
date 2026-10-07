import { act, fireEvent, render } from '@testing-library/react';
import { Form } from 'antd';
import React from 'react';
import ScriptListEditor, { newScriptDraft } from '../src/components/ScriptListEditor';

/**
 * 「启用」开关的回归测试。
 *
 * 曾经的 bug：ScriptListEditor 把 ProFormSwitch 包在了一层 Form.Item 里，
 * 而 ProFormSwitch 自己就是一个完整的 ProFormItem，于是变成两层，
 * 外层 Form.Item 拿不到值，导致：
 *   1）打开编辑页永远显示为「未启用」（不管库里存的是什么）；
 *   2）在页面上点开关也写不回表单，保存后库里还是原值。
 *
 * 这两个方向都要测：控件 → 表单（点击写回）、表单 → 控件（外部赋值能显示）。
 */

type Values = { commands: Array<Record<string, unknown>> };

const Harness: React.FC<{
  initialEnabled: boolean | undefined;
  formRef: { current?: any };
}> = ({ initialEnabled, formRef }) => {
  const [form] = Form.useForm<Values>();
  formRef.current = form;
  return (
    <Form
      form={form}
      initialValues={{ commands: [{ id: 1, name: 'update', script: 'echo hi', enabled: initialEnabled }] }}
    >
      <ScriptListEditor listName="commands" itemNoun="命令" ownerNoun="项目" />
    </Form>
  );
};

const setup = (initialEnabled: boolean | undefined) => {
  const formRef: { current?: any } = {};
  render(<Harness initialEnabled={initialEnabled} formRef={formRef} />);
  const sw = () => document.querySelector('.ant-switch') as HTMLElement;
  const checked = () => sw().getAttribute('aria-checked') === 'true';
  const formValue = () => formRef.current?.getFieldsValue()?.commands?.[0]?.enabled;
  return { sw, checked, formValue, formRef };
};

describe('ScriptListEditor 的「启用」开关', () => {
  it('库里是启用时，打开编辑页应显示为已启用', () => {
    const { checked } = setup(true);
    expect(checked()).toBe(true);
  });

  it('库里是禁用时，打开编辑页应显示为未启用', () => {
    const { checked } = setup(false);
    expect(checked()).toBe(false);
  });

  it('点击开关要把新值写回表单（关 → 开）', () => {
    const { sw, checked, formValue } = setup(false);
    fireEvent.click(sw());
    expect(checked()).toBe(true);
    expect(formValue()).toBe(true);
  });

  it('点击开关要把新值写回表单（开 → 关）', () => {
    const { sw, checked, formValue } = setup(true);
    fireEvent.click(sw());
    expect(checked()).toBe(false);
    expect(formValue()).toBe(false);
  });

  it('新增命令的默认值是「启用」', () => {
    expect(newScriptDraft().enabled).toBe(true);
  });

  it('外部 setFieldsValue 改了 enabled，开关显示要跟着变', () => {
    const { checked, formValue, formRef } = setup(true);
    expect(checked()).toBe(true);

    // 模拟 load() 拉到 enabled=false 的命令后回填表单
    act(() => {
      formRef.current.setFieldsValue({
        commands: [{ id: 1, name: 'update', script: 'echo hi', enabled: false }],
      });
    });

    expect(formValue()).toBe(false);
    expect(checked()).toBe(false);
  });
});