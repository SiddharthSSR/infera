import type { Worker } from '../types';

const WORKER_SNAPSHOT_GRACE_MS = 20 * 1000;

type CachedWorkerSnapshot = {
  worker: Worker;
  seenAt: number;
};

const workerSnapshotCache = new Map<string, Map<string, CachedWorkerSnapshot>>();

function cacheKey(scope?: string) {
  return scope || 'default';
}

function getScopeCache(scope?: string) {
  const key = cacheKey(scope);
  const existing = workerSnapshotCache.get(key);
  if (existing) return existing;
  const next = new Map<string, CachedWorkerSnapshot>();
  workerSnapshotCache.set(key, next);
  return next;
}

export function stabilizeWorkerSnapshot(
  workers: Worker[] | undefined,
  scope?: string,
  now = Date.now(),
): Worker[] {
  const scopeCache = getScopeCache(scope);
  const nextWorkers = workers || [];
  const liveWorkerIDs = new Set(nextWorkers.map((worker) => worker.worker_id));

  for (const worker of nextWorkers) {
    scopeCache.set(worker.worker_id, { worker, seenAt: now });
  }

  for (const [workerID, snapshot] of scopeCache.entries()) {
    if (now - snapshot.seenAt > WORKER_SNAPSHOT_GRACE_MS) {
      scopeCache.delete(workerID);
    }
  }

  const retainedWorkers = Array.from(scopeCache.values())
    .filter((snapshot) => !liveWorkerIDs.has(snapshot.worker.worker_id))
    .map((snapshot) => snapshot.worker);

  return [...nextWorkers, ...retainedWorkers];
}

export function resetStableWorkerSnapshotCache(scope?: string) {
  if (scope) {
    workerSnapshotCache.delete(cacheKey(scope));
    return;
  }
  workerSnapshotCache.clear();
}
