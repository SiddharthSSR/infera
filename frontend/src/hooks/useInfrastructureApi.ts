import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ProvisionRequest } from '../types';
import { createVisibilityAwarePollingOptions, POLLING_INTERVALS_MS } from '../lib/polling';
import { fetchCosts, fetchInstances, fetchOfferings, fetchProviders, provisionInstance, startInstance, stopInstance, terminateInstance } from '../lib/infrastructureClient';

export function useInstances() {
  return useQuery({
    queryKey: ['instances'],
    queryFn: fetchInstances,
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.instances),
  });
}

export function useOfferings() {
  return useQuery({
    queryKey: ['offerings'],
    queryFn: fetchOfferings,
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.offerings),
  });
}

export function useProviders() {
  return useQuery({
    queryKey: ['providers'],
    queryFn: fetchProviders,
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.providers),
  });
}

export function useCosts() {
  return useQuery({
    queryKey: ['costs'],
    queryFn: fetchCosts,
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.costs),
  });
}

export function useProvisionInstance() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (request: ProvisionRequest) => provisionInstance(request),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] });
      queryClient.invalidateQueries({ queryKey: ['costs'] });
      queryClient.invalidateQueries({ queryKey: ['models'] });
      queryClient.invalidateQueries({ queryKey: ['deployment-attempts'] });
    },
  });
}

export function useTerminateInstance() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (instanceId: string) => terminateInstance(instanceId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] });
      queryClient.invalidateQueries({ queryKey: ['costs'] });
    },
  });
}

export function useStartInstance() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (instanceId: string) => startInstance(instanceId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] });
    },
  });
}

export function useStopInstance() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (instanceId: string) => stopInstance(instanceId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] });
    },
  });
}
