// https://umijs.org/config/
import { defineConfig } from '@umijs/max';
import baseConfig from './baseConfig';
import defaultSettings from './defaultSettings';
import proxy from './proxy';
import routes from './routes';

const { REACT_APP_ENV = 'dev' } = process.env;

const config: ReturnType<typeof defineConfig> = defineConfig({
  base: baseConfig.basePath,
  publicPath: baseConfig.basePath,

  //https://umijs.org/docs/api/config#history
  history: {
    type: 'hash',
  },
  /**
   * @name 开启 hash 模式
   * @description 让 build 之后的产物包含 hash 后缀。通常用于增量发布和避免浏览器加载缓存。
   * @doc https://umijs.org/docs/api/config#hash
   */
  hash: true,

  /**
   * @name 路由的配置，不在路由中引入的文件不会编译
   * @doc https://umijs.org/docs/guides/routes
   */
  routes,
  /**
   * @name 主题的配置
   * @description 虽然叫主题，但是其实只是 less 的变量设置
   * @doc https://ant.design/docs/react/customize-theme-cn
   */
  theme: {
    'root-entry-name': 'variable',
  },
  /**
   * @name 国际化（中英双语）
   *
   * 应用自己的文案走 src/i18n 的 t()（key 就是中文原文，见 src/locales/zh-CN.ts 的说明），
   * 不依赖这个插件。这里开 locale 插件只为一件事：**路由菜单名**。
   * umi 的 layout 插件把菜单名当词条处理——按 `menu.<route name>` 组装 id 去查
   * （见 @umijs/route-utils 的 transformRoute），没有这个插件它拿不到 formatMessage，
   * 菜单就永远是中文原文。
   *
   * 所以 src/locales/ 里只需要维护 `menu.*` 这些 id；其余词条由 src/i18n 负责。
   * baseNavigator 关掉：内网后台默认一律中文，语言由用户在「个人设置」里选，
   * 存 localStorage（umi_locale），与 src/i18n 读的是同一个 key。
   * @doc https://umijs.org/docs/max/i18n
   */
  locale: {
    default: 'zh-CN',
    baseNavigator: false,
    useLocalStorage: true,
    antd: true,
  },
  /**
   * @name moment 的国际化配置
   * @description 如果对国际化没有要求，打开之后能减少js的包大小
   */
  ignoreMomentLocale: true,
  /**
   * @name 代理配置
   * @description 只在本地开发时使用，build 之后无效。见 config/proxy.ts
   */
  proxy: proxy[REACT_APP_ENV as keyof typeof proxy],
  /**
   * @name 快速热更新配置
   */
  fastRefresh: true,
  //============== 以下都是max的插件配置 ===============
  /**
   * @name 数据流插件
   */
  model: {},
  /**
   * @name 全局初始状态（存 currentUser / settings）
   * @doc https://umijs.org/docs/max/data-flow
   */
  initialState: {},
  /**
   * @name layout 插件
   */
  title: baseConfig.title,
  layout: {
    ...defaultSettings,
  },
  /**
   * @name moment2dayjs 插件
   */
  moment2dayjs: {
    preset: 'antd',
    plugins: ['duration'],
  },
  /**
   * @name antd 插件
   */
  antd: {},
  /**
   * @name 网络请求配置
   * @doc https://umijs.org/docs/max/request
   */
  request: {},
  /**
   * @name 权限插件
   * @description 基于 initialState 的权限插件，必须先打开 initialState
   */
  access: {},
  /**
   * @name <head> 中额外的 script
   */
  headScripts: [
    // 解决首次加载时白屏的问题
    { src: baseConfig.basePath + 'scripts/loading.js', async: true },
  ],
  //================ pro 插件配置 =================
  presets: ['umi-presets-pro'],
  mfsu: {
    strategy: 'normal',
  },
  esbuildMinifyIIFE: true,
  mako: {},
  // 关掉 source map。
  //
  // 原来是 'source-map'，实测让 dist 多出 24 个 .map 共 11 MB ——
  // 而这份产物是要 go:embed 进二进制的，map 既撑大体积又没必要地暴露源码结构，
  // 生产环境也从来不提供它。关掉后 dist 从 14.42 MB 降到 3.19 MB，
  // 构建也明显更快。
  devtool: false,
});
export default config;
