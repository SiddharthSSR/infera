import { useCallback, useMemo } from 'react';

import type { DeploymentAttemptRecord } from '../lib/deploymentHistory';
import { summarizeDeploymentAttempt } from '../lib/deploymentHistory';
import { getInstanceReadiness } from '../lib/instanceReadiness';
import { deriveModelRuntimeDrilldown } from '../lib/modelRuntimeDrilldown';
import { getProviderDisplayName, isInventoryProviderType } from '../lib/providerInventory';
import type { GPUOffering, Instance, Model, ProviderStatus, Worker } from '../types';

const RECOMMENDED_MODEL_IDS = [
  'Qwen/Qwen3-4B-Thinking-2507',
  'moonshotai/Kimi-K2.5-Instruct',
] as const;

const GPU_VRAM_GB: Record<string, number> = {
  RTX_4090: 24,
  RTX_4080: 16,
  A100_40GB: 40,
  A100_80GB: 80,
  H100: 80,
  L40S: 48,
};

export type ModelServingOverview = {
  state: 'not_deployed' | 'runtime_pending' | 'serving_unverified' | 'serving_verified' | 'serving_failed' | 'degraded';
  summary: string;
  badgeLabel: string;
  badgeTone: '' | 'warning' | 'error' | 'inactive';
  activeInstances: number;
  verifiedAt?: string;
  latestVerificationError?: string;
  latestVerificationLatencyMs?: number;
  latestAttempt?: ReturnType<typeof summarizeDeploymentAttempt> | null;
};

export type ModelDeployReadiness = {
  state: 'active' | 'deploying' | 'setup' | 'capacity' | 'ready';
  summary: string;
  actionLabel: string;
  actionTarget: string;
};

export function describeDeployReadiness(
  model: Model,
  offerings: GPUOffering[],
  providers: ProviderStatus[],
): ModelDeployReadiness {
  const connectedProviders = providers.filter((provider) => provider.connected);
  const requiredMB = model.vram_required || 0;
  const compatibleOfferings = offerings.filter((offering) => {
    if (!requiredMB) return true;
    const vramGB = offering.memory_gb || GPU_VRAM_GB[offering.gpu_type] || 0;
    return vramGB * 1024 >= requiredMB;
  });
  const cheapest = compatibleOfferings.reduce<GPUOffering | null>((best, offering) => {
    if (!best || offering.cost_per_hour < best.cost_per_hour) return offering;
    return best;
  }, null);
  const providerNames = [...new Set(compatibleOfferings.map((offering) => getProviderDisplayName(offering.provider)))];

  if (model.loaded !== false) {
    return {
      state: 'active',
      summary: 'Already loaded on active infrastructure.',
      actionLabel: 'MANAGE',
      actionTarget: '/instances',
    };
  }

  if (model.vault_status === 'testing') {
    return {
      state: 'deploying',
      summary: 'Provisioning or model load is already in progress.',
      actionLabel: 'VIEW NODES',
      actionTarget: '/instances',
    };
  }

  if (connectedProviders.length === 0) {
    return {
      state: 'setup',
      summary: 'No live provider is connected for this workspace yet.',
      actionLabel: 'SETUP PROVIDER',
      actionTarget: '/workspace',
    };
  }

  if (compatibleOfferings.length === 0) {
    return {
      state: 'capacity',
      summary: requiredMB
        ? `Needs about ${Math.ceil(requiredMB / 1024)}GB VRAM. No matching capacity is live right now.`
        : 'Provider capacity is connected, but no compatible inventory is live right now.',
      actionLabel: 'VIEW CAPACITY',
      actionTarget: '/instances',
    };
  }

  return {
    state: 'ready',
    summary: `Ready on ${compatibleOfferings.length} GPU config${compatibleOfferings.length === 1 ? '' : 's'} via ${providerNames.join(', ')}${cheapest ? ` from $${cheapest.cost_per_hour.toFixed(2)}/hr` : ''}.`,
    actionLabel: 'DEPLOY',
    actionTarget: `/instances?provision=true&model=${encodeURIComponent(model.id)}&from=models`,
  };
}

export function deriveModelServingOverview(
  model: Model,
  instances: Instance[],
  workers: Worker[] | undefined,
  deploymentAttempts: DeploymentAttemptRecord[],
): ModelServingOverview {
  const relatedInstances = instances.filter((instance) => (instance.models || []).includes(model.id));
  const relatedAttempts = deploymentAttempts
    .filter((attempt) =>
      (attempt.request.models || []).includes(model.id)
      || attempt.inference_verification?.model === model.id,
    )
    .map((attempt) => summarizeDeploymentAttempt(attempt, instances, workers));
  const latestAttempt = relatedAttempts[0] || null;
  const readinessList = relatedInstances.map((instance) => getInstanceReadiness(instance, workers));
  const anyServing = readinessList.some((readiness) => readiness.serving);
  const allServingVerified = readinessList.length > 0 && readinessList.every((readiness) => readiness.verified && readiness.serving);
  const latestVerification = latestAttempt?.attempt.inference_verification;

  if (relatedInstances.length === 0) {
    return {
      state: 'not_deployed',
      summary: latestAttempt?.readiness.label === 'REQUEST FAILED'
        ? latestAttempt.readiness.detail
        : 'No live deployment is currently serving this model.',
      badgeLabel: latestAttempt?.readiness.label === 'REQUEST FAILED' ? 'DEPLOY FAILED' : 'NOT DEPLOYED',
      badgeTone: latestAttempt?.readiness.label === 'REQUEST FAILED' ? 'error' : 'inactive',
      activeInstances: 0,
      latestAttempt,
      latestVerificationError: latestVerification?.error,
      verifiedAt: latestVerification?.verified_at,
      latestVerificationLatencyMs: latestVerification?.latency_ms,
    };
  }

  if (latestVerification?.status === 'passed' && anyServing) {
    return {
      state: 'serving_verified',
      summary: `${relatedInstances.length} instance${relatedInstances.length === 1 ? '' : 's'} currently host this model and the latest live inference check passed.`,
      badgeLabel: 'SERVING VERIFIED',
      badgeTone: '',
      activeInstances: relatedInstances.length,
      latestAttempt,
      verifiedAt: latestVerification.verified_at,
      latestVerificationLatencyMs: latestVerification.latency_ms,
    };
  }

  if (latestVerification?.status === 'failed' && anyServing) {
    return {
      state: 'serving_failed',
      summary: latestVerification.error || 'Runtime looks healthy, but the latest live inference check failed.',
      badgeLabel: 'INFERENCE FAILED',
      badgeTone: 'error',
      activeInstances: relatedInstances.length,
      latestAttempt,
      verifiedAt: latestVerification.verified_at,
      latestVerificationError: latestVerification.error,
    };
  }

  if (allServingVerified) {
    return {
      state: 'serving_unverified',
      summary: `${relatedInstances.length} instance${relatedInstances.length === 1 ? '' : 's'} are runtime-ready for this model. Run or wait for inference verification.`,
      badgeLabel: 'SERVING UNVERIFIED',
      badgeTone: 'warning',
      activeInstances: relatedInstances.length,
      latestAttempt,
    };
  }

  if (anyServing || readinessList.some((readiness) => readiness.label === 'MODEL LOADING' || readiness.label === 'PARTIAL READY')) {
    return {
      state: 'runtime_pending',
      summary: latestAttempt?.readiness.detail || 'Runtime is still converging for this model.',
      badgeLabel: 'RUNTIME PENDING',
      badgeTone: 'warning',
      activeInstances: relatedInstances.length,
      latestAttempt,
    };
  }

  return {
    state: 'degraded',
    summary: latestAttempt?.readiness.detail || 'This model is assigned to infrastructure, but it is not currently healthy enough to serve.',
    badgeLabel: 'DEGRADED',
    badgeTone: 'error',
    activeInstances: relatedInstances.length,
    latestAttempt,
  };
}

export function useModelsViewState({
  displayModels,
  offerings,
  providers,
  liveInstances,
  workers,
  deploymentAttempts,
  deferredQuery,
  activeTagFilter,
}: {
  displayModels: Model[];
  offerings: GPUOffering[] | undefined;
  providers: ProviderStatus[] | undefined;
  liveInstances: Instance[];
  workers: Worker[] | undefined;
  deploymentAttempts: DeploymentAttemptRecord[];
  deferredQuery: string;
  activeTagFilter: string | null;
}) {
  const allTags = useMemo(() => {
    const tagSet = new Set<string>();
    for (const model of displayModels) {
      for (const tag of model.tags || []) tagSet.add(tag);
    }
    return [...tagSet].sort();
  }, [displayModels]);

  const filtered = useMemo(() => displayModels.filter((model) => {
    if (activeTagFilter && !(model.tags || []).includes(activeTagFilter)) return false;
    if (!deferredQuery) return true;
    const q = deferredQuery.toLowerCase();
    return model.id.toLowerCase().includes(q)
      || model.family?.toLowerCase().includes(q)
      || model.owned_by?.toLowerCase().includes(q)
      || (model.quantization || '').toLowerCase().includes(q)
      || (model.tags || []).some((tag) => tag.toLowerCase().includes(q));
  }), [activeTagFilter, deferredQuery, displayModels]);

  const visibleProviders = useMemo(
    () => (providers || []).filter((provider) => isInventoryProviderType(provider.provider)),
    [providers],
  );

  const visibleOfferings = useMemo(
    () => (offerings || []).filter((offering) => isInventoryProviderType(offering.provider)),
    [offerings],
  );

  const modelOverviewByID = useMemo(() => {
    const map = new Map<string, ModelServingOverview>();
    for (const model of displayModels) {
      map.set(model.id, deriveModelServingOverview(model, liveInstances, workers, deploymentAttempts));
    }
    return map;
  }, [deploymentAttempts, displayModels, liveInstances, workers]);

  const modelRuntimeByID = useMemo(() => {
    const map = new Map<string, ReturnType<typeof deriveModelRuntimeDrilldown>>();
    for (const model of displayModels) {
      map.set(model.id, deriveModelRuntimeDrilldown(model.id, liveInstances, workers, deploymentAttempts));
    }
    return map;
  }, [deploymentAttempts, displayModels, liveInstances, workers]);

  const getDeployState = useCallback(
    (model: Model) => describeDeployReadiness(model, visibleOfferings, visibleProviders),
    [visibleOfferings, visibleProviders],
  );

  const getOverview = useCallback(
    (modelId: string) => modelOverviewByID.get(modelId)!,
    [modelOverviewByID],
  );

  const getRuntime = useCallback(
    (modelId: string) => modelRuntimeByID.get(modelId)!,
    [modelRuntimeByID],
  );

  const readyCount = useMemo(
    () => filtered.filter((model) => getDeployState(model).state === 'ready').length,
    [filtered, getDeployState],
  );

  const activeCount = useMemo(
    () => filtered.filter((model) => (modelOverviewByID.get(model.id)?.activeInstances || 0) > 0).length,
    [filtered, modelOverviewByID],
  );

  const servingVerifiedCount = useMemo(
    () => filtered.filter((model) => modelOverviewByID.get(model.id)?.state === 'serving_verified').length,
    [filtered, modelOverviewByID],
  );

  const recommendedModels = useMemo(
    () => RECOMMENDED_MODEL_IDS
      .map((id) => displayModels.find((model) => model.id === id))
      .filter((model): model is Model => Boolean(model)),
    [displayModels],
  );

  const connectedProviderCount = useMemo(
    () => visibleProviders.filter((provider) => provider.connected).length,
    [visibleProviders],
  );

  return {
    allTags,
    filtered,
    visibleProviders,
    visibleOfferings,
    getDeployState,
    getOverview,
    getRuntime,
    readyCount,
    activeCount,
    servingVerifiedCount,
    recommendedModels,
    connectedProviderCount,
  };
}
