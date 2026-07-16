import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { CreateVaultModelInput, VaultModelFilter } from '../types';
import { createVisibilityAwarePollingOptions, POLLING_INTERVALS_MS } from '../lib/polling';
import { deleteVaultModel, fetchVaultFamilies, fetchVaultModels, fetchVaultStats, registerVaultModel } from '../lib/vaultClient';

export function useVaultModels(filters?: VaultModelFilter) {
  return useQuery({
    queryKey: ['vault-models', filters],
    queryFn: () => fetchVaultModels(filters),
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.vaultModels),
  });
}

export function useVaultStats() {
  return useQuery({
    queryKey: ['vault-stats'],
    queryFn: fetchVaultStats,
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.vaultStats),
  });
}

export function useVaultFamilies() {
  return useQuery({
    queryKey: ['vault-families'],
    queryFn: fetchVaultFamilies,
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.vaultFamilies),
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
