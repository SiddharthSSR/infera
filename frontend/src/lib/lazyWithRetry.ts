import { lazy } from 'react';

function isChunkLoadError(error: unknown): boolean {
  if (!(error instanceof Error)) return false;

  const message = error.message.toLowerCase();
  return (
    message.includes('failed to fetch dynamically imported module') ||
    message.includes('importing a module script failed') ||
    message.includes('failed to load module script') ||
    message.includes('chunkloaderror')
  );
}

export function lazyWithRetry<T extends { default: React.ComponentType<any> }>(
  importer: () => Promise<T>,
  routeKey: string,
) {
  return lazy(async () => {
    const retryKey = `lazy-retry:${routeKey}`;

    try {
      const module = await importer();
      if (typeof window !== 'undefined') {
        window.sessionStorage.removeItem(retryKey);
      }
      return module;
    } catch (error) {
      if (typeof window !== 'undefined' && isChunkLoadError(error)) {
        const hasRetried = window.sessionStorage.getItem(retryKey) === '1';
        if (!hasRetried) {
          window.sessionStorage.setItem(retryKey, '1');
          window.location.reload();
          return new Promise<T>(() => {});
        }
        window.sessionStorage.removeItem(retryKey);
      }

      throw error;
    }
  });
}
