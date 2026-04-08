import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  fetchWorkers, fetchModels, fetchStats, fetchAgents,
  fetchInstances, fetchOfferings, fetchProviders, fetchCosts,
  provisionInstance, terminateInstance, startInstance, stopInstance,
  fetchDeploymentAttempts, updateDeploymentVerification, markDeploymentAutoVerificationRequested,
  fetchVaultModels, fetchVaultStats, fetchVaultFamilies,
  registerVaultModel, deleteVaultModel,
} from '../lib/api';
import type { ProvisionRequest, VaultModelFilter, CreateVaultModelInput } from '../types';
import type { DeploymentAttemptRecord } from '../lib/deploymentHistory';
import { stabilizeWorkerSnapshot } from '../lib/stableWorkers';

export function useWorkers(workspaceID?: string) {
  return useQuery({
    queryKey: ['workers', workspaceID],
    queryFn: async () => stabilizeWorkerSnapshot(await fetchWorkers(), workspaceID),
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

export function useAgents() {
  return useQuery({
    queryKey: ['agents'],
    queryFn: fetchAgents,
    refetchInterval: 30000,
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

export function useDeploymentAttempts(workspaceID?: string) {
  return useQuery({
    queryKey: ['deployment-attempts', workspaceID],
    queryFn: fetchDeploymentAttempts,
    enabled: Boolean(workspaceID),
    refetchInterval: 5000,
  });
}

function updateAttemptInCache(
  queryClient: ReturnType<typeof useQueryClient>,
  workspaceID: string | undefined,
  attempt: DeploymentAttemptRecord,
) {
  if (!workspaceID) return;
  queryClient.setQueryData<DeploymentAttemptRecord[] | undefined>(
    ['deployment-attempts', workspaceID],
    (current) => {
      if (!current) return [attempt];
      const next = [attempt, ...current.filter((item) => item.id !== attempt.id)];
      return next.sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at));
    },
  );
}

export function useUpdateDeploymentVerification(workspaceID?: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      attemptId,
      verification,
    }: {
      attemptId: string;
      verification: NonNullable<DeploymentAttemptRecord['inference_verification']>;
    }) => updateDeploymentVerification(attemptId, verification),
    onSuccess: (attempt) => {
      updateAttemptInCache(queryClient, workspaceID, attempt);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['deployment-attempts', workspaceID] });
    },
  });
}

export function useMarkDeploymentAutoVerificationRequested(workspaceID?: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ attemptId, requestedAt }: { attemptId: string; requestedAt: string }) =>
      markDeploymentAutoVerificationRequested(attemptId, requestedAt),
    onSuccess: (attempt) => {
      updateAttemptInCache(queryClient, workspaceID, attempt);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['deployment-attempts', workspaceID] });
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
