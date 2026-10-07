import { LANG_OPTIONS, useI18n } from '@/i18n';
import { GlobalOutlined } from '@ant-design/icons';
import { Segmented } from 'antd';
import React from 'react';

/**
 * 语言切换（中 / 英）。
 *
 * 切换后 `setLang` 会整页刷新：这样 antd 的 ConfigProvider、路由菜单名、
 * 以及模块级常量表里的文案都会一起换过去，不会出现半中半英。
 * 选择结果存在 localStorage（umi_locale），下次打开仍是上次选的。
 */
export const LanguageSwitcher: React.FC<{ block?: boolean }> = ({ block }) => {
  const { lang, setLang } = useI18n();
  return (
    <Segmented
      block={block}
      value={lang}
      onChange={(v) => setLang(v as 'zh-CN' | 'en-US')}
      options={LANG_OPTIONS}
    />
  );
};

/**
 * 顶栏下拉里的语言项：显示「另一种语言」的名字，点一下切过去。
 *
 * 显示目标语言而不是当前语言，是为了让用户一眼看出「点了会变成什么」；
 * 下拉项本身只承担切换动作，所以标签里直接带了 onClick。
 */
export const LanguageMenuItemLabel: React.FC<{ onClick?: () => void }> = ({ onClick }) => {
  const { lang, setLang } = useI18n();
  const next = lang === 'zh-CN' ? 'en-US' : 'zh-CN';
  const label = LANG_OPTIONS.find((o) => o.value === next)?.label ?? next;
  return (
    <span
      onClick={() => {
        onClick?.();
        setLang(next);
      }}
    >
      <GlobalOutlined style={{ marginRight: 8 }} />
      {label}
    </span>
  );
};

export default LanguageSwitcher;