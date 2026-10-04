/**
 * 全局启动逻辑。
 * 原模板在这里注册 PWA / service worker，本产品是内网部署的管理后台，
 * 不需要离线缓存（反而会导致发版后拿到旧页面），所以这里不再注册 SW，
 * 只保留清理历史遗留注册的逻辑。
 */

const isHttps = typeof document !== 'undefined' && document.location.protocol === 'https:';

const clearCache = () => {
  if (typeof window !== 'undefined' && window.caches) {
    caches
      .keys()
      .then((keys) => {
        keys.forEach((key) => {
          caches.delete(key);
        });
      })
      .catch((e) => console.log(e));
  }
};

if (typeof navigator !== 'undefined' && 'serviceWorker' in navigator && isHttps) {
  const { serviceWorker } = navigator;
  if (serviceWorker.getRegistrations) {
    serviceWorker.getRegistrations().then((sws) => {
      sws.forEach((sw) => {
        sw.unregister();
      });
    });
  }
  clearCache();
}

export {};
