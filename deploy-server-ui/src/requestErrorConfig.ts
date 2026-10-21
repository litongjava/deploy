import { t } from '@/i18n';
import type { RequestOptions } from '@@/plugin-request/request';
import type { RequestConfig } from '@umijs/max';
import { message, notification } from 'antd';
import { getToken, redirectToLogin } from './services/authStore';

// 错误处理方案： 错误类型
enum ErrorShowType {
  SILENT = 0,
  WARN_MESSAGE = 1,
  ERROR_MESSAGE = 2,
  NOTIFICATION = 3,
  REDIRECT = 9,
}

// 与后端约定的响应数据格式（见 docs/API设计.md §0.1）
interface ResponseStructure {
  success: boolean;
  data: any;
  errorCode?: number;
  errorMessage?: string;
  showType?: ErrorShowType;
}

/** 认证失效错误码（见 docs/API设计.md §0.2） */
const UNAUTHORIZED_CODE = 40100;

/**
 * @name 错误处理
 * pro 自带的错误处理， 可以在这里做自己的改动
 * @doc https://umijs.org/docs/max/request#配置
 */
export const errorConfig: RequestConfig = {
  // 错误处理： umi@3 的错误处理方案。
  errorConfig: {
    // 错误抛出
    errorThrower: (res) => {
      const { success, data, errorCode, errorMessage, showType } =
        res as unknown as ResponseStructure;
      if (!success) {
        // 业务失败里也可能带认证失效（后端约定是 HTTP 401，这里兜底）
        if (errorCode === UNAUTHORIZED_CODE) {
          redirectToLogin();
        }
        const error: any = new Error(errorMessage);
        error.name = 'BizError';
        error.info = { errorCode, errorMessage, showType, data };
        throw error; // 抛出自制的错误
      }
    },
    // 错误接收及处理
    errorHandler: (error: any, opts: any) => {
      const status: number | undefined = error?.response?.status;
      const body: ResponseStructure | undefined = error?.response?.data;

      // 认证失效：HTTP 401 或 errorCode 40100 —— 清 token 跳登录页
      if (status === 401 || body?.errorCode === UNAUTHORIZED_CODE) {
        if (!opts?.skipErrorHandler || status === 401) {
          redirectToLogin();
          return;
        }
      }

      if (opts?.skipErrorHandler) throw error;
      // 我们的 errorThrower 抛出的错误。
      if (error.name === 'BizError') {
        const errorInfo: ResponseStructure | undefined = error.info;
        if (errorInfo) {
          const { errorMessage, errorCode } = errorInfo;
          switch (errorInfo.showType) {
            case ErrorShowType.SILENT:
              // do nothing
              break;
            case ErrorShowType.WARN_MESSAGE:
              message.warning(errorMessage);
              break;
            case ErrorShowType.ERROR_MESSAGE:
              message.error(errorMessage);
              break;
            case ErrorShowType.NOTIFICATION:
              notification.open({
                description: errorMessage,
                message: errorCode,
              });
              break;
            case ErrorShowType.REDIRECT:
              // TODO: redirect
              break;
            default:
              message.error(errorMessage);
          }
        }
      } else if (error.response) {
        // Axios 的错误
        // 请求成功发出且服务器也响应了状态码，但状态代码超出了 2xx 的范围
        message.error(
          body?.errorMessage || t('request.requestFailedHTTP', { status: error.response.status }),
        );
      } else if (error.request) {
        // 请求已经成功发起，但没有收到响应
        message.error(t('request.serviceIsNotRespondingCheck'));
      } else {
        // 发送请求时出了点问题
        message.error(t('request.requestFailedToSendPlease'));
      }
    },
  },

  // 请求拦截器
  requestInterceptors: [
    (config: RequestOptions) => {
      // 从本地存储中获取 token
      const token = getToken();

      // 如果存在 token，则将其添加到请求头中（裸 token，见 API 文档 §0.3）
      if (token) {
        const { headers = {} } = config;
        (headers as Record<string, any>)['Authorization'] = token;
        return { ...config, headers };
      }

      return { ...config };
    },
  ],

  // 响应拦截器
  responseInterceptors: [
    (response) => {
      // 拦截响应数据，进行个性化处理
      const { data, status } = response as any;
      const envelope = data as ResponseStructure | undefined;

      if (status === 401 || envelope?.errorCode === UNAUTHORIZED_CODE) {
        redirectToLogin();
      }
      // 业务失败由 errorThrower 统一处理，这里不再重复弹提示
      return response;
    },
  ],
};
