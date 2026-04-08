import { useState, useEffect, useMemo, useRef, useCallback, useDeferredValue, type ReactNode } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import { GridRow, Cell, LabelText, StatusDot, Badge, ActionButton, ControlInput, ProgressBar } from '../components/shared';
import { InstancesSkeleton } from '../components/skeletons';
import type { Instance, GPUOffering, KnownGPUType, GPUType, ProviderStatus, ProviderType, VaultModel, Worker, ProvisionRequest } from '../types';
import { fetchWorkspaceProviderConfigs, sendChatCompletion } from '../lib/api';
import {
  getDeploymentRemediation,
  getDeploymentTimeline,
  summarizeDeploymentAttempt,
  type DeploymentAttemptRecord,
  type DeploymentAttemptSummary,
  type DeploymentRemediation,
} from '../lib/deploymentHistory';
import { getInstanceReadiness } from '../lib/instanceReadiness';
import { useDeploymentAttempts, useInstances, useMarkDeploymentAutoVerificationRequested, useOfferings, useProviders, useTerminateInstance, useStartInstance, useStopInstance, useProvisionInstance, useUpdateDeploymentVerification, useVaultModels, useWorkers } from '../hooks/useApi';
import { useIsMobile } from '../hooks/useIsMobile';
import { InstanceMobileCard } from '../components/InstanceMobileCard';
import { useAuthSession } from '../lib/auth-context';
import { getProviderDisplayName, isInventoryProviderType, isWorkspaceProviderType, WORKSPACE_PROVIDER_TYPES } from '../lib/providerInventory';
import { formatShortTimestamp, formatLatency } from '../lib/formatting';
import { formatGPUDisplayName, formatOfferingRegion, instanceStatusClass, instanceStatusLabel, providerStateBadge } from '../lib/labels';
import { DeploymentTimeline } from '../components/DeploymentTimeline';

const GPU_VRAM_GB: Record<KnownGPUType, number> = {
  RTX_4090: 24,
  RTX_4080: 16,
  A100_40GB: 40,
  A100_80GB: 80,
  H100: 80,
  L40S: 48,
};


const AUTO_VERIFY_DELAY_MS = 1500;
const RECOMMENDED_MODEL_IDS = [
  'Qwen/Qwen3-4B-Thinking-2507',
  'moonshotai/Kimi-K2.5-Instruct',
] as const;

type ModelDeploymentPreset = {
  label: string;
  detail: string;
  preferredProvider?: ProviderType;
  preferredGPUType: GPUType;
  preferredGPUCount: number;
};

const MODEL_DEPLOYMENT_PRESETS: Record<string, ModelDeploymentPreset> = {
  'Qwen/Qwen3-4B-Thinking-2507': {
    label: 'Budget Reasoning',
    detail: 'Starts on a single RTX 4090 when available and is the cheapest recommended reasoning trial.',
    preferredProvider: 'runpod',
    preferredGPUType: 'RTX_4090',
    preferredGPUCount: 1,
  },
  'moonshotai/Kimi-K2.5-Instruct': {
    label: 'High-Capacity',
    detail: 'Treat this as a large-cluster target. Prefer H100-class multi-GPU capacity and expect materially higher cost.',
    preferredProvider: 'runpod',
    preferredGPUType: 'H100',
    preferredGPUCount: 8,
  },
};

type ProvisionDraft = {
  name?: string;
  provider?: ProviderType;
  gpu_type?: GPUType;
  gpu_count?: number;
  spot_instance?: boolean;
  models?: string[];
};

type NodeIncident = {
  title: string;
  detail: string;
  tone: '' | 'warning' | 'error' | 'inactive';
};

type OfferingGroup = {
  key: string;
  provider: ProviderType;
  gpuType: GPUType;
  displayName?: string;
  regions: string[];
  counts: GPUOffering[];
  cheapestCostPerHour: number;
  totalAvailable: number;
};

type ProvisionStep = 'compute' | 'models' | 'review';

const PROVISION_FLOW: Array<{ id: ProvisionStep; label: string; caption: string }> = [
  { id: 'compute', label: 'Compute', caption: 'Choose GPU and size' },
  { id: 'models', label: 'Models', caption: 'Pick compatible models' },
  { id: 'review', label: 'Review', caption: 'Confirm and provision' },
];

function describeProvisioningState(configuredProviders: string[], providerStatuses: ProviderStatus[], offeringsCount: number) {
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

// Aliases for backwards-compatible call sites in this file
const getStatusClass = instanceStatusClass;
const getStatusLabel = instanceStatusLabel;
const formatAttemptTime = (value: string) => formatShortTimestamp(value) ?? value;
const formatVerificationLatency = formatLatency;

function findPresetOffering(
  offerings: GPUOffering[] | undefined,
  preset?: ModelDeploymentPreset,
): GPUOffering | null {
  if (!offerings || !preset) return null;

  const exact = offerings.find((offering) =>
    (!preset.preferredProvider || offering.provider === preset.preferredProvider) &&
    offering.gpu_type === preset.preferredGPUType &&
    offering.gpu_count === preset.preferredGPUCount,
  );
  if (exact) return exact;

  const sameGPU = offerings
    .filter((offering) =>
      (!preset.preferredProvider || offering.provider === preset.preferredProvider) &&
      offering.gpu_type === preset.preferredGPUType,
    )
    .sort((a, b) => a.cost_per_hour - b.cost_per_hour);
  return sameGPU[0] || null;
}

function presetCapacityWarning(preset?: ModelDeploymentPreset): string | null {
  if (!preset) return null;
  return `This preset currently needs ${preset.preferredGPUCount}x ${preset.preferredGPUType.replace('_', ' ')} or larger live capacity.`;
}

function deriveNodeIncident(
  instance: Instance,
  workers: Worker[] | undefined,
  summary: DeploymentAttemptSummary | null,
): NodeIncident | null {
  const readiness = getInstanceReadiness(instance, workers);
  const verification = summary?.attempt.inference_verification;

  if (verification?.status === 'failed') {
    return {
      title: 'INFERENCE CHECK FAILED',
      detail: verification.error
        ? `Latest verification failed on ${formatAttemptTime(verification.verified_at)}: ${verification.error}`
        : `Latest verification failed on ${formatAttemptTime(verification.verified_at)}.`,
      tone: 'error',
    };
  }

  if (instance.status === 'error') {
    return {
      title: 'PROVIDER INCIDENT',
      detail: instance.error || 'Provider reported a node error during startup or serving.',
      tone: 'error',
    };
  }

  switch (readiness.label) {
    case 'WORKER NOT CONNECTED':
    case 'WORKER MISSING':
    case 'WORKER UNHEALTHY':
    case 'WORKER DEGRADED':
      return {
        title: readiness.label,
        detail: readiness.detail,
        tone: readiness.tone,
      };
    case 'MODEL LOADING':
    case 'MODEL LOAD DELAY':
    case 'PARTIAL READY':
      return {
        title: 'MODEL RUNTIME ISSUE',
        detail: readiness.detail,
        tone: readiness.tone,
      };
    case 'HEARTBEAT STALE':
    case 'SERVING UNVERIFIED':
      return {
        title: 'VERIFICATION STALE',
        detail: readiness.detail,
        tone: readiness.tone,
      };
    default:
      return null;
  }
}

function useInstanceActions(instance: Instance) {
  const terminateMutation = useTerminateInstance();
  const startMutation = useStartInstance();
  const stopMutation = useStopInstance();
  const isLoading = terminateMutation.isPending || startMutation.isPending || stopMutation.isPending;

  const handleStart = async () => {
    try {
      await startMutation.mutateAsync(instance.id);
      toast.success('Instance started');
    } catch (err) {
      console.error('Failed to start instance', err);
      toast.error(err instanceof Error ? err.message : 'Failed to start');
    }
  };

  const handleStop = async () => {
    try {
      await stopMutation.mutateAsync(instance.id);
      toast.success('Instance stopped');
    } catch (err) {
      console.error('Failed to stop instance', err);
      toast.error(err instanceof Error ? err.message : 'Failed to stop');
    }
  };

  const handleTerminate = async () => {
    if (!confirm('Terminate this instance?')) return;
    try {
      await terminateMutation.mutateAsync(instance.id);
      toast.success('Terminated');
    } catch (err) {
      console.error('Failed to terminate instance', err);
      toast.error('Failed to terminate');
    }
  };

  return { isLoading, handleStart, handleStop, handleTerminate };
}

function InstanceActions({
  instance,
  compact = false,
  incidentActions,
}: {
  instance: Instance;
  compact?: boolean;
  incidentActions?: ReactNode;
}) {
  const { isLoading, handleStart, handleStop, handleTerminate } = useInstanceActions(instance);
  const buttonStyle = compact ? { fontSize: '0.65rem' } : { fontSize: '0.65rem', marginRight: '1rem' };

  return (
    <>
      {incidentActions}
      {instance.status === 'stopped' && (
        <ActionButton style={buttonStyle} disabled={isLoading} onClick={handleStart}>START</ActionButton>
      )}
      {instance.status === 'running' && (
        <ActionButton style={buttonStyle} disabled={isLoading} onClick={handleStop}>STOP</ActionButton>
      )}
      {instance.status !== 'terminating' && instance.status !== 'terminated' && (
        <ActionButton
          variant="destructive"
          style={{ fontSize: '0.65rem' }}
          disabled={isLoading}
          onClick={handleTerminate}
        >
          TERMINATE
        </ActionButton>
      )}
    </>
  );
}

function InstanceRow({
  instance,
  workers,
  highlighted,
  incident,
  incidentActions,
}: {
  instance: Instance;
  workers: Worker[] | undefined;
  highlighted?: boolean;
  incident?: NodeIncident | null;
  incidentActions?: ReactNode;
}) {
  const statusClass = getStatusClass(instance.status);
  const statusLabel = getStatusLabel(instance.status);
  const readiness = getInstanceReadiness(instance, workers);

  return (
    <tr id={`instance-row-${instance.id}`} style={{ borderBottom: '1px solid #EEEEEC', background: incident?.tone === 'warning' ? 'rgba(249, 168, 37, 0.06)' : highlighted ? 'rgba(244, 242, 238, 0.7)' : 'transparent' }}>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle' }}>
        <div className="mono">{instance.name || instance.id.slice(0, 16)}</div>
        <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginTop: 2 }}>
          {instance.gpu_count}x {formatGPUDisplayName(instance.gpu_type)}
          {instance.models && instance.models.length > 0 && (
            <> &middot; {instance.models[0].split('/').pop()}</>
          )}
        </div>
      </td>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: '0.85rem' }}>
          <StatusDot tone={statusClass as 'success' | 'warning' | 'error' | 'inactive' | undefined} />
          {statusLabel}
        </div>
        <div style={{ marginTop: '0.45rem', display: 'flex', alignItems: 'center', gap: '0.45rem', flexWrap: 'wrap' }}>
          <Badge tone={readiness.tone || undefined}>{readiness.label}</Badge>
        </div>
        <div style={{ marginTop: '0.35rem', fontSize: '0.75rem', color: 'var(--text-secondary)', lineHeight: 1.5, maxWidth: '22rem' }}>
          {readiness.detail}
        </div>
        {incident && (
          <div style={{ marginTop: '0.65rem', maxWidth: '22rem' }}>
            <Badge tone={incident.tone || undefined}>{incident.title}</Badge>
            <div style={{ marginTop: '0.35rem', fontSize: '0.75rem', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
              {incident.detail}
            </div>
          </div>
        )}
      </td>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle' }}>
        <div className="mono">${instance.cost_per_hour.toFixed(2)}/hr</div>
      </td>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle' }}>
        {instance.public_ip ? (
          <div className="mono" style={{ fontSize: '0.8rem' }}>{instance.public_ip}</div>
        ) : (
          <span style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>-</span>
        )}
      </td>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle', textAlign: 'right' }}>
        <InstanceActions instance={instance} incidentActions={incidentActions} />
      </td>
    </tr>
  );
}

export function ProvisionModal({ isOpen, onClose, onProvisioned, onProvisionFailed, onOpenWorkspace, offerings, preselectedModel, initialDraft, providerStatuses, configuredProviders }: {
  isOpen: boolean;
  onClose: () => void;
  onProvisioned: (
    instance: Instance,
    request: ProvisionRequest & { name?: string },
    selectedModelName?: string,
  ) => void;
  onProvisionFailed: (
    request: ProvisionRequest & { name?: string },
    failureReason: string,
  ) => void;
  onOpenWorkspace: () => void;
  offerings: GPUOffering[] | undefined;
  preselectedModel?: string | null;
  initialDraft?: ProvisionDraft | null;
  providerStatuses: ProviderStatus[];
  configuredProviders: string[];
}) {
  const [step, setStep] = useState<ProvisionStep>('compute');
  const [selectedGPU, setSelectedGPU] = useState<string>('');
  const [name, setName] = useState('');
  const [spotInstance, setSpotInstance] = useState(false);
  const [selectedModels, setSelectedModels] = useState<string[]>([]);
  const [modelSearch, setModelSearch] = useState('');
  const provisionMutation = useProvisionInstance();
  const { data: vaultModels } = useVaultModels({ status: 'available' });
  const initializedDraftRef = useRef<string | null>(null);
  const deferredModelSearch = useDeferredValue(modelSearch);

  const getOfferingKey = (o: GPUOffering) =>
    `${o.provider}-${o.provider_gpu_type_id || o.gpu_type}-${o.gpu_count}-${o.memory_gb}-${o.vcpu}-${o.region || 'global'}`;

  const getOfferingGroupKey = (o: GPUOffering) =>
    `${o.provider}-${o.provider_gpu_type_id || o.gpu_type}-${o.display_name || o.gpu_type}`;

  // Deduplicate offerings
  const dedupedOfferings = useMemo(
    () => offerings ? Array.from(
      offerings.reduce((map, o) => {
        const key = getOfferingKey(o);
        const existing = map.get(key);
        if (!existing || o.cost_per_hour < existing.cost_per_hour) map.set(key, o);
        return map;
      }, new Map<string, GPUOffering>()).values()
    ) : undefined,
    [offerings],
  );
  const provisioningState = describeProvisioningState(configuredProviders, providerStatuses, dedupedOfferings?.length ?? 0);

  const selectedOffering = dedupedOfferings?.find(o => getOfferingKey(o) === selectedGPU);
  const selectedGPUVram = selectedOffering ? (selectedOffering.memory_gb || GPU_VRAM_GB[selectedOffering.gpu_type as KnownGPUType]) : undefined;
  const groupedOfferings = useMemo(() => {
    if (!dedupedOfferings) return [];

    const groups = Array.from(
      dedupedOfferings.reduce((map, offering) => {
        const key = getOfferingGroupKey(offering);
        const existing = map.get(key);
        if (!existing) {
          map.set(key, {
            key,
            provider: offering.provider,
            gpuType: offering.gpu_type,
            displayName: offering.display_name,
            regions: offering.region ? [offering.region] : ['global'],
            counts: [offering],
            cheapestCostPerHour: offering.cost_per_hour,
            totalAvailable: offering.available,
          } satisfies OfferingGroup);
          return map;
        }

        existing.counts.push(offering);
        existing.cheapestCostPerHour = Math.min(existing.cheapestCostPerHour, offering.cost_per_hour);
        existing.totalAvailable += offering.available;
        if (offering.region && !existing.regions.includes(offering.region)) {
          existing.regions.push(offering.region);
        }
        return map;
      }, new Map<string, OfferingGroup>()).values(),
    );

    return groups
      .map((group) => ({
        ...group,
        counts: [...group.counts].sort((left, right) => left.gpu_count - right.gpu_count || left.cost_per_hour - right.cost_per_hour),
      }))
      .sort((left, right) => left.cheapestCostPerHour - right.cheapestCostPerHour);
  }, [dedupedOfferings]);

  const allVaultModels = vaultModels?.models;
  const pinnedModelRecord = useMemo(
    () => allVaultModels?.find((model) => model.source_uri === preselectedModel),
    [allVaultModels, preselectedModel],
  );
  const selectedPreset = useMemo(
    () => (preselectedModel ? MODEL_DEPLOYMENT_PRESETS[preselectedModel] : undefined),
    [preselectedModel],
  );
  const compatibleModels = useMemo(() => {
    return allVaultModels?.filter((m: VaultModel) => {
      if (!selectedGPUVram) return true;
      return m.vram_required <= selectedGPUVram * 1024;
    });
  }, [allVaultModels, selectedGPUVram]);
  const pinnedModelCompatibleOfferings = useMemo(() => {
    if (!dedupedOfferings) return [];
    if (!pinnedModelRecord?.vram_required) return dedupedOfferings;

    return dedupedOfferings.filter((offering) => {
      const vramGB = offering.memory_gb || GPU_VRAM_GB[offering.gpu_type as KnownGPUType] || 0;
      return vramGB * 1024 >= pinnedModelRecord.vram_required;
    });
  }, [dedupedOfferings, pinnedModelRecord]);
  const recommendedOffering = useMemo(
    () => pinnedModelCompatibleOfferings.reduce<GPUOffering | null>((best, offering) => {
      if (!best || offering.cost_per_hour < best.cost_per_hour) return offering;
      return best;
    }, null),
    [pinnedModelCompatibleOfferings],
  );
  const presetOffering = useMemo(
    () => findPresetOffering(dedupedOfferings, selectedPreset),
    [dedupedOfferings, selectedPreset],
  );
  const primarySelectedModelRecord = useMemo(
    () => allVaultModels?.find((model) => model.source_uri === selectedModels[0]),
    [allVaultModels, selectedModels],
  );
  const selectedModelEntries = useMemo(
    () => selectedModels
      .map((sourceUri) => allVaultModels?.find((model) => model.source_uri === sourceUri))
      .filter((model): model is VaultModel => Boolean(model)),
    [allVaultModels, selectedModels],
  );
  const filteredCompatibleModels = useMemo(() => {
    const query = deferredModelSearch.trim().toLowerCase();
    if (!query) return compatibleModels || [];

    return (compatibleModels || []).filter((model) => {
      const haystack = [
        model.name,
        model.source_uri,
        model.parameters,
        model.family,
        model.quantization,
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
      return haystack.includes(query);
    });
  }, [compatibleModels, deferredModelSearch]);
  const filteredRecommendedModels = useMemo(
    () => filteredCompatibleModels.filter((model) => RECOMMENDED_MODEL_IDS.includes(model.source_uri as typeof RECOMMENDED_MODEL_IDS[number])),
    [filteredCompatibleModels],
  );
  const filteredCatalogModels = useMemo(
    () => filteredCompatibleModels.filter((model) => !RECOMMENDED_MODEL_IDS.includes(model.source_uri as typeof RECOMMENDED_MODEL_IDS[number])),
    [filteredCompatibleModels],
  );
  const inventorySnapshot = useMemo(() => {
    if (!dedupedOfferings?.length) return null;

    const providers = new Set(dedupedOfferings.map((offering) => getProviderDisplayName(offering.provider)));
    const regions = new Set(dedupedOfferings.map((offering) => offering.region || 'global'));
    const lowestCost = dedupedOfferings.reduce<number | null>((best, offering) => {
      if (best == null || offering.cost_per_hour < best) return offering.cost_per_hour;
      return best;
    }, null);

    return {
      providerCount: providers.size,
      gpuFamilyCount: groupedOfferings.length,
      regionCount: regions.size,
      availableNow: dedupedOfferings.reduce((sum, offering) => sum + offering.available, 0),
      lowestCost,
    };
  }, [dedupedOfferings, groupedOfferings]);
  const stepIndex = PROVISION_FLOW.findIndex((entry) => entry.id === step);
  const canContinueFromCompute = Boolean(selectedOffering) && Boolean(dedupedOfferings?.length) && !provisioningState;
  const primaryActionLabel = step === 'compute'
    ? 'Continue to models'
    : step === 'models'
      ? selectedModels.length > 0
        ? 'Continue to review'
        : 'Continue without model'
      : (provisionMutation.isPending ? 'Provisioning...' : 'Provision node');

  useEffect(() => {
    if (!isOpen) {
      initializedDraftRef.current = null;
      setStep('compute');
      setModelSearch('');
      return;
    }

    const initKey = JSON.stringify({
      draft: initialDraft || null,
      preselectedModel: preselectedModel || null,
    });
    if (initializedDraftRef.current === initKey) return;

    setName(initialDraft?.name || '');
    setSpotInstance(Boolean(initialDraft?.spot_instance));
    setSelectedModels(initialDraft?.models || (preselectedModel ? [preselectedModel] : []));

    if (!dedupedOfferings || !initialDraft?.gpu_type) {
      setSelectedGPU('');
      return;
    }

    const matchingOffering = dedupedOfferings.find((offering) =>
      (!initialDraft.provider || offering.provider === initialDraft.provider) &&
      offering.gpu_type === initialDraft.gpu_type &&
      offering.gpu_count === (initialDraft.gpu_count || 1),
    );

    setSelectedGPU(matchingOffering ? getOfferingKey(matchingOffering) : '');
    initializedDraftRef.current = initKey;
  }, [dedupedOfferings, initialDraft, isOpen, preselectedModel]);

  useEffect(() => {
    if (!isOpen) return;
    setStep('compute');
    setModelSearch('');
  }, [initialDraft, isOpen, preselectedModel]);

  useEffect(() => {
    if (!isOpen) return;

    const compatibleSources = new Set((compatibleModels || []).map((model) => model.source_uri));
    setSelectedModels((prev) => {
      const next = prev.filter((model) => compatibleSources.has(model));

      const normalizedNext =
        preselectedModel && compatibleSources.has(preselectedModel) && !next.includes(preselectedModel)
          ? [preselectedModel, ...next]
          : next;

      if (
        normalizedNext.length === prev.length &&
        normalizedNext.every((model, index) => model === prev[index])
      ) {
        return prev;
      }

      return normalizedNext;
    });
  }, [compatibleModels, preselectedModel, isOpen]);

  useEffect(() => {
    if (!isOpen || !preselectedModel || selectedGPU) return;
    const targetOffering = presetOffering || recommendedOffering;
    if (!targetOffering) return;
    setSelectedGPU(getOfferingKey(targetOffering));
  }, [isOpen, preselectedModel, presetOffering, recommendedOffering, selectedGPU]);

  useEffect(() => {
    if (!isOpen || step === 'compute' || selectedGPU) return;
    setStep('compute');
  }, [isOpen, selectedGPU, step]);

  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  const toggleModel = (sourceUri: string) => {
    setSelectedModels(prev => prev.includes(sourceUri) ? prev.filter(id => id !== sourceUri) : [...prev, sourceUri]);
  };

  const jumpToStep = (targetStep: ProvisionStep) => {
    if (targetStep === 'compute') {
      setStep('compute');
      return;
    }
    if (targetStep === 'models' && canContinueFromCompute) {
      setStep('models');
      return;
    }
    if (targetStep === 'review' && canContinueFromCompute) {
      setStep('review');
    }
  };

  const handlePrimaryAction = () => {
    if (step === 'compute') {
      if (canContinueFromCompute) setStep('models');
      return;
    }
    if (step === 'models') {
      setStep('review');
      return;
    }
    void handleProvision();
  };

  const handleProvision = async () => {
    if (!selectedOffering) return;
    const request = {
      name: name || 'infera-worker',
      provider: selectedOffering.provider,
      gpu_type: selectedOffering.gpu_type,
      provider_gpu_type_id: selectedOffering.provider_gpu_type_id,
      gpu_count: selectedOffering.gpu_count,
      spot_instance: spotInstance,
      models: selectedModels.length > 0 ? selectedModels : undefined,
      selected_model_name: selectedModels.length === 1 ? (primarySelectedModelRecord?.name || selectedModels[0].split('/').pop()) : undefined,
    } as const;

    try {
      const provisionedInstance = await provisionMutation.mutateAsync(request);
      onProvisioned(
        provisionedInstance,
        request,
        selectedModels.length === 1 ? (primarySelectedModelRecord?.name || selectedModels[0].split('/').pop()) : undefined,
      );
      toast.success(
        selectedModels.length > 0
          ? `Provisioning ${selectedModels.length === 1 ? (primarySelectedModelRecord?.name || selectedModels[0].split('/').pop()) : `${selectedModels.length} models`} on ${formatGPUDisplayName(selectedOffering.gpu_type, selectedOffering.display_name)}`
          : `Provisioning node on ${formatGPUDisplayName(selectedOffering.gpu_type, selectedOffering.display_name)}`,
      );
      onClose();
      setName('');
      setSelectedGPU('');
      setSelectedModels([]);
      setSpotInstance(false);
    } catch (error) {
      const failureReason = error instanceof Error ? error.message : 'Provider request failed before an instance was created.';
      onProvisionFailed(request, failureReason);
      toast.error(failureReason);
    }
  };

  if (!isOpen) return null;

  return (
    <>
      <div className="provision-modal-overlay" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="provision-modal-title"
        className="provision-modal-shell"
      >
        <div className="provision-modal-header">
          <div className="provision-modal-heading">
            <LabelText as="div">PROVISION NEW NODE</LabelText>
            <h2 id="provision-modal-title" className="provision-modal-title">Provision a node</h2>
            <p className="provision-modal-description">
              Choose compute first, then review the models that fit that hardware before confirming the deployment.
            </p>
          </div>
          <div className="provision-stepper" role="tablist" aria-label="Provision node steps">
            {PROVISION_FLOW.map((flowStep, index) => {
              const isActive = flowStep.id === step;
              const isAvailable = flowStep.id === 'compute' || canContinueFromCompute;
              const isComplete = index < stepIndex;

              return (
                <button
                  key={flowStep.id}
                  type="button"
                  role="tab"
                  aria-selected={isActive}
                  className={`provision-step ${isActive ? 'active' : ''} ${isComplete ? 'complete' : ''}`}
                  onClick={() => jumpToStep(flowStep.id)}
                  disabled={!isAvailable}
                >
                  <span className="provision-step-index">0{index + 1}</span>
                  <span className="provision-step-copy">
                    <span>{flowStep.label}</span>
                    <span>{flowStep.caption}</span>
                  </span>
                </button>
              );
            })}
          </div>
        </div>

        <div className="provision-modal-body">
          {step === 'compute' && (
            <div className="provision-stage">
              <div className="provision-stage-main">
                {pinnedModelRecord && (
                  <div className="provision-context-card">
                    <div className="provision-context-header">
                      <div>
                        <LabelText as="div">PINNED MODEL CONTEXT</LabelText>
                        <div className="provision-context-title">{pinnedModelRecord.name}</div>
                        <div className="mono provision-context-source">{pinnedModelRecord.source_uri}</div>
                      </div>
                      <div className="provision-context-badges">
                        {selectedPreset && <Badge>{selectedPreset.label}</Badge>}
                        {pinnedModelRecord.parameters && <Badge>{pinnedModelRecord.parameters}</Badge>}
                        {pinnedModelRecord.quantization && <Badge>{pinnedModelRecord.quantization}</Badge>}
                        {pinnedModelRecord.vram_required ? <Badge mono>{Math.ceil(pinnedModelRecord.vram_required / 1024)}GB VRAM</Badge> : null}
                      </div>
                    </div>
                    <div className="provision-context-copy">
                      {selectedPreset ? `${selectedPreset.detail} ` : ''}
                      {pinnedModelCompatibleOfferings.length > 0
                        ? `${presetOffering ? `The preferred preset maps to ${presetOffering.gpu_count}x ${formatGPUDisplayName(presetOffering.gpu_type, presetOffering.display_name)} on ${getProviderDisplayName(presetOffering.provider)}. ` : ''}This model fits ${pinnedModelCompatibleOfferings.length} available GPU option${pinnedModelCompatibleOfferings.length === 1 ? '' : 's'}${recommendedOffering ? `, starting at $${recommendedOffering.cost_per_hour.toFixed(2)}/hr.` : '.'}`
                        : 'No live GPU option currently satisfies the recorded VRAM requirement for this model.'}
                    </div>
                    {selectedPreset && pinnedModelCompatibleOfferings.length === 0 && (
                      <div className="provision-inline-warning">
                        <LabelText as="div">CAPACITY GAP</LabelText>
                        <div>{presetCapacityWarning(selectedPreset)} Choose a larger configuration once inventory appears, or switch to a smaller reasoning model.</div>
                      </div>
                    )}
                  </div>
                )}

                <div className="provision-section">
                  <LabelText as="div">STEP 1</LabelText>
                  <div className="provision-section-title">Choose the GPU family and node size</div>
                  <div className="provision-section-copy">
                    Start with the hardware. The next step will narrow the catalog to models that fit the VRAM budget you choose here.
                  </div>
                </div>

                {provisioningState ? (
                  <div className="provision-empty-state">
                    <div className="provision-empty-title">{provisioningState.title}</div>
                    <div className="provision-empty-copy">{provisioningState.detail}</div>
                    <div className="help-actions">
                      <ActionButton onClick={onOpenWorkspace}>OPEN WORKSPACE</ActionButton>
                      <ActionButton onClick={onClose}>CANCEL</ActionButton>
                    </div>
                  </div>
                ) : (
                  <>
                    {inventorySnapshot && (
                      <div className="provision-metric-strip" aria-label="Live inventory snapshot">
                        <div className="provision-metric-card">
                          <LabelText as="div">LIVE SOURCES</LabelText>
                          <div className="provision-metric-value">{inventorySnapshot.providerCount}</div>
                          <div className="provision-metric-copy">Connected inventory providers</div>
                        </div>
                        <div className="provision-metric-card">
                          <LabelText as="div">GPU FAMILIES</LabelText>
                          <div className="provision-metric-value">{inventorySnapshot.gpuFamilyCount}</div>
                          <div className="provision-metric-copy">Distinct compute families</div>
                        </div>
                        <div className="provision-metric-card">
                          <LabelText as="div">REGIONS</LabelText>
                          <div className="provision-metric-value">{inventorySnapshot.regionCount}</div>
                          <div className="provision-metric-copy">Capacity footprints visible now</div>
                        </div>
                        <div className="provision-metric-card">
                          <LabelText as="div">STARTING RATE</LabelText>
                          <div className="provision-metric-value mono">
                            {inventorySnapshot.lowestCost != null ? `$${inventorySnapshot.lowestCost.toFixed(2)}/hr` : '—'}
                          </div>
                          <div className="provision-metric-copy">{inventorySnapshot.availableNow} slots reported live</div>
                        </div>
                      </div>
                    )}

                    <div className="gpu-choice-grid">
                      {groupedOfferings.map((group) => {
                        const selectedGroupOffering = group.counts.find((offering) => getOfferingKey(offering) === selectedGPU) || null;
                        const activeOffering = selectedGroupOffering || group.counts[0];
                        const perGpuMemoryGB = Math.max(1, Math.round((activeOffering.memory_gb || 0) / Math.max(activeOffering.gpu_count || 1, 1)));

                        return (
                          <div key={group.key} className={`gpu-choice-card ${selectedGroupOffering ? 'selected' : ''}`}>
                            <div className="gpu-choice-card-header">
                              <div>
                                <div className="gpu-choice-title">{formatGPUDisplayName(group.gpuType, group.displayName)}</div>
                              <div className="gpu-choice-meta">
                                <span>{perGpuMemoryGB}GB each</span>
                                  <span>{group.regions.length === 1 ? formatOfferingRegion(group.regions[0], group.provider) : `${group.regions.length} regions`}</span>
                                  <span>{group.totalAvailable} available</span>
                              </div>
                              </div>
                              <Badge>{getProviderDisplayName(group.provider)}</Badge>
                            </div>
                            <div className="gpu-choice-price">
                              <span className="mono">${activeOffering.cost_per_hour.toFixed(2)}</span>
                              <span>/hr starting</span>
                            </div>
                            <div className="gpu-choice-detail">
                              {pinnedModelRecord?.vram_required
                                ? `${Math.ceil(pinnedModelRecord.vram_required / 1024)}GB model requirement ${((activeOffering.memory_gb || 0) * 1024) >= pinnedModelRecord.vram_required ? 'fits this size.' : 'needs a larger size.'}`
                                : `${group.counts.length} size option${group.counts.length === 1 ? '' : 's'} available for this GPU family.`}
                            </div>
                            <div className="gpu-choice-counts">
                              {group.counts.map((offering) => {
                                const offeringKey = getOfferingKey(offering);
                                const isOfferingSelected = selectedGPU === offeringKey;

                                return (
                                  <button
                                    key={offeringKey}
                                  type="button"
                                  className={`gpu-count-chip ${isOfferingSelected ? 'active' : ''}`}
                                  onClick={() => setSelectedGPU(offeringKey)}
                                >
                                    {offering.gpu_count}x GPU · {formatOfferingRegion(offering.region, offering.provider)}
                                    <span className="mono">${offering.cost_per_hour.toFixed(2)}/hr</span>
                                  </button>
                                );
                              })}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  </>
                )}
              </div>

              <aside className="provision-sidebar">
                <LabelText as="div">CURRENT SELECTION</LabelText>
                {selectedOffering ? (
                  <>
                    <div className="provision-sidebar-title">{formatGPUDisplayName(selectedOffering.gpu_type, selectedOffering.display_name)}</div>
                    <div className="provision-sidebar-copy">
                      {selectedOffering.gpu_count}x GPU on {getProviderDisplayName(selectedOffering.provider)} · {selectedOffering.memory_gb}GB total VRAM · {formatOfferingRegion(selectedOffering.region, selectedOffering.provider)}
                    </div>
                    <div className="provision-sidebar-stat mono">${selectedOffering.cost_per_hour.toFixed(2)}/hr</div>
                  </>
                ) : (
                  <div className="provision-sidebar-copy">
                    Choose a GPU size to unlock compatible model recommendations and the final deployment review.
                  </div>
                )}
                {pinnedModelRecord && (
                  <div className="provision-sidebar-block">
                    <LabelText as="div">MODEL REQUIREMENT</LabelText>
                    <div className="provision-sidebar-copy">
                      {pinnedModelRecord.name} targets roughly {Math.ceil((pinnedModelRecord.vram_required || 0) / 1024)}GB VRAM.
                    </div>
                  </div>
                )}
              </aside>
            </div>
          )}

          {step === 'models' && (
            <div className="provision-stage">
              <div className="provision-stage-main">
                <div className="provision-section">
                  <LabelText as="div">STEP 2</LabelText>
                  <div className="provision-section-title">Pick models that fit the selected GPU</div>
                  <div className="provision-section-copy">
                    Compatible models are filtered by the VRAM available on the selected node. Leave this step empty if you only want raw capacity first.
                  </div>
                </div>

                <label className="provision-search">
                  <LabelText>SEARCH MODELS</LabelText>
                  <ControlInput
                    type="text"
                    placeholder="Search by model name, family, or size..."
                    value={modelSearch}
                    onChange={(event) => setModelSearch(event.target.value)}
                  />
                </label>

                <div className="provision-metric-strip compact" aria-label="Model fit summary">
                  <div className="provision-metric-card">
                    <LabelText as="div">VRAM</LabelText>
                    <div className="provision-metric-value mono">{selectedOffering?.memory_gb ?? 0}GB</div>
                    <div className="provision-metric-copy">Total memory on this node</div>
                  </div>
                  <div className="provision-metric-card">
                    <LabelText as="div">COMPATIBLE</LabelText>
                    <div className="provision-metric-value">{filteredCompatibleModels.length}</div>
                    <div className="provision-metric-copy">Models fit the current node</div>
                  </div>
                  <div className="provision-metric-card">
                    <LabelText as="div">SELECTED</LabelText>
                    <div className="provision-metric-value">{selectedModels.length}</div>
                    <div className="provision-metric-copy">Models will preload on provision</div>
                  </div>
                </div>

                {filteredCompatibleModels.length === 0 ? (
                  <div className="provision-empty-state">
                    <div className="provision-empty-title">No compatible models for this GPU size</div>
                    <div className="provision-empty-copy">
                      Try a larger GPU configuration, or continue without preloading a model if you only need the node online first.
                    </div>
                    <div className="help-actions">
                      <ActionButton onClick={() => setStep('compute')}>BACK TO GPU CHOICE</ActionButton>
                    </div>
                  </div>
                ) : (
                  <>
                    {filteredRecommendedModels.length > 0 && (
                      <div className="provision-model-group">
                        <LabelText as="div">RECOMMENDED QUICK PICKS</LabelText>
                        <div className="provision-model-list">
                          {filteredRecommendedModels.map((model) => {
                            const isSelected = selectedModels.includes(model.source_uri);
                            return (
                              <button
                                key={`recommended-${model.id}`}
                                type="button"
                                className={`provision-model-card ${isSelected ? 'selected' : ''}`}
                                onClick={() => toggleModel(model.source_uri)}
                              >
                                <div className="provision-model-copy">
                                  <div className="provision-model-title">{model.name}</div>
                                  <div className="mono provision-model-source">{model.source_uri}</div>
                                </div>
                                <div className="provision-model-meta">
                                  {model.parameters && <Badge>{model.parameters}</Badge>}
                                  {model.quantization && <Badge>{model.quantization}</Badge>}
                                  {model.vram_required ? <Badge mono>{Math.ceil(model.vram_required / 1024)}GB VRAM</Badge> : null}
                                  {model.source_uri === preselectedModel ? <Badge>PINNED</Badge> : null}
                                  {isSelected ? <Badge>SELECTED</Badge> : null}
                                </div>
                              </button>
                            );
                          })}
                        </div>
                      </div>
                    )}

                    <div className="provision-model-group">
                      <LabelText as="div">COMPATIBLE MODEL LIBRARY</LabelText>
                      <div className="provision-model-list">
                        {filteredCatalogModels.map((model) => {
                          const isSelected = selectedModels.includes(model.source_uri);
                          return (
                            <button
                              key={model.id}
                              type="button"
                              className={`provision-model-card ${isSelected ? 'selected' : ''}`}
                              onClick={() => toggleModel(model.source_uri)}
                            >
                              <div className="provision-model-copy">
                                <div className="provision-model-title">{model.name}</div>
                                <div className="mono provision-model-source">{model.source_uri}</div>
                              </div>
                              <div className="provision-model-meta">
                                {model.parameters && <Badge>{model.parameters}</Badge>}
                                {model.quantization && <Badge>{model.quantization}</Badge>}
                                {model.vram_required ? <Badge mono>{Math.ceil(model.vram_required / 1024)}GB VRAM</Badge> : null}
                                {isSelected ? <Badge>SELECTED</Badge> : null}
                              </div>
                            </button>
                          );
                        })}
                      </div>
                    </div>
                  </>
                )}
              </div>

              <aside className="provision-sidebar">
                <LabelText as="div">COMPUTE SUMMARY</LabelText>
                {selectedOffering && (
                  <>
                    <div className="provision-sidebar-title">{formatGPUDisplayName(selectedOffering.gpu_type, selectedOffering.display_name)}</div>
                    <div className="provision-sidebar-copy">
                      {selectedOffering.gpu_count}x GPU · {selectedOffering.memory_gb}GB total VRAM · {getProviderDisplayName(selectedOffering.provider)}
                    </div>
                    <div className="provision-sidebar-stat mono">${selectedOffering.cost_per_hour.toFixed(2)}/hr</div>
                  </>
                )}
                <div className="provision-sidebar-block">
                  <LabelText as="div">SELECTED MODELS</LabelText>
                  {selectedModelEntries.length > 0 ? (
                    <div className="provision-selected-list">
                      {selectedModelEntries.map((model) => (
                        <div key={`selected-${model.id}`} className="provision-selected-item">
                          <span>{model.name}</span>
                          {model.parameters && <Badge>{model.parameters}</Badge>}
                        </div>
                      ))}
                    </div>
                  ) : (
                    <div className="provision-sidebar-copy">
                      No models selected yet. You can continue without preloading and attach models after the node is online.
                    </div>
                  )}
                </div>
              </aside>
            </div>
          )}

          {step === 'review' && (
            <div className="provision-stage">
              <div className="provision-stage-main">
                <div className="provision-section">
                  <LabelText as="div">STEP 3</LabelText>
                  <div className="provision-section-title">Review deployment details</div>
                  <div className="provision-section-copy">
                    Confirm the node name and cost posture, then provision the node with the selected compute and model bundle.
                  </div>
                </div>

                <div className="provision-review-grid">
                  <label className="provision-form-field">
                    <LabelText>INSTANCE NAME</LabelText>
                    <ControlInput
                      type="text"
                      value={name}
                      onChange={(event) => setName(event.target.value)}
                      placeholder="infera-worker"
                    />
                    <span className="provision-helper-text">This label appears in the node inventory and deployment history.</span>
                  </label>

                  <label className="provision-toggle">
                    <input type="checkbox" checked={spotInstance} onChange={(event) => setSpotInstance(event.target.checked)} />
                    <span>
                      <strong>Use spot capacity</strong>
                      <span>Lower hourly cost, but the node can be interrupted by the provider.</span>
                    </span>
                  </label>
                </div>

                <div className="provision-review-block">
                  <LabelText as="div">WHAT HAPPENS NEXT</LabelText>
                  <div className="provision-review-copy">
                    The provider request is submitted first, then the node appears in deployment history while the worker connects and models load. If you selected models, the platform will track inference verification after the node becomes ready.
                  </div>
                </div>
              </div>

              <aside className="provision-sidebar">
                <LabelText as="div">DEPLOYMENT SUMMARY</LabelText>
                {selectedOffering && (
                  <>
                    <div className="provision-sidebar-title">{formatGPUDisplayName(selectedOffering.gpu_type, selectedOffering.display_name)}</div>
                    <div className="provision-sidebar-copy">
                      {selectedOffering.gpu_count}x GPU · {selectedOffering.memory_gb}GB total VRAM · {getProviderDisplayName(selectedOffering.provider)} · {formatOfferingRegion(selectedOffering.region, selectedOffering.provider)}
                    </div>
                    <div className="provision-sidebar-stat mono">${selectedOffering.cost_per_hour.toFixed(2)}/hr</div>
                  </>
                )}
                <div className="provision-sidebar-block">
                  <LabelText as="div">MODELS</LabelText>
                  {selectedModelEntries.length > 0 ? (
                    <div className="provision-selected-list">
                      {selectedModelEntries.map((model) => (
                        <div key={`review-${model.id}`} className="provision-selected-item">
                          <span>{model.name}</span>
                          {model.parameters && <Badge>{model.parameters}</Badge>}
                        </div>
                      ))}
                    </div>
                  ) : (
                    <div className="provision-sidebar-copy">No model selected. The node will provision as infrastructure only.</div>
                  )}
                </div>
                <div className="provision-sidebar-block">
                  <LabelText as="div">DEPLOYMENT MODE</LabelText>
                  <div className="provision-sidebar-copy">{spotInstance ? 'Spot capacity enabled' : 'On-demand capacity'}</div>
                </div>
              </aside>
            </div>
          )}
        </div>

        <div className="provision-modal-footer">
          <div className="provision-footer-actions">
            <ActionButton onClick={onClose}>CANCEL</ActionButton>
            {step !== 'compute' && (
              <ActionButton
                onClick={() => setStep(step === 'review' ? 'models' : 'compute')}
              >
                BACK
              </ActionButton>
            )}
          </div>
          <ActionButton
            variant="primary"
            onClick={handlePrimaryAction}
            disabled={
              step === 'compute'
                ? !canContinueFromCompute
                : step === 'review'
                  ? !selectedGPU || provisionMutation.isPending || !dedupedOfferings?.length
                  : !selectedGPU
            }
          >
            {primaryActionLabel}
          </ActionButton>
        </div>
      </div>
    </>
  );
}

export function Instances() {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const [showProvisionModal, setShowProvisionModal] = useState(false);
  const [provisionModalReturnTo, setProvisionModalReturnTo] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState<string>('active');
  const [configuredProviders, setConfiguredProviders] = useState<string[]>([]);
  const [provisionDraft, setProvisionDraft] = useState<ProvisionDraft | null>(null);
  const [verifyingAttemptID, setVerifyingAttemptID] = useState<string | null>(null);
  const autoVerifyTimerRef = useRef<number | null>(null);
  const isMobile = useIsMobile(900);
  const { session } = useAuthSession();
  const workspaceID = session?.workspace?.id;
  const role = session?.key?.role ?? 'user';
  const { data: instances, isLoading } = useInstances();
  const { data: offerings } = useOfferings();
  const { data: providers } = useProviders();
  const { data: workers } = useWorkers();
  const { data: deploymentAttempts = [] } = useDeploymentAttempts(workspaceID);
  const updateDeploymentVerification = useUpdateDeploymentVerification(workspaceID);
  const markAutoVerificationRequested = useMarkDeploymentAutoVerificationRequested(workspaceID);

  const [preselectedModel, setPreselectedModel] = useState<string | null>(null);

  // Scaling controls state
  const scalingDefaults = { minNodes: 1, maxNodes: 5, autoscaleTrigger: 80 };
  const [scalingMinNodes, setScalingMinNodes] = useState(scalingDefaults.minNodes);
  const [scalingMaxNodes, setScalingMaxNodes] = useState(scalingDefaults.maxNodes);
  const [scalingTrigger, setScalingTrigger] = useState(scalingDefaults.autoscaleTrigger);
  const [scalingSaved, setScalingSaved] = useState(scalingDefaults);

  const scalingDirty =
    scalingMinNodes !== scalingSaved.minNodes ||
    scalingMaxNodes !== scalingSaved.maxNodes ||
    scalingTrigger !== scalingSaved.autoscaleTrigger;

  const scalingErrors = {
    minNodes: scalingMinNodes >= scalingMaxNodes ? 'Min must be less than max nodes' : '',
    maxNodes: scalingMaxNodes <= scalingMinNodes ? 'Max must be greater than min nodes' : '',
    trigger: scalingTrigger < 0 || scalingTrigger > 100 ? 'Must be between 0 and 100' : '',
  };
  const scalingHasErrors = Object.values(scalingErrors).some(Boolean);

  const handleApplyScaling = () => {
    if (scalingHasErrors || !scalingDirty) return;
    setScalingSaved({ minNodes: scalingMinNodes, maxNodes: scalingMaxNodes, autoscaleTrigger: scalingTrigger });
    toast.success('Scaling configuration updated');
  };

  const drilldownModel = searchParams.get('model');
  const drilldownFocus = searchParams.get('focus');
  const drilldownModelLabel = drilldownModel?.split('/').pop() || drilldownModel;
  const allInstances = instances || [];

  const healthyWorkers = workers?.filter(w => w.status === 'healthy') || [];
  const totalGpuUtil = healthyWorkers.length > 0
    ? Math.round(healthyWorkers.reduce((sum, w) => sum + w.gpu_utilization, 0) / healthyWorkers.length)
    : 0;
  const totalMemUsed = healthyWorkers.reduce((sum, w) => sum + w.memory_used, 0);
  const totalMemTotal = healthyWorkers.reduce((sum, w) => sum + w.memory_total, 0);
  const visibleProviderStatuses = useMemo(
    () => (providers || []).filter((status) => isInventoryProviderType(status.provider)),
    [providers],
  );
  const filteredInstances = useMemo(() => {
    return allInstances.filter((instance) => {
      if (drilldownModel && !(instance.models || []).includes(drilldownModel)) return false;
      if (statusFilter === 'active' && ['terminated', 'terminating'].includes(instance.status)) return false;
      if (statusFilter !== 'active' && statusFilter !== 'all' && instance.status !== statusFilter) return false;
      if (drilldownFocus === 'degraded') {
        const tone = getInstanceReadiness(instance, workers).tone;
        return tone === 'warning' || tone === 'error';
      }
      return true;
    });
  }, [allInstances, drilldownFocus, drilldownModel, statusFilter, workers]);
  const connectedProviders = visibleProviderStatuses.filter((status) => status.connected);
  const visibleOfferings = useMemo(
    () => (offerings || []).filter((offering) => isInventoryProviderType(offering.provider)),
    [offerings],
  );
  const provisioningState = describeProvisioningState(configuredProviders, visibleProviderStatuses, visibleOfferings.length);
  const providerSummary = filteredInstances.length > 0
    ? [...new Set(filteredInstances.map((instance) => instance.provider))]
    : visibleProviderStatuses
      .filter((status) => status.provider === 'mock' || configuredProviders.includes(status.provider))
      .map((status) => status.provider);
  const providerRail = useMemo(() => {
    const extras = visibleProviderStatuses
      .map((status) => status.provider)
      .filter((provider) => !WORKSPACE_PROVIDER_TYPES.includes(provider as typeof WORKSPACE_PROVIDER_TYPES[number]));
    return [...WORKSPACE_PROVIDER_TYPES, ...extras];
  }, [visibleProviderStatuses]);
  const deploymentHistory = useMemo(
    () => deploymentAttempts.map((attempt) => summarizeDeploymentAttempt(attempt, instances, workers)),
    [deploymentAttempts, instances, workers],
  );
  const latestDeployment = deploymentHistory[0] || null;
  const latestTimeline = latestDeployment ? getDeploymentTimeline(latestDeployment) : [];
  const latestRemediation = latestDeployment ? getDeploymentRemediation(latestDeployment) : null;
  const deploymentSummaryByInstanceID = useMemo(
    () => new Map(
      deploymentHistory
        .filter((summary): summary is DeploymentAttemptSummary & { instance: Instance } => Boolean(summary.instance?.id))
        .map((summary) => [summary.instance.id, summary]),
    ),
    [deploymentHistory],
  );
  const incidentRows = useMemo(
    () => filteredInstances
      .map((instance) => {
        const summary = deploymentSummaryByInstanceID.get(instance.id) || null;
        const incident = deriveNodeIncident(instance, workers, summary);
        return { instance, summary, incident };
      })
      .filter((row) => row.incident),
    [deploymentSummaryByInstanceID, filteredInstances, workers],
  );

  // Auto-open provision modal if redirected from dashboard or registry.
  useEffect(() => {
    if (searchParams.get('provision') === 'true') {
      const model = searchParams.get('model');
      const from = searchParams.get('from');
      if (model) setPreselectedModel(model);
      setProvisionDraft(null);
      setProvisionModalReturnTo(from ? `/${from}` : null);
      setShowProvisionModal(true);
      setSearchParams({}, { replace: true });
    }
  }, [searchParams, setSearchParams]);

  useEffect(() => {
    const workspaceId = session?.workspace?.id;
    if (!workspaceId) {
      setConfiguredProviders([]);
      return;
    }

    if (role !== 'owner' && role !== 'admin') {
      setConfiguredProviders(visibleProviderStatuses.map((status) => status.provider));
      return;
    }

    fetchWorkspaceProviderConfigs(workspaceId)
      .then((configs) => {
        setConfiguredProviders(configs.filter((config) => config.configured).map((config) => config.provider));
      })
      .catch(() => setConfiguredProviders(visibleProviderStatuses.map((status) => status.provider)));
  }, [role, session?.workspace?.id, visibleProviderStatuses]);

  const focusInstance = (instanceID: string) => {
    const target = document.getElementById(`instance-row-${instanceID}`);
    if (!target) return;
    target.scrollIntoView({ behavior: 'smooth', block: 'center' });
  };

  const openFreshProvisionModal = () => {
    setProvisionDraft(null);
    setShowProvisionModal(true);
  };

  const openRetryModal = (attempt: DeploymentAttemptRecord) => {
    setProvisionDraft(attempt.request);
    setPreselectedModel(attempt.request.models?.length === 1 ? attempt.request.models[0] : null);
    setShowProvisionModal(true);
  };

  const runInferenceVerification = useCallback(async (summary: DeploymentAttemptSummary) => {
    const model = summary.instance?.models?.[0] || summary.attempt.request.models?.[0];
    if (!model) {
      toast.error('No deployed model is available to verify');
      return;
    }

    setVerifyingAttemptID(summary.attempt.id);
    const startedAt = Date.now();

    try {
      const response = await sendChatCompletion({
        model,
        messages: [
          { role: 'system', content: 'Reply with a short readiness confirmation.' },
          { role: 'user', content: 'Return a short response confirming that inference is working.' },
        ],
        temperature: 0,
        max_tokens: 16,
      });

      const latencyMs = Date.now() - startedAt;
      const content = response.choices?.[0]?.message?.content?.trim() || '';
      await updateDeploymentVerification.mutateAsync({
        attemptId: summary.attempt.id,
        verification: {
          status: 'passed',
          verified_at: new Date().toISOString(),
          latency_ms: latencyMs,
          model,
          response_preview: content.slice(0, 120),
        },
      });
      toast.success(`Inference verified in ${formatVerificationLatency(latencyMs) || `${latencyMs}ms`}`);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Verification request failed';
      await updateDeploymentVerification.mutateAsync({
        attemptId: summary.attempt.id,
        verification: {
          status: 'failed',
          verified_at: new Date().toISOString(),
          model,
          error: message,
        },
      });
      toast.error(message);
    } finally {
      setVerifyingAttemptID(null);
    }
  }, [workspaceID]);

  const handleRemediation = (summary: DeploymentAttemptSummary, remediation: DeploymentRemediation | null) => {
    if (!remediation) return;

    switch (remediation.action) {
      case 'open_workspace':
        navigate('/workspace');
        return;
      case 'view_capacity':
      case 'retry_config':
        openRetryModal(summary.attempt);
        return;
      case 'focus_instance':
        if (summary.instance?.id) focusInstance(summary.instance.id);
        return;
      case 'verify_inference':
        void runInferenceVerification(summary);
        return;
    }
  };

  const renderIncidentActions = (instance: Instance, summary: DeploymentAttemptSummary | null, compact = false) => {
    if (!summary) return null;

    const buttonStyle = compact ? { fontSize: '0.65rem' } : { fontSize: '0.65rem', marginRight: '1rem' };
    const hasModel = Boolean(instance.models?.length || summary.attempt.request.models?.length);

    return (
      <>
        {instance.status === 'running' && hasModel && (
          <ActionButton
            style={buttonStyle}
            disabled={verifyingAttemptID === summary.attempt.id}
            onClick={() => void runInferenceVerification(summary)}
          >
            {verifyingAttemptID === summary.attempt.id ? 'VERIFYING...' : 'VERIFY NOW'}
          </ActionButton>
        )}
        {summary.retryable && (
          <ActionButton style={buttonStyle} onClick={() => openRetryModal(summary.attempt)}>
            RETRY CONFIG
          </ActionButton>
        )}
      </>
    );
  };

  useEffect(() => {
    if (autoVerifyTimerRef.current) {
      window.clearTimeout(autoVerifyTimerRef.current);
      autoVerifyTimerRef.current = null;
    }

    if (
      !latestDeployment
      || latestDeployment.readiness.label !== 'SERVING VERIFIED'
      || latestDeployment.inferenceVerified
      || latestDeployment.autoVerificationRequested
      || latestDeployment.attempt.inference_verification?.status === 'failed'
      || verifyingAttemptID
    ) {
      return;
    }

    void markAutoVerificationRequested.mutateAsync({
      attemptId: latestDeployment.attempt.id,
      requestedAt: new Date().toISOString(),
    });
    autoVerifyTimerRef.current = window.setTimeout(() => {
      void runInferenceVerification({
        ...latestDeployment,
        autoVerificationRequested: true,
        attempt: {
          ...latestDeployment.attempt,
          auto_verification_requested_at: new Date().toISOString(),
        },
      });
    }, AUTO_VERIFY_DELAY_MS);

    return () => {
      if (autoVerifyTimerRef.current) {
        window.clearTimeout(autoVerifyTimerRef.current);
        autoVerifyTimerRef.current = null;
      }
    };
  }, [latestDeployment, markAutoVerificationRequested, runInferenceVerification, verifyingAttemptID]);

  if (isLoading) return <InstancesSkeleton />;

  return (
    <div className="instances-page animate-fade-in">
      {latestDeployment && (
        <div style={{ padding: '1.25rem 2rem', borderBottom: 'var(--grid-line)', background: 'rgba(255, 255, 255, 0.82)' }}>
          <LabelText as="div" style={{ marginBottom: '0.5rem' }}>LATEST DEPLOYMENT</LabelText>
          <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'flex-start', flexWrap: 'wrap' }}>
            <div>
              <div style={{ fontSize: '1rem', fontWeight: 600 }}>
                {latestDeployment.attempt.selected_model_name
                  || latestDeployment.attempt.instance_name
                  || latestDeployment.attempt.request.name
                  || latestDeployment.attempt.instance_id?.slice(0, 16)
                  || 'Recent deployment'}
              </div>
              <div style={{ marginTop: '0.4rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth: '44rem' }}>
                {latestDeployment.readiness.detail}
              </div>
              <div style={{ marginTop: '0.6rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
                {formatAttemptTime(latestDeployment.attempt.updated_at)}
              </div>
              {latestDeployment.attempt.inference_verification && (
                <div style={{ marginTop: '0.75rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth: '44rem' }}>
                  <LabelText as="div" style={{ marginBottom: '0.35rem' }}>FIRST INFERENCE</LabelText>
                  {latestDeployment.attempt.inference_verification.status === 'passed'
                    ? `Verified on ${formatAttemptTime(latestDeployment.attempt.inference_verification.verified_at)}${latestDeployment.attempt.inference_verification.latency_ms != null ? ` in ${formatVerificationLatency(latestDeployment.attempt.inference_verification.latency_ms)}` : ''}${latestDeployment.attempt.inference_verification.response_preview ? `. Response: ${latestDeployment.attempt.inference_verification.response_preview}` : '.'}`
                    : `Inference check failed on ${formatAttemptTime(latestDeployment.attempt.inference_verification.verified_at)}${latestDeployment.attempt.inference_verification.error ? `: ${latestDeployment.attempt.inference_verification.error}` : '.'}`}
                </div>
              )}
              <DeploymentTimeline steps={latestTimeline} />
              {latestRemediation && (
                <div style={{ marginTop: '0.85rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth: '44rem' }}>
                  {latestRemediation.detail}
                </div>
              )}
            </div>
            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', alignItems: 'center' }}>
              <span className={`badge ${latestDeployment.readiness.tone ? `status-${latestDeployment.readiness.tone}` : ''}`}>{latestDeployment.readiness.label}</span>
              {latestDeployment.instance && getStatusLabel(latestDeployment.instance.status).toUpperCase() !== latestDeployment.readiness.label && (
                <span className="badge">{getStatusLabel(latestDeployment.instance.status).toUpperCase()}</span>
              )}
              {latestRemediation && (
                <ActionButton
                  disabled={latestRemediation.action === 'verify_inference' && verifyingAttemptID === latestDeployment.attempt.id}
                  onClick={() => handleRemediation(latestDeployment, latestRemediation)}
                >
                  {latestRemediation.action === 'verify_inference' && verifyingAttemptID === latestDeployment.attempt.id
                    ? 'VERIFYING...'
                    : latestRemediation.label}
                </ActionButton>
              )}
              {latestDeployment.inferenceVerified && <span className="badge">INFERENCE VERIFIED</span>}
              {latestDeployment.attempt.inference_verification?.status === 'failed' && (
                <span className="badge status-error">INFERENCE FAILED</span>
              )}
              {!latestDeployment.attempt.inference_verification && latestDeployment.autoVerificationRequested && (
                <span className="badge status-warning">
                  {verifyingAttemptID === latestDeployment.attempt.id ? 'AUTO VERIFYING' : 'AUTO VERIFY QUEUED'}
                </span>
              )}
              {latestDeployment.retryable && latestRemediation?.action !== 'retry_config' && (
                <ActionButton onClick={() => openRetryModal(latestDeployment.attempt)}>
                  RETRY CONFIG
                </ActionButton>
              )}
            </div>
          </div>
        </div>
      )}

      <GridRow>
        <Cell span={4}>
          <div className="help-callout">
            <LabelText as="div">NODE STATUS GUIDE</LabelText>
            <div className="help-callout-copy">
              <strong>Connected inventory</strong> means the workspace or local provider path can return live status. <strong>Serving verified</strong> means the worker heartbeat is fresh and runtime looks ready. <strong>Inference verified</strong> means a real chat-completions request passed. Treat the latest deployment banner as the fastest path from provisioned node to confirmed serving.
            </div>
            <div className="help-actions">
              <ActionButton onClick={() => navigate('/workspace')}>OPEN WORKSPACE</ActionButton>
              <ActionButton onClick={() => navigate('/docs')}>READ DEPLOYMENT DOCS</ActionButton>
            </div>
          </div>
        </Cell>
      </GridRow>

      {drilldownModel && (
        <GridRow>
          <Cell span={4}>
            <div className="help-callout">
              <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '1rem', flexWrap: 'wrap' }}>
                <div>
                  <LabelText as="div">MODEL DRILLDOWN</LabelText>
                  <div className="help-callout-copy">
                    Showing {drilldownFocus === 'degraded' ? 'degraded runtime nodes' : 'nodes'} for <strong>{drilldownModelLabel}</strong>. Use this view to inspect the deployments behind the model health signal from the registry.
                  </div>
                </div>
                <span className={`badge ${drilldownFocus === 'degraded' ? 'status-error' : ''}`}>
                  {filteredInstances.length} NODE{filteredInstances.length === 1 ? '' : 'S'}
                </span>
              </div>
              {incidentRows.length > 0 && (
                <div style={{ display: 'grid', gap: '0.75rem', marginTop: '1rem' }}>
                  {incidentRows.slice(0, 3).map(({ instance, summary, incident }) => (
                    <div
                      key={`incident-${instance.id}`}
                      style={{
                        display: 'grid',
                        gap: '0.5rem',
                        padding: '0.85rem 1rem',
                        border: '1px solid var(--border-color)',
                        background: 'rgba(255, 255, 255, 0.88)',
                      }}
                    >
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', flexWrap: 'wrap', alignItems: 'center' }}>
                        <div>
                          <div className="mono" style={{ fontSize: '0.85rem' }}>{instance.name || instance.id.slice(0, 16)}</div>
                          <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginTop: '0.2rem' }}>
                            {instance.gpu_count}x {formatGPUDisplayName(instance.gpu_type)}
                          </div>
                        </div>
                        <span className={`badge ${incident?.tone ? `status-${incident.tone}` : ''}`}>{incident?.title}</span>
                      </div>
                      <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.6 }}>
                        {incident?.detail}
                      </div>
                      <div className="help-actions">
                        <ActionButton onClick={() => focusInstance(instance.id)}>FOCUS NODE</ActionButton>
                        {summary && renderIncidentActions(instance, summary)}
                      </div>
                    </div>
                  ))}
                </div>
              )}
              <div className="help-actions">
                <ActionButton onClick={() => setSearchParams({}, { replace: true })}>CLEAR DRILLDOWN</ActionButton>
                <ActionButton onClick={() => navigate('/models')}>OPEN MODELS</ActionButton>
              </div>
            </div>
          </Cell>
        </GridRow>
      )}

      {/* Metrics Row */}
      <GridRow className="instances-metrics-row">
        <Cell style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
          <LabelText as="div" icon={
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
              <line x1="8" y1="21" x2="16" y2="21" /><line x1="12" y1="17" x2="12" y2="21" />
            </svg>
          }>TOTAL INSTANCES</LabelText>
          <div className="value-text">{filteredInstances.length}</div>
          <div style={{ marginTop: '1rem', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
            {instances?.length || 0} total
          </div>
        </Cell>
        <Cell style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
          <LabelText as="div" icon={
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M12 2v20M2 12h20" />
            </svg>
          }>AVG GPU UTIL</LabelText>
          <div className="value-text">{totalGpuUtil}%</div>
          <ProgressBar value={totalGpuUtil} />
        </Cell>
        <Cell style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
          <LabelText as="div" icon={
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
            </svg>
          }>MEMORY USAGE</LabelText>
          <div className="value-text">
            {totalMemTotal > 0 ? `${(totalMemUsed / 1073741824).toFixed(1)} / ${(totalMemTotal / 1073741824).toFixed(1)} GB` : '-'}
          </div>
          <ProgressBar value={totalMemTotal > 0 ? (totalMemUsed / totalMemTotal * 100) : 0} />
        </Cell>
        <Cell style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
          <LabelText as="div" icon={
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
            </svg>
          }>STATUS</LabelText>
          <div className="value-text" style={{ fontSize: '1.25rem' }}>
            {filteredInstances.filter(i => i.status === 'running').length} Running
          </div>
          <div style={{ marginTop: '1rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <StatusDot tone={filteredInstances.some(i => i.status === 'running') ? 'success' : 'inactive'} />
            <span style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
              {filteredInstances.some(i => i.status === 'running') ? 'Operational' : 'No active nodes'}
            </span>
          </div>
        </Cell>
      </GridRow>

      {/* Main Content Row */}
      <GridRow className="instances-main-row" style={{ flexGrow: 1 }}>
        {/* Node Table */}
        <Cell className="instances-list-cell" span={3}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '2rem' }}>
            <LabelText as="div">NODE OVERVIEW</LabelText>
            <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
              <select
                className="filter-select"
                value={statusFilter}
                onChange={e => setStatusFilter(e.target.value)}
              >
                <option value="active">Active</option>
                <option value="running">Running</option>
                <option value="stopped">Stopped</option>
                <option value="all">All</option>
              </select>
              {drilldownModel && (
                <ActionButton onClick={() => setSearchParams({}, { replace: true })}>
                  CLEAR MODEL FILTER
                </ActionButton>
              )}
            </div>
          </div>

          {filteredInstances.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '4rem 0', color: 'var(--text-secondary)' }}>
              <div style={{ fontSize: '0.9rem', marginBottom: '0.75rem' }}>
                {drilldownModel
                  ? `No ${drilldownFocus === 'degraded' ? 'degraded ' : ''}instances found for ${drilldownModelLabel}`
                  : provisioningState?.title || 'No instances found'}
              </div>
              <div style={{ maxWidth: '34rem', margin: '0 auto 1.25rem', lineHeight: 1.6 }}>
                {drilldownModel
                  ? 'This model does not currently match the selected node view. Clear the drilldown to return to the full cluster inventory.'
                  : provisioningState?.detail || 'Provision your first node to start serving models from this workspace.'}
              </div>
              <div className="help-actions" style={{ justifyContent: 'center' }}>
                <ActionButton
                  onClick={() => {
                    if (drilldownModel) {
                      setSearchParams({}, { replace: true });
                      return;
                    }
                    if (provisioningState) {
                      navigate('/workspace');
                      return;
                    }
                    openFreshProvisionModal();
                  }}
                >
                  {drilldownModel ? 'CLEAR DRILLDOWN' : provisioningState?.action || 'PROVISION NEW NODE'}
                </ActionButton>
                <ActionButton onClick={() => navigate('/models')}>OPEN MODELS</ActionButton>
                <ActionButton onClick={() => navigate('/getting-started')}>OPEN QUICKSTART</ActionButton>
              </div>
            </div>
          ) : isMobile ? (
            <div className="mobile-data-list">
              {filteredInstances.map(instance => {
                const summary = deploymentSummaryByInstanceID.get(instance.id) || null;
                return (
                  <InstanceMobileCard
                    key={instance.id}
                    anchorId={`instance-row-${instance.id}`}
                    instance={instance}
                    statusClass={getStatusClass(instance.status)}
                    statusLabel={getStatusLabel(instance.status)}
                    readiness={getInstanceReadiness(instance, workers)}
                    incident={deriveNodeIncident(instance, workers, summary) || undefined}
                    actions={<InstanceActions instance={instance} compact incidentActions={renderIncidentActions(instance, summary, true)} />}
                  />
                );
              })}
            </div>
          ) : (
            <div className="responsive-scroll-x">
              <table className="data-table responsive-scroll-x-content">
                <thead>
                  <tr>
                    <th scope="col">NODE ID</th>
                    <th scope="col">STATUS</th>
                    <th scope="col">COST</th>
                    <th scope="col">ENDPOINT</th>
                    <th scope="col" style={{ textAlign: 'right' }}>ACTIONS</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredInstances.map(instance => {
                    const summary = deploymentSummaryByInstanceID.get(instance.id) || null;
                    return (
                      <InstanceRow
                        key={instance.id}
                        instance={instance}
                        workers={workers}
                        highlighted={instance.id === latestDeployment?.attempt.instance_id}
                        incident={deriveNodeIncident(instance, workers, summary)}
                        incidentActions={renderIncidentActions(instance, summary)}
                      />
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}

          <ActionButton style={{ marginTop: '2rem' }} onClick={openFreshProvisionModal}>
            PROVISION NEW NODE
          </ActionButton>
        </Cell>

        {/* Sidebar */}
        <Cell className="instances-sidebar-cell" bg="var(--bg-accent)">
          <LabelText as="div" style={{ marginBottom: '2rem' }}>WORKSPACE INFRASTRUCTURE</LabelText>

          <div style={{ marginBottom: '2.5rem' }}>
            <LabelText as="div">PROVIDERS</LabelText>
            <div style={{ marginTop: '0.9rem', display: 'grid', gap: '0.65rem' }}>
              {providerRail.map((providerName) => {
                const status = visibleProviderStatuses.find((provider) => provider.provider === providerName);
                const configured = configuredProviders.includes(providerName);
                const badge = providerStateBadge(status, configured);
                return (
                  <div key={providerName} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem' }}>
                    <span style={{ fontSize: '0.85rem' }}>{getProviderDisplayName(providerName)}</span>
                    <span className={`badge ${badge.tone ? `status-${badge.tone}` : ''}`}>{badge.label}</span>
                  </div>
                );
              })}
            </div>
          </div>

          <div style={{ marginBottom: '2.5rem' }}>
            <LabelText as="div">TOTAL WORKERS</LabelText>
            <div className="mono" style={{ fontSize: '1.25rem', marginTop: '0.5rem' }}>
              {healthyWorkers.length}
            </div>
          </div>

          <div style={{ marginBottom: '2.5rem' }}>
            <LabelText as="div">ACTIVE MODELS</LabelText>
            <div style={{ marginTop: '0.5rem' }}>
              {healthyWorkers.length > 0 ? (
                [...new Set(healthyWorkers.flatMap(w => w.models || []))].map(model => (
                  <div key={model} style={{ fontSize: '0.85rem', padding: '0.25rem 0' }}>
                    {model.split('/').pop()}
                  </div>
                ))
              ) : (
                <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>None</div>
              )}
            </div>
          </div>

          <div style={{ marginTop: '4rem', borderTop: '1px solid var(--border-color)', paddingTop: '2rem' }}>
            <LabelText as="div">PLATFORM HEALTH</LabelText>
            <div style={{ marginTop: '1rem' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '0.85rem', marginBottom: '0.5rem' }}>
                <span>Gateway</span>
                <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', color: 'var(--color-success)' }}><StatusDot tone="success" />OK</span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '0.85rem', marginBottom: '0.5rem' }}>
                <span>Router</span>
                <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', color: 'var(--color-success)' }}><StatusDot tone="success" />OK</span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '0.85rem' }}>
                <span>Workers</span>
                <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', color: healthyWorkers.length > 0 ? 'var(--color-success)' : 'var(--color-warning)' }}>
                  <StatusDot tone={healthyWorkers.length > 0 ? 'success' : 'warning'} />{healthyWorkers.length > 0 ? 'OK' : 'NONE'}
                </span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '0.85rem', marginTop: '0.5rem' }}>
                <span>Providers</span>
                <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', color: connectedProviders.length > 0 ? 'var(--color-success)' : 'var(--color-warning)' }}>
                  <StatusDot tone={connectedProviders.length > 0 ? 'success' : 'warning'} />{connectedProviders.length > 0 ? `${connectedProviders.length} live` : 'CHECK'}
                </span>
              </div>
            </div>
          </div>

          <div style={{ marginTop: '2.5rem', borderTop: '1px solid var(--border-color)', paddingTop: '2rem' }}>
            <LabelText as="div" style={{ marginBottom: '1.25rem' }}>SCALING CONTROLS</LabelText>

            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-secondary)', marginBottom: '0.4rem' }}>
                MIN NODES
              </label>
              <ControlInput
                type="number"
                value={scalingMinNodes}
                onChange={e => setScalingMinNodes(Number(e.target.value))}
                style={{ width: '100%' }}
              />
              {scalingErrors.minNodes && (
                <div style={{ fontSize: '0.75rem', color: 'var(--color-error)', marginTop: '0.3rem', lineHeight: 1.4 }}>
                  {scalingErrors.minNodes}
                </div>
              )}
            </div>

            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-secondary)', marginBottom: '0.4rem' }}>
                MAX NODES
              </label>
              <ControlInput
                type="number"
                value={scalingMaxNodes}
                onChange={e => setScalingMaxNodes(Number(e.target.value))}
                style={{ width: '100%' }}
              />
              {scalingErrors.maxNodes && (
                <div style={{ fontSize: '0.75rem', color: 'var(--color-error)', marginTop: '0.3rem', lineHeight: 1.4 }}>
                  {scalingErrors.maxNodes}
                </div>
              )}
            </div>

            <div style={{ marginBottom: '1.25rem' }}>
              <label style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-secondary)', marginBottom: '0.4rem' }}>
                AUTOSCALE TRIGGER (%)
              </label>
              <ControlInput
                type="number"
                value={scalingTrigger}
                onChange={e => setScalingTrigger(Number(e.target.value))}
                style={{ width: '100%' }}
              />
              {scalingErrors.trigger && (
                <div style={{ fontSize: '0.75rem', color: 'var(--color-error)', marginTop: '0.3rem', lineHeight: 1.4 }}>
                  {scalingErrors.trigger}
                </div>
              )}
            </div>

            <ActionButton
              variant="primary"
              disabled={!scalingDirty || scalingHasErrors}
              onClick={handleApplyScaling}
              style={{ width: '100%' }}
            >
              APPLY CHANGES
            </ActionButton>
          </div>
        </Cell>
      </GridRow>

      {deploymentHistory.length > 0 && (
        <GridRow>
          <Cell span={4}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'center', marginBottom: '1.5rem', flexWrap: 'wrap' }}>
              <div>
                <LabelText as="div" style={{ marginBottom: '0.35rem' }}>DEPLOYMENT HISTORY</LabelText>
                <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
                  Recent provisioning attempts persist per workspace so you can recover the flow after refresh.
                </div>
              </div>
              <ActionButton onClick={openFreshProvisionModal}>NEW ATTEMPT</ActionButton>
            </div>

            <div style={{ display: 'grid', gap: '0.85rem' }}>
              {deploymentHistory.slice(0, 5).map((summary) => {
                const { attempt, readiness, instance, retryable } = summary;
                const timeline = getDeploymentTimeline(summary);
                const remediation = getDeploymentRemediation(summary);

                return (
                  <div
                    key={attempt.id}
                    style={{
                      border: 'var(--grid-line)',
                      padding: '1rem 1.1rem',
                      background: latestDeployment?.attempt.id === attempt.id ? 'rgba(244, 242, 238, 0.7)' : 'transparent',
                    }}
                  >
                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'flex-start', flexWrap: 'wrap' }}>
                      <div style={{ minWidth: 0, flex: '1 1 28rem' }}>
                        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
                          <div style={{ fontSize: '0.95rem', fontWeight: 600 }}>
                            {attempt.selected_model_name
                              || attempt.instance_name
                              || attempt.request.name
                              || attempt.request.models?.[0]?.split('/').pop()
                              || 'Provisioning attempt'}
                          </div>
                          <span className={`badge ${readiness.tone ? `status-${readiness.tone}` : ''}`}>{readiness.label}</span>
                          {instance && getStatusLabel(instance.status).toUpperCase() !== readiness.label && <span className="badge">{getStatusLabel(instance.status).toUpperCase()}</span>}
                        </div>
                        <div style={{ marginTop: '0.45rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth: '54rem' }}>
                          {readiness.detail}
                        </div>
                        <div style={{ marginTop: '0.65rem', display: 'flex', gap: '0.5rem', flexWrap: 'wrap', color: 'var(--text-secondary)', fontSize: '0.75rem' }}>
                          <span className="badge">{formatAttemptTime(attempt.updated_at)}</span>
                          {attempt.request.provider && <span className="badge">{getProviderDisplayName(attempt.request.provider)}</span>}
                          <span className="badge">{attempt.request.gpu_count || 1}x {formatGPUDisplayName(attempt.request.gpu_type)}</span>
                          {attempt.request.spot_instance ? <span className="badge">SPOT</span> : null}
                          {attempt.request.models?.length ? <span className="badge">{attempt.request.models.length} MODEL{attempt.request.models.length === 1 ? '' : 'S'}</span> : null}
                          {summary.inferenceVerified ? <span className="badge">INFERENCE VERIFIED</span> : null}
                          {attempt.inference_verification?.status === 'failed' ? <span className="badge status-error">INFERENCE FAILED</span> : null}
                          {!attempt.inference_verification && summary.autoVerificationRequested ? (
                            <span className="badge status-warning">{verifyingAttemptID === attempt.id ? 'AUTO VERIFYING' : 'AUTO VERIFY QUEUED'}</span>
                          ) : null}
                        </div>
                        {attempt.inference_verification && (
                          <div style={{ marginTop: '0.75rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth: '54rem' }}>
                            <LabelText as="div" style={{ marginBottom: '0.35rem' }}>FIRST INFERENCE</LabelText>
                            {attempt.inference_verification.status === 'passed'
                              ? `Verified on ${formatAttemptTime(attempt.inference_verification.verified_at)}${attempt.inference_verification.latency_ms != null ? ` in ${formatVerificationLatency(attempt.inference_verification.latency_ms)}` : ''}${attempt.inference_verification.response_preview ? `. Response: ${attempt.inference_verification.response_preview}` : '.'}`
                              : `Inference check failed on ${formatAttemptTime(attempt.inference_verification.verified_at)}${attempt.inference_verification.error ? `: ${attempt.inference_verification.error}` : '.'}`}
                          </div>
                        )}
                        <DeploymentTimeline steps={timeline} />
                        {remediation && (
                          <div style={{ marginTop: '0.75rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth: '54rem' }}>
                            {remediation.detail}
                          </div>
                        )}
                      </div>

                      <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
                        {instance && <InstanceActions instance={instance} compact />}
                        {remediation && (
                          <ActionButton
                            disabled={remediation.action === 'verify_inference' && verifyingAttemptID === attempt.id}
                            onClick={() => handleRemediation(summary, remediation)}
                          >
                            {remediation.action === 'verify_inference' && verifyingAttemptID === attempt.id
                              ? 'VERIFYING...'
                              : remediation.label}
                          </ActionButton>
                        )}
                        {retryable && remediation?.action !== 'retry_config' && (
                          <ActionButton onClick={() => openRetryModal(attempt)}>
                            RETRY CONFIG
                          </ActionButton>
                        )}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </Cell>
        </GridRow>
      )}

      {/* Footer */}
      <GridRow className="instances-footer-row">
        <Cell>
          <LabelText as="div">PROVIDER</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {providerSummary.length > 0 ? providerSummary.map((provider) => getProviderDisplayName(provider)).join(', ') : '—'}
          </div>
        </Cell>
        <Cell>
          <LabelText as="div">TOTAL COST</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            ${filteredInstances.reduce((sum, i) => sum + i.cost_per_hour, 0).toFixed(2)}/hr
          </div>
        </Cell>
        <Cell span={2}>
          <LabelText as="div">TAGS</LabelText>
          <div style={{ display: 'flex', gap: '0.75rem', marginTop: '0.5rem' }}>
            <Badge>INFERENCE</Badge>
            <Badge>GPU</Badge>
            <Badge>PRODUCTION</Badge>
          </div>
        </Cell>
      </GridRow>

      <ProvisionModal
        isOpen={showProvisionModal}
        onClose={() => {
          setShowProvisionModal(false);
          setPreselectedModel(null);
          setProvisionDraft(null);
          if (provisionModalReturnTo) {
            navigate(provisionModalReturnTo);
            setProvisionModalReturnTo(null);
          }
        }}
        onProvisioned={() => {
          setStatusFilter('active');
          setProvisionModalReturnTo(null);
        }}
        onProvisionFailed={(request) => {
          setProvisionDraft(request);
        }}
        onOpenWorkspace={() => {
          setShowProvisionModal(false);
          navigate('/workspace');
        }}
        offerings={visibleOfferings}
        preselectedModel={preselectedModel}
        initialDraft={provisionDraft}
        providerStatuses={visibleProviderStatuses}
        configuredProviders={configuredProviders}
      />
    </div>
  );
}
