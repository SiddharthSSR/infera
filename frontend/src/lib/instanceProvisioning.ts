import type { GPUType, ProviderStatus, ProviderType } from '../types';
import { isInventoryProviderType, isWorkspaceProviderType } from './providerInventory';

export type ProvisionDraft = {
  name?: string;
  provider?: ProviderType;
  gpu_type?: GPUType;
  gpu_count?: number;
  spot_instance?: boolean;
  models?: string[];
};

export function describeProvisioningState(
  configuredProviders: string[],
  providerStatuses: ProviderStatus[],
  offeringsCount: number,
) {
  const visibleStatuses = providerStatuses.filter((status) => isInventoryProviderType(status.provider));
  const connectedProviders = visibleStatuses.filter((status) => status.connected);
  const hasWorkspaceProviderConfig = configuredProviders.some((provider) => isWorkspaceProviderType(provider));

  if (connectedProviders.length === 0 && !hasWorkspaceProviderConfig) {
    return {
      title: 'No live inventory is connected yet',
      detail: 'Connect RunPod or Vast.ai in Workspace settings, or enable the local inventory source in development before provisioning nodes.',
      action: 'OPEN WORKSPACE',
    };
  }

  if (connectedProviders.length === 0) {
    return {
      title: 'Configured providers are not currently reachable',
      detail: 'At least one provider config exists, but none are returning healthy live status right now. Check credentials and provider status in Workspace settings.',
      action: 'OPEN WORKSPACE',
    };
  }

  if (offeringsCount === 0) {
    return {
      title: 'No GPU offerings are currently available',
      detail: 'Providers are connected, but no matching inventory is being returned for this workspace right now.',
      action: 'VIEW PROVIDERS',
    };
  }

  return null;
}
