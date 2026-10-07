import {ProLayoutProps} from '@ant-design/pro-components';
import baseConfig from "./baseConfig";

const Settings: ProLayoutProps & {
  pwa?: boolean;
  logo?: string;
} = {
  navTheme: 'light',
  // 拂晓蓝
  colorPrimary: '#1890ff',
  // 侧边菜单 + 顶部栏：用户名显示在右上角（side 布局下 ProLayout 会把头像/用户名
  // 放在侧边栏底部，header 根本不渲染；mix 布局才会渲染顶部栏并把右侧内容放到右上角）
  layout: "mix",
  contentWidth: 'Fluid',
  fixedHeader: false,
  // mix 布局下 ProLayout 会把 header 固定住（needFixedHeader = fixedHeader || layout === "mix"），
  // 侧边栏必须一起固定才会被顶栏让开位置（-fixed-mix 的 top:56px、height:calc(100% - 56px)）；
  // 否则侧栏从 y=0 开始，第一个菜单项会被顶栏盖住。
  fixSiderbar: true,
  colorWeak: false,
  title: baseConfig.title,
  pwa: true,
  logo: baseConfig.basePath + 'logo.svg',
  iconfontUrl: '',
  splitMenus: false,
  // 让菜单名走 i18n：ProLayout 只有在 menu.locale 为真时才会用 formatMessage 翻路由
  // name（见 @umijs/route-utils 的 transformRoute），否则菜单永远显示路由里写的中文。
  // umi 的 locale 插件把 formatMessage 传给 ProLayout（见 config/config.ts 的 locale 段）。
  menu: { locale: true },
  token: {
    // 参见ts声明，demo 见文档，通过token 修改样式
    //https://procomponents.ant.design/components/layout#%E9%80%9A%E8%BF%87-token-%E4%BF%AE%E6%94%B9%E6%A0%B7%E5%BC%8F
  },
};

export default Settings;
