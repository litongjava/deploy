import zhCN from '../locales/zh-CN';
import enUS from '../locales/en-US';

/**
 * 极简的界面文案国际化。
 *
 * 为什么不用 umi 自带的 `formatMessage`：它走 react-intl 的 ICU 解析，而本项目的文案里
 * 有大量 shell 变量（`${WORKDIR}`、`${BUILD_NUMBER}`）和运行日志片段，ICU 会把 `{WORKDIR}`
 * 当成插值参数解析、在缺参时抛错。这里改成「查字典 + 无参时原样返回」的朴素实现，
 * 对这类文案天然安全，也省掉了为转义 ICU 语法而生的样板。
 *
 * **词条 key 就是中文原文**（模板串里直接写 `{参数名}` 占位）。中文侧因此几乎不用维护：
 * 查不到就用 key 本身，天然就是中文。真正要写的是 `en-US.ts`。
 *
 * 切换语言后**整页刷新**（和 umi locale 插件默认行为一致）：这样连模块级常量、
 * antd 的 ConfigProvider、路由菜单名都会一起切过去，不会出现半中半英。
 */

export type Lang = 'zh-CN' | 'en-US';

/** 与 umi locale 插件共用同一个 localStorage key，两套系统读的是同一份选择 */
const STORAGE_KEY = 'umi_locale';

export const LANG_OPTIONS: { label: string; value: Lang }[] = [
  { label: '简体中文', value: 'zh-CN' },
  { label: 'English', value: 'en-US' },
];

const DICTS: Record<Lang, Record<string, string>> = {
  'zh-CN': zhCN as Record<string, string>,
  'en-US': enUS as Record<string, string>,
};

function detect(): Lang {
  if (typeof window === 'undefined') return 'zh-CN';
  try {
    const saved = window.localStorage.getItem(STORAGE_KEY);
    if (saved === 'en-US' || saved === 'zh-CN') return saved;
  } catch (e) {
    // 隐私模式等读不到 localStorage：回退中文
  }
  return 'zh-CN';
}

let currentLang: Lang = detect();

export function getLang(): Lang {
  return currentLang;
}

/** 切换语言：写回存储后刷新页面，保证整棵树（含 antd / 路由菜单）一起换语言 */
export function setLang(lang: Lang): void {
  if (lang === currentLang) return;
  try {
    window.localStorage.setItem(STORAGE_KEY, lang);
  } catch (e) {
    // 存不进去也要切，至少当前这次刷新内生效
  }
  currentLang = lang;
  window.location.reload();
}

/**
 * 插值参数。值类型放宽到 unknown：调用点常把 ProTable / ProDescriptions 的
 * render 值（类型是 ReactNode）直接传进来，卡成 string|number 会到处报错，
 * 而这里无论如何都会 String() 一次。
 */
export type TParams = Record<string, unknown>;

/**
 * 取文案。
 *
 * @param key 中文原文（可含 `{参数名}` 占位）
 * @param params 插值参数；**不传就不做任何替换**，因此 `${WORKDIR}` 这类字面量会原样保留
 */
export function t(key: string, params?: TParams): string {
  const raw = DICTS[currentLang]?.[key] ?? key;
  if (!params) return raw;
  return raw.replace(/\{(\w+)\}/g, (match, name: string) => {
    const v = params[name];
    return v === undefined || v === null ? match : String(v);
  });
}

/** 组件里取 t；同时拿到当前语言与切换函数 */
export function useI18n() {
  return { t, lang: currentLang, setLang };
}

export default t;