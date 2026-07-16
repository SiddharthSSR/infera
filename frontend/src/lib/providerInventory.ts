import type { ProviderType } from '../types';

export const WORKSPACE_PROVIDER_TYPES = ['runpod', 'vastai'] as const;
export const INVENTORY_PROVIDER_TYPES = ['runpod', 'vastai', 'mock'] as const;

export type WorkspaceProviderType = typeof WORKSPACE_PROVIDER_TYPES[number];
export type InventoryProviderType = typeof INVENTORY_PROVIDER_TYPES[number];

export function isWorkspaceProviderType(provider: string): provider is WorkspaceProviderType {
  return WORKSPACE_PROVIDER_TYPES.includes(provider as WorkspaceProviderType);
}

export function isInventoryProviderType(provider: string): provider is InventoryProviderType {
  return INVENTORY_PROVIDER_TYPES.includes(provider as InventoryProviderType);
}

export function getProviderDisplayName(provider: string | ProviderType): string {
  switch (provider) {
    case 'runpod':
      return 'RunPod';
    case 'vastai':
      return 'Vast.ai';
    case 'mock':
      return 'Local inventory';
    case 'lambda':
      return 'Lambda';
    default:
      return provider;
  }
}
