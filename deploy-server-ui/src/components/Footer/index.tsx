import { DefaultFooter } from '@ant-design/pro-components';
import React from 'react';
import { t } from '@/i18n';

/** 开源项目地址（Gitee）：不再单独占一行，直接做在版权文字上 */
const SOURCE_CODE_URL = 'https://gitee.com/ppnt/deploy';

/**
 * 版权文字本身就是指向仓库的链接。
 *
 * DefaultFooter 把 copyright 的类型写成了 WithFalse<string>，但运行时是直接渲染这个
 * 节点的（`<CopyrightOutlined /> {copyright}`），所以这里断言一下类型 —— 不然为了一个
 * 链接就得放弃它自带的样式与「©」前缀。
 */
const COPYRIGHT = (
  <a href={SOURCE_CODE_URL} target="_blank" rel="noreferrer">
     {t('轻量命令式 CI/CD 平台')} </a>
) as unknown as string;

const Footer: React.FC = () => {
  return (
    <DefaultFooter
      style={{
        background: 'none',
      }}
      copyright={COPYRIGHT}
      links={[]}
    />
  );
};

export default Footer;
