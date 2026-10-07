/**
 * English messages.
 *
 * Keys are the **Chinese source strings** (see zh-CN.ts): the app calls
 * `formatMessage({ id: '新建项目', defaultMessage: '新建项目' })` through the `t()` helper,
 * so this file is the only place that actually needs English wording.
 *
 * Anything missing here falls back to the Chinese `defaultMessage`, so a gap shows up as
 * Chinese text inside the English UI rather than as a crash.
 */
export default {
  // Route menu names (umi's layout plugin looks up `menu.<route name>`).
  'menu.概览': 'Overview',
  'menu.服务器': 'Server',
  'menu.项目': 'Projects',
  'menu.任务': 'Tasks',
  'menu.运行': 'Runs',
  'menu.环境自检': 'Doctor',
  'menu.设置': 'Settings',
  'menu.个人设置': 'Profile',
  'menu.用户管理': 'Users',
  'menu.凭据管理': 'Credentials',
  'menu.系统设置': 'System Settings',
  'menu.登录': 'Sign in',
  'menu.初始化': 'Setup',
};