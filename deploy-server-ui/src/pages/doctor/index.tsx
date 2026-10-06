import { DoctorPanelWithFetch } from '@/components/DoctorPanel';
import MiddlewarePanel from '@/components/MiddlewarePanel';
import ToolchainPanel from '@/components/ToolchainPanel';
import { PageContainer } from '@ant-design/pro-components';
import React from 'react';

/**
 * 环境自检（独立菜单页）。
 *
 * 以前这两块挤在「系统设置」里：那里是改配置的地方，而自检更像是「看清这台机器现在什么样」，
 * 出问题时要能一眼找到，所以单独开一页。权限仍然分开：
 * 自检任何登录用户可看，工具链安装仅管理员。
 *
 * 中间件面板也挂在这一页：它和工具链属于同一件事的两半——「先把构建环境补齐，再把依赖的服务跑起来」，
 * 而且都要看这台机器上到底有什么（比如 Docker 有没有），分到两个页面反而要来回切。
 */
const DoctorPage: React.FC = () => {
  return (
    <PageContainer
      title="环境自检"
      content="检查服务端机器上构建工具链与中间件（数据库 / 缓存 / 搜索）的就绪情况；缺失的可以在这里一键安装，装完还能启停、重启、移除（每次操作都有实时日志，可取消）。"
    >
      <DoctorPanelWithFetch />
      <ToolchainPanel />
      <MiddlewarePanel />
    </PageContainer>
  );
};

export default DoctorPage;
