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
