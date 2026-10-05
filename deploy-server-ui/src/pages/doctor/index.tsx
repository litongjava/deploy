import { DoctorPanelWithFetch } from '@/components/DoctorPanel';
import ToolchainPanel from '@/components/ToolchainPanel';
import { PageContainer } from '@ant-design/pro-components';
import React from 'react';

/**
 * 环境自检（独立菜单页）。
 *
 * 以前这两块挤在「系统设置」里：那里是改配置的地方，而自检更像是「看清这台机器现在什么样」，
 * 出问题时要能一眼找到，所以单独开一页。权限仍然分开：
 * 自检任何登录用户可看，工具链安装仅管理员。
 */
const DoctorPage: React.FC = () => {
  return (
    <PageContainer
      title="环境自检"
      content="检查服务端机器上构建工具链的就绪情况；缺失的可以在这里一键安装（安装过程有实时日志，可取消）。"
    >
      <DoctorPanelWithFetch />
      <ToolchainPanel />
    </PageContainer>
  );
};

export default DoctorPage;
