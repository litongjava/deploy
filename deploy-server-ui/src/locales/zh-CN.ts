/**
 * 简体中文词条。
 *
 * 本项目的做法是**直接用中文原文当词条 key**：代码里写
 *   intl.formatMessage({ id: '新建项目', defaultMessage: '新建项目' })
 * 或更常见的简写
 *   t('新建项目')
 * 于是这个文件几乎不需要维护——umi 的 formatMessage 在词条缺失时会回落到
 * defaultMessage，也就是那句中文原文本身。
 *
 * 只有三类需要在这里显式写：
 *   1. 路由菜单名（umi 的 layout 插件按 `menu.<name>` 取词条，id 不是原文）；
 *   2. 同一个中文原文在不同语境下要译成不同英文的（用 `ctx.xxx` 这种带前缀的 id）；
 *   3. 需要在中文侧也改写成另一种说法的。
 * 其余一律留空，由 en-US.ts 提供英文。
 */
export default {
  // 路由菜单：umi layout 插件用 `menu.` + 路由 name 组装 id（见 @umijs/route-utils
  // transformRoute 的 getItemLocaleName），所以这里必须显式给出中文侧的值。
  'menu.概览': '概览',
  'menu.服务器': '服务器',
  'menu.项目': '项目',
  'menu.任务': '任务',
  'menu.运行': '运行',
  'menu.环境自检': '环境自检',
  'menu.设置': '设置',
  'menu.个人设置': '个人设置',
  'menu.用户管理': '用户管理',
  'menu.凭据管理': '凭据管理',
  'menu.系统设置': '系统设置',
  'menu.登录': '登录',
  'menu.初始化': '初始化',
};