export const POLLING_INTERVALS_MS = {
  workers: 5000,
  models: 30000,
  agents: 30000,
  stats: 5000,
  instances: 10000,
  offerings: 60000,
  providers: 30000,
  costs: 15000,
  deployments: 5000,
  vaultModels: 30000,
  vaultStats: 30000,
  vaultFamilies: 60000,
} as const;

type VisibilityStateLike = 'hidden' | 'visible' | 'prerender';

export function resolvePollingInterval(
  intervalMs: number,
  visibilityState: VisibilityStateLike = readVisibilityState(),
): number | false {
  if (intervalMs <= 0) {
    return false;
  }

  return visibilityState === 'visible' ? intervalMs : false;
}

export function createVisibilityAwarePollingOptions(intervalMs: number) {
  return {
    refetchInterval: () => resolvePollingInterval(intervalMs),
    refetchIntervalInBackground: false as const,
  };
}

function readVisibilityState(): VisibilityStateLike {
  if (typeof document === 'undefined') {
    return 'visible';
  }

  const visibilityState = String(document.visibilityState);

  if (visibilityState === 'hidden') {
    return 'hidden';
  }

  if (visibilityState === 'prerender') {
    return 'prerender';
  }

  return 'visible';
}
