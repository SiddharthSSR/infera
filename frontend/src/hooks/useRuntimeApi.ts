import { useQuery } from '@tanstack/react-query';
import { fetchAgents } from '../lib/agentsClient';
import { createVisibilityAwarePollingOptions, POLLING_INTERVALS_MS } from '../lib/polling';
import { fetchModels, fetchStats, fetchWorkers } from '../lib/runtimeClient';
import { stabilizeWorkerSnapshot } from '../lib/stableWorkers';

export function useWorkers(workspaceID?: string) {
  return useQuery({
    queryKey: ['workers', workspaceID],
    queryFn: async () => stabilizeWorkerSnapshot(await fetchWorkers(), workspaceID),
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.workers),
  });
}

export function useModels() {
  return useQuery({
    queryKey: ['models'],
    queryFn: fetchModels,
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.models),
  });
}

export function useAgents() {
  return useQuery({
    queryKey: ['agents'],
    queryFn: fetchAgents,
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.agents),
  });
}

export function useStats() {
  return useQuery({
    queryKey: ['stats'],
    queryFn: fetchStats,
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.stats),
  });
}
