import { history } from '@umijs/max';
import { Button, Result } from 'antd';
import React from 'react';
import { t } from '@/i18n';

const NoFoundPage: React.FC = () => (
  <Result
    status="404"
    title="404"
    subTitle={t('抱歉，你访问的页面不存在。')}
    extra={
      <Button type="primary" onClick={() => history.push('/')}>
         {t('返回首页')} </Button>
    }
  />
);

export default NoFoundPage;
