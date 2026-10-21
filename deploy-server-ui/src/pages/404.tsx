import { t } from '@/i18n';
import { history } from '@umijs/max';
import { Button, Result } from 'antd';
import React from 'react';

const NoFoundPage: React.FC = () => (
  <Result
    status="404"
    title="404"
    subTitle={t('error.notFound')}
    extra={
      <Button type="primary" onClick={() => history.push('/')}>
        {t('error.backToHome')}{' '}
      </Button>
    }
  />
);

export default NoFoundPage;
