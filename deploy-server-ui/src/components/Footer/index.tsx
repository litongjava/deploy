import { DefaultFooter } from '@ant-design/pro-components';
import React from 'react';

/** 开源项目地址（Gitee） */
const SOURCE_CODE_URL = 'https://gitee.com/ppnt/deploy';

const Footer: React.FC = () => {
  return (
    <DefaultFooter
      style={{
        background: 'none',
      }}
      copyright="轻量命令式 CI/CD 平台"
      links={[
        {
          key: 'open-source',
          title: '开源项目地址',
          href: SOURCE_CODE_URL,
          blankTarget: true,
        },
      ]}
    />
  );
};

export default Footer;
