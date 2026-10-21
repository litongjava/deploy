/**
 * @name umi 的路由配置
 * @description 只支持 path,component,routes,redirect,wrappers,name,icon 的配置
 * @param path  path 只支持两种占位符配置，第一种是动态参数 :id 的形式，第二种是 * 通配符，通配符只能出现路由字符串的最后。
 * @param component 配置 location 和 path 匹配后用于渲染的 React 组件路径。
 * @param name 菜单标识（与 src/locales 的 menu.<name> 词条对应，文案不走这里）
 * @param icon 图标，取值参考 https://ant.design/components/icon-cn
 * @doc https://umijs.org/docs/guides/routes
 */
export default [
  {
    path: '/user',
    layout: false,
    routes: [
      {
        path: '/user/login',
        layout: false,
        name: 'login',
        component: './user/login',
      },
      {
        path: '/user/setup',
        layout: false,
        name: 'setup',
        component: './user/setup',
      },
      {
        path: '/user',
        redirect: '/user/login',
      },
      {
        component: '404',
        path: '/user/*',
      },
    ],
  },
  {
    path: '/',
    name: 'dashboard',
    icon: 'DashboardOutlined',
    component: './dashboard',
  },
  {
    // 服务器自身的资源占用（CPU/内存/磁盘）。后端数据来自 GET /api/v1/system/info，
    // 任意登录用户可见——只暴露聚合指标，不含工作目录、密钥等配置细节。
    path: '/server-info',
    name: 'server',
    icon: 'CloudServerOutlined',
    component: './server-info',
  },
  {
    path: '/projects',
    name: 'projects',
    icon: 'AppstoreOutlined',
    routes: [
      {
        path: '/projects',
        component: './projects',
      },
      {
        // 新建项目必须有独立的静态子路由：只靠 /projects/:key 会把 /projects/edit
        // 当成 key=edit 的项目详情页，然后去查一个不存在的项目。
        // 静态段在路由排序里优先于 :key，所以 'new' 是保留字，不能用作项目 Key。
        path: '/projects/new',
        hideInMenu: true,
        name: 'new',
        component: './projects/edit',
      },
      {
        path: '/projects/:key/edit',
        hideInMenu: true,
        name: 'edit',
        component: './projects/edit',
      },
      {
        path: '/projects/:key',
        hideInMenu: true,
        name: 'detail',
        component: './projects/detail',
      },
    ],
  },
  {
    // 任务：跑一件事（压测 / 巡检 / 清缓存……）。
    // 与项目的区别是没有 Git 准备阶段与依赖构建，所以编辑页也没有那两张卡片。
    path: '/tasks',
    name: 'tasks',
    icon: 'UnorderedListOutlined',
    routes: [
      {
        path: '/tasks',
        component: './tasks',
      },
      {
        // 与 /projects/new 同理：静态段优先于 :key，'new' 是保留字。
        path: '/tasks/new',
        hideInMenu: true,
        name: 'new',
        component: './tasks/edit',
      },
      {
        path: '/tasks/:key/edit',
        hideInMenu: true,
        name: 'edit',
        component: './tasks/edit',
      },
      {
        path: '/tasks/:key',
        hideInMenu: true,
        name: 'detail',
        component: './tasks/detail',
      },
    ],
  },
  {
    path: '/runs',
    name: 'runs',
    icon: 'PlayCircleOutlined',
    routes: [
      {
        path: '/runs',
        component: './runs',
      },
      {
        path: '/runs/:id',
        hideInMenu: true,
        name: 'detail',
        component: './runs/detail',
      },
    ],
  },
  {
    // 环境自检单独占一个菜单：它回答的是「这台服务器现在什么样、缺什么工具」，
    // 与「改配置」不是一回事，出问题时也更容易被找到。
    path: '/doctor',
    name: 'doctor',
    icon: 'ToolOutlined',
    component: './doctor',
  },
  {
    path: '/settings',
    name: 'settings',
    icon: 'SettingOutlined',
    routes: [
      {
        path: '/settings',
        redirect: '/settings/profile',
      },
      {
        path: '/settings/profile',
        name: 'profile',
        component: './settings/profile',
      },
      {
        path: '/settings/users',
        name: 'users',
        access: 'canAdmin',
        component: './settings/users',
      },
      {
        path: '/settings/credentials',
        name: 'credentials',
        component: './settings/credentials',
      },
      {
        path: '/settings/system',
        name: 'system',
        component: './settings/system',
      },
    ],
  },
  {
    path: '/exception',
    layout: false,
    routes: [
      {
        path: '/exception/403',
        name: '403',
        component: './exception/403',
      },
      {
        path: '/exception/404',
        name: '404',
        component: './exception/404',
      },
      {
        path: '/exception/500',
        name: '500',
        component: './exception/500',
      },
    ],
  },
  {
    path: '*',
    layout: false,
    component: '404',
  },
];
