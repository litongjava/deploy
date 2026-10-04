import { QuestionCircleOutlined } from '@ant-design/icons';

export type SiderTheme = 'light' | 'dark';

/** 本项目为中文单语言界面，不做多语言切换 */
export const SelectLang = () => {
  return null;
};

export const Question = () => {
  return (
    <div
      style={{
        display: 'flex',
        height: 26,
        cursor: 'pointer',
      }}
      title="接口契约见 docs/API设计.md"
    >
      <QuestionCircleOutlined />
    </div>
  );
};
