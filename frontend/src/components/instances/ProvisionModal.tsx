import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';

import { ActionButton, Badge, ControlInput, LabelText } from '../shared';
import { useProvisionInstance } from '../../hooks/useInfrastructureApi';
import { useVaultModels } from '../../hooks/useVaultApi';
import { describeProvisioningState, type ProvisionDraft } from '../../lib/instanceProvisioning';
import { formatGPUDisplayName, formatOfferingRegion } from '../../lib/labels';
import { getProviderDisplayName } from '../../lib/providerInventory';
import type {
  GPUOffering,
  GPUType,
  Instance,
  KnownGPUType,
  ProviderStatus,
  ProviderType,
  ProvisionRequest,
  VaultModel,
} from '../../types';

const GPU_VRAM_GB: Record<KnownGPUType, number> = {
  RTX_4090: 24,
  RTX_4080: 16,
  A100_40GB: 40,
  A100_80GB: 80,
  H100: 80,
  L40S: 48,
};

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
    .sort((left, right) => left.cost_per_hour - right.cost_per_hour);
  return sameGPU[0] || null;
}

function presetCapacityWarning(preset?: ModelDeploymentPreset): string | null {
  if (!preset) return null;
  return `This preset currently needs ${preset.preferredGPUCount}x ${preset.preferredGPUType.replace('_', ' ')} or larger live capacity.`;
}

export function ProvisionModal({
  isOpen,
  onClose,
  onProvisioned,
  onProvisionFailed,
  onOpenWorkspace,
  offerings,
  preselectedModel,
  initialDraft,
  providerStatuses,
  configuredProviders,
}: {
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
  const [selectedGPU, setSelectedGPU] = useState('');
  const [name, setName] = useState('');
  const [spotInstance, setSpotInstance] = useState(false);
  const [selectedModels, setSelectedModels] = useState<string[]>([]);
  const [modelSearch, setModelSearch] = useState('');
  const provisionMutation = useProvisionInstance();
  const { data: vaultModels } = useVaultModels({ status: 'available' });
  const initializedDraftRef = useRef<string | null>(null);
  const deferredModelSearch = useDeferredValue(modelSearch);

  const getOfferingKey = (offering: GPUOffering) =>
    `${offering.provider}-${offering.provider_gpu_type_id || offering.gpu_type}-${offering.gpu_count}-${offering.memory_gb}-${offering.vcpu}-${offering.region || 'global'}`;

  const getOfferingGroupKey = (offering: GPUOffering) =>
    `${offering.provider}-${offering.provider_gpu_type_id || offering.gpu_type}-${offering.display_name || offering.gpu_type}`;

  const dedupedOfferings = useMemo(
    () => offerings ? Array.from(
      offerings.reduce((map, offering) => {
        const key = getOfferingKey(offering);
        const existing = map.get(key);
        if (!existing || offering.cost_per_hour < existing.cost_per_hour) map.set(key, offering);
        return map;
      }, new Map<string, GPUOffering>()).values(),
    ) : undefined,
    [offerings],
  );
  const provisioningState = describeProvisioningState(
    configuredProviders,
    providerStatuses,
    dedupedOfferings?.length ?? 0,
  );

  const selectedOffering = dedupedOfferings?.find((offering) => getOfferingKey(offering) === selectedGPU);
  const selectedGPUVram = selectedOffering
    ? (selectedOffering.memory_gb || GPU_VRAM_GB[selectedOffering.gpu_type as KnownGPUType])
    : undefined;
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
  const compatibleModels = useMemo(
    () => allVaultModels?.filter((model: VaultModel) => {
      if (!selectedGPUVram) return true;
      return model.vram_required <= selectedGPUVram * 1024;
    }),
    [allVaultModels, selectedGPUVram],
  );
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
  }, [compatibleModels, isOpen, preselectedModel]);

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
    setSelectedModels((prev) => prev.includes(sourceUri) ? prev.filter((id) => id !== sourceUri) : [...prev, sourceUri]);
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
              <ActionButton onClick={() => setStep(step === 'review' ? 'models' : 'compute')}>
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
