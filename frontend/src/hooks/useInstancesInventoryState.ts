import { useEffect, useMemo, useState } from 'react';

import { fetchWorkspaceProviderConfigs } from '../lib/workspaceAdminClient';
import { describeProvisioningState } from '../lib/instanceProvisioning';
import { isInventoryProviderType, WORKSPACE_PROVIDER_TYPES } from '../lib/providerInventory';
import type { GPUOffering, Instance, ProviderStatus } from '../types';

export function useInstancesInventoryState({
  role,
  workspaceID,
  providers,
  offerings,
  filteredInstances,
}: {
  role: string;
  workspaceID: string | undefined;
  providers: ProviderStatus[] | undefined;
  offerings: GPUOffering[] | undefined;
  filteredInstances: Instance[];
}) {
  const [configuredProviders, setConfiguredProviders] = useState<string[]>([]);

  const visibleProviderStatuses = useMemo(
    () => (providers || []).filter((status) => isInventoryProviderType(status.provider)),
    [providers],
  );
  const visibleOfferings = useMemo(
    () => (offerings || []).filter((offering) => isInventoryProviderType(offering.provider)),
    [offerings],
  );
  const connectedProviders = useMemo(
    () => visibleProviderStatuses.filter((status) => status.connected),
    [visibleProviderStatuses],
  );
  const providerRail = useMemo(() => {
    const extras = visibleProviderStatuses
      .map((status) => status.provider)
      .filter((provider) => !WORKSPACE_PROVIDER_TYPES.includes(provider as typeof WORKSPACE_PROVIDER_TYPES[number]));
    return [...WORKSPACE_PROVIDER_TYPES, ...extras];
  }, [visibleProviderStatuses]);
  const provisioningState = useMemo(
    () => describeProvisioningState(configuredProviders, visibleProviderStatuses, visibleOfferings.length),
    [configuredProviders, visibleOfferings.length, visibleProviderStatuses],
  );
  const providerSummary = useMemo(
    () => (
      filteredInstances.length > 0
        ? [...new Set(filteredInstances.map((instance) => instance.provider))]
        : visibleProviderStatuses
          .filter((status) => status.provider === 'mock' || configuredProviders.includes(status.provider))
          .map((status) => status.provider)
    ),
    [configuredProviders, filteredInstances, visibleProviderStatuses],
  );

  useEffect(() => {
    if (!workspaceID) {
      setConfiguredProviders([]);
      return;
    }

    if (role !== 'owner' && role !== 'admin') {
      setConfiguredProviders(visibleProviderStatuses.map((status) => status.provider));
      return;
    }

    let cancelled = false;

    fetchWorkspaceProviderConfigs(workspaceID)
      .then((configs) => {
        if (cancelled) return;
        setConfiguredProviders(configs.filter((config) => config.configured).map((config) => config.provider));
      })
      .catch(() => {
        if (cancelled) return;
        setConfiguredProviders(visibleProviderStatuses.map((status) => status.provider));
      });

    return () => {
      cancelled = true;
    };
  }, [role, visibleProviderStatuses, workspaceID]);

  return {
    configuredProviders,
    connectedProviders,
    providerRail,
    providerSummary,
    provisioningState,
    visibleOfferings,
    visibleProviderStatuses,
  };
}
