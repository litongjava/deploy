/**
 * @name 代理的配置
 * @description 只在 `pnpm start` / `pnpm dev` 本地开发时生效，build 之后无效。
 * 生产环境前端由 deploy-server 同源提供，不需要代理。
 * @doc https://umijs.org/docs/guides/proxy
 */
export default {
  dev: {
    // localhost:8000/api/** -> http://127.0.0.1:10055/api/**
    '/api/': {
      target: 'http://127.0.0.1:10055',
      changeOrigin: true,
      // WebSocket 日志流也要走代理（/api/v1/ws/runs/{id}）
      ws: true,
    },
  },
};
