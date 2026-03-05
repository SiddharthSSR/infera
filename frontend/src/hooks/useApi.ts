import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  fetchWorkers, fetchModels, fetchStats,
  fetchInstances, fetchOfferings, fetchProviders, fetchCosts,
  provisionInstance, terminateInstance, startInstance, stopInstance,
  fetchVaultModels, fetchVaultStats, fetchVaultFamilies,
  registerVaultModel, deleteVaultModel,
} from '../lib/api';
import type { ProvisionRequest, VaultModelFilter, CreateVaultModelInput } from '../types';

export function useWorkers() {
  return useQuery({
    queryKey: ['workers'],
    queryFn: fetchWorkers,
    refetchInterval: 5000,
  });
}

export function useModels() {
  return useQuery({
    queryKey: ['models'],
    queryFn: fetchModels,
    refetchInterval: 10000,
  });
}

export function useStats() {
  return useQuery({
    queryKey: ['stats'],
    queryFn: fetchStats,
    refetchInterval: 2000,
  });
}

export function useInstances() {
  return useQuery({
    queryKey: ['instances'],
    queryFn: fetchInstances,
    refetchInterval: 5000,
  });
}

export function useOfferings() {
  return useQuery({
    queryKey: ['offerings'],
    queryFn: fetchOfferings,
    refetchInterval: 30000,
  });
}

export function useProviders() {
  return useQuery({
    queryKey: ['providers'],
    queryFn: fetchProviders,
    refetchInterval: 10000,
  });
}

export function useCosts() {
  return useQuery({
    queryKey: ['costs'],
    queryFn: fetchCosts,
    refetchInterval: 5000,
  });
}

export function useProvisionInstance() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: (request: ProvisionRequest) => provisionInstance(request),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['instances'] });
      queryClient.invalidateQueries({ queryKey: ['costs'] });
      queryClient.invalidateQueries({ queryKey: ['models'] });
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

// Vault hooks

export function useVaultModels(filters?: VaultModelFilter) {
  return useQuery({
    queryKey: ['vault-models', filters],
    queryFn: () => fetchVaultModels(filters),
    refetchInterval: 10000,
  });
}

export function useVaultStats() {
  return useQuery({
    queryKey: ['vault-stats'],
    queryFn: fetchVaultStats,
    refetchInterval: 10000,
  });
}

export function useVaultFamilies() {
  return useQuery({
    queryKey: ['vault-families'],
    queryFn: fetchVaultFamilies,
    refetchInterval: 30000,
  });
}

export function useRegisterVaultModel() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (model: CreateVaultModelInput) => registerVaultModel(model),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['vault-models'] });
      queryClient.invalidateQueries({ queryKey: ['vault-stats'] });
      queryClient.invalidateQueries({ queryKey: ['vault-families'] });
    },
  });
}

export function useDeleteVaultModel() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => deleteVaultModel(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['vault-models'] });
      queryClient.invalidateQueries({ queryKey: ['vault-stats'] });
      queryClient.invalidateQueries({ queryKey: ['vault-families'] });
    },
  });
}
