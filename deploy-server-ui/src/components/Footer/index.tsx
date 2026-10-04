import { DefaultFooter } from '@ant-design/pro-components';
import React from 'react';

const Footer: React.FC = () => {
  return (
    <DefaultFooter
      style={{
        background: 'none',
      }}
      copyright="轻量命令式 CI/CD 平台"
      links={[]}
    />
  );
};

export default Footer;
