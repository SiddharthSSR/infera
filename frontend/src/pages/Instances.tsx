import { useState, useEffect, useMemo } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import type { Instance, GPUOffering, GPUType, ProviderStatus, VaultModel } from '../types';
import { fetchWorkspaceProviderConfigs } from '../lib/api';
import { useInstances, useOfferings, useProviders, useTerminateInstance, useStartInstance, useStopInstance, useProvisionInstance, useVaultModels, useWorkers } from '../hooks/useApi';
import { useIsMobile } from '../hooks/useIsMobile';
import { InstanceMobileCard } from '../components/InstanceMobileCard';
import { useAuthSession } from '../lib/auth-context';

const GPU_VRAM_GB: Record<GPUType, number> = {
  RTX_4090: 24,
  RTX_4080: 16,
  A100_40GB: 40,
  A100_80GB: 80,
  H100: 80,
  L40S: 48,
};

const CONFIGURABLE_PROVIDERS = ['runpod', 'vastai'] as const;

function describeProvisioningState(configuredProviders: string[], providerStatuses: ProviderStatus[], offeringsCount: number) {
  const visibleStatuses = providerStatuses.filter((status) => CONFIGURABLE_PROVIDERS.includes(status.provider as typeof CONFIGURABLE_PROVIDERS[number]));
  const connectedProviders = visibleStatuses.filter((status) => status.connected);

  if (configuredProviders.length === 0) {
    return {
      title: 'No workspace provider is configured',
      detail: 'Add RunPod or Vast.ai credentials in Workspace settings before provisioning nodes from this workspace.',
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

function providerStateBadge(status?: ProviderStatus, configured?: boolean) {
  if (!configured) return { label: 'NOT CONFIGURED', tone: 'inactive' };
  if (!status) return { label: 'UNAVAILABLE', tone: 'warning' };
  if (status.connected) return { label: 'CONNECTED', tone: '' };
  if (status.error_code === 'auth_failed') return { label: 'AUTH FAILED', tone: 'error' };
  return { label: 'DEGRADED', tone: 'warning' };
}

function getStatusClass(status: string) {
  switch (status) {
    case 'running':
      return '';
    case 'error':
      return 'error';
    case 'stopping':
    case 'pending':
    case 'provisioning':
      return 'warning';
    case 'stopped':
    case 'terminating':
    case 'terminated':
      return 'inactive';
    default:
      console.warn('Unknown instance status class fallback', status);
      return '';
  }
}

function getStatusLabel(status: string) {
  switch (status) {
    case 'pending':
      return 'Pending';
    case 'provisioning':
      return 'Provisioning';
    case 'running':
      return 'Running';
    case 'stopping':
      return 'Stopping';
    case 'stopped':
      return 'Stopped';
    case 'terminating':
      return 'Terminating';
    case 'terminated':
      return 'Terminated';
    case 'error':
      return 'Error';
    default:
      console.warn('Unknown instance status label fallback', status);
      return 'Unknown';
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
      toast.error('Failed to start');
    }
  };

  const handleStop = async () => {
    try {
      await stopMutation.mutateAsync(instance.id);
      toast.success('Instance stopped');
    } catch (err) {
      console.error('Failed to stop instance', err);
      toast.error('Failed to stop');
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

function InstanceActions({ instance, compact = false }: { instance: Instance; compact?: boolean }) {
  const { isLoading, handleStart, handleStop, handleTerminate } = useInstanceActions(instance);
  const buttonStyle = compact ? { fontSize: '0.65rem' } : { fontSize: '0.65rem', marginRight: '1rem' };

  return (
    <>
      {instance.status === 'stopped' && (
        <button className="action-btn" style={buttonStyle} disabled={isLoading} onClick={handleStart}>START</button>
      )}
      {instance.status === 'running' && (
        <button className="action-btn" style={buttonStyle} disabled={isLoading} onClick={handleStop}>STOP</button>
      )}
      {instance.status !== 'terminating' && instance.status !== 'terminated' && (
        <button
          className="action-btn destructive"
          style={{ fontSize: '0.65rem' }}
          disabled={isLoading}
          onClick={handleTerminate}
        >
          TERMINATE
        </button>
      )}
    </>
  );
}

function InstanceRow({ instance }: { instance: Instance }) {
  const statusClass = getStatusClass(instance.status);
  const statusLabel = getStatusLabel(instance.status);

  return (
    <tr style={{ borderBottom: '1px solid #EEEEEC' }}>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle' }}>
        <div className="mono">{instance.name || instance.id.slice(0, 16)}</div>
        <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginTop: 2 }}>
          {instance.gpu_count}x {instance.gpu_type.replace('_', ' ')}
          {instance.models && instance.models.length > 0 && (
            <> &middot; {instance.models[0].split('/').pop()}</>
          )}
        </div>
      </td>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: '0.85rem' }}>
          <span className={`status-dot ${statusClass}`} />
          {statusLabel}
        </div>
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
        <InstanceActions instance={instance} />
      </td>
    </tr>
  );
}

function ProvisionModal({ isOpen, onClose, offerings, preselectedModel, providerStatuses, configuredProviders }: {
  isOpen: boolean;
  onClose: () => void;
  offerings: GPUOffering[] | undefined;
  preselectedModel?: string | null;
  providerStatuses: ProviderStatus[];
  configuredProviders: string[];
}) {
  const [selectedGPU, setSelectedGPU] = useState<string>('');
  const [name, setName] = useState('');
  const [spotInstance, setSpotInstance] = useState(false);
  const [selectedModels, setSelectedModels] = useState<string[]>([]);
  const provisionMutation = useProvisionInstance();
  const { data: vaultModels } = useVaultModels({ status: 'available' });

  const getOfferingKey = (o: GPUOffering) =>
    `${o.provider}-${o.gpu_type}-${o.gpu_count}-${o.memory_gb}-${o.vcpu}`;

  // Deduplicate offerings
  const dedupedOfferings = offerings ? Array.from(
    offerings.reduce((map, o) => {
      const key = getOfferingKey(o);
      const existing = map.get(key);
      if (!existing || o.cost_per_hour < existing.cost_per_hour) map.set(key, o);
      return map;
    }, new Map<string, GPUOffering>()).values()
  ) : undefined;
  const provisioningState = describeProvisioningState(configuredProviders, providerStatuses, dedupedOfferings?.length ?? 0);

  const selectedOffering = dedupedOfferings?.find(o => getOfferingKey(o) === selectedGPU);
  const selectedGPUVram = selectedOffering ? GPU_VRAM_GB[selectedOffering.gpu_type] : undefined;

  const allVaultModels = vaultModels?.models;
  const selectedModelRecord = useMemo(
    () => allVaultModels?.find((model) => model.source_uri === preselectedModel),
    [allVaultModels, preselectedModel],
  );
  const compatibleModels = useMemo(() => {
    return allVaultModels?.filter((m: VaultModel) => {
      if (!selectedGPUVram) return true;
      return m.vram_required <= selectedGPUVram * 1024;
    });
  }, [allVaultModels, selectedGPUVram]);
  const pinnedModelCompatibleOfferings = useMemo(() => {
    if (!dedupedOfferings) return [];
    if (!selectedModelRecord?.vram_required) return dedupedOfferings;

    return dedupedOfferings.filter((offering) => {
      const vramGB = GPU_VRAM_GB[offering.gpu_type] || 0;
      return vramGB * 1024 >= selectedModelRecord.vram_required;
    });
  }, [dedupedOfferings, selectedModelRecord]);
  const recommendedOffering = useMemo(
    () => pinnedModelCompatibleOfferings.reduce<GPUOffering | null>((best, offering) => {
      if (!best || offering.cost_per_hour < best.cost_per_hour) return offering;
      return best;
    }, null),
    [pinnedModelCompatibleOfferings],
  );

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
    if (!isOpen || !preselectedModel || selectedGPU || !recommendedOffering) return;
    setSelectedGPU(getOfferingKey(recommendedOffering));
  }, [isOpen, preselectedModel, recommendedOffering, selectedGPU]);

  const toggleModel = (sourceUri: string) => {
    setSelectedModels(prev => prev.includes(sourceUri) ? prev.filter(id => id !== sourceUri) : [...prev, sourceUri]);
  };

  const handleProvision = async () => {
    if (!selectedOffering) return;
    try {
      await provisionMutation.mutateAsync({
        name: name || 'infera-worker',
        provider: selectedOffering.provider,
        gpu_type: selectedOffering.gpu_type,
        gpu_count: selectedOffering.gpu_count,
        spot_instance: spotInstance,
        models: selectedModels.length > 0 ? selectedModels : undefined,
      });
      toast.success(
        selectedModels.length > 0
          ? `Provisioning ${selectedModels.length === 1 ? (selectedModelRecord?.name || selectedModels[0].split('/').pop()) : `${selectedModels.length} models`} on ${selectedOffering.gpu_type.replace('_', ' ')}`
          : `Provisioning node on ${selectedOffering.gpu_type.replace('_', ' ')}`,
      );
      onClose();
      setName('');
      setSelectedGPU('');
      setSelectedModels([]);
      setSpotInstance(false);
    } catch { toast.error('Failed to provision'); }
  };

  if (!isOpen) return null;

  return (
    <>
      <div
        style={{ position: 'fixed', inset: 0, background: 'rgba(253,251,248,0.8)', backdropFilter: 'blur(4px)', zIndex: 50 }}
        onClick={onClose}
      />
      <div style={{
        position: 'fixed', top: '1.5rem', bottom: '1.5rem', left: '50%', transform: 'translateX(-50%)',
        width: 'min(1180px, calc(100vw - 3rem))', maxWidth: 'none',
        background: 'var(--bg-paper)', border: 'var(--grid-line)', zIndex: 50,
        display: 'flex', flexDirection: 'column', overflow: 'hidden'
      }}>
        {/* Header */}
        <div style={{ padding: '2rem', borderBottom: 'var(--grid-line)' }}>
          <div className="label-text" style={{ marginBottom: '0.5rem' }}>PROVISION NEW NODE</div>
          <div style={{ fontSize: '0.9rem', color: 'var(--text-secondary)' }}>Select GPU configuration and models to deploy</div>
        </div>

        {/* Content */}
        <div className="provision-modal-body" style={{ flex: 1, overflowY: 'auto', padding: '2rem' }}>
          {selectedModelRecord && (
            <div className="workspace-provider-card" style={{ marginBottom: '2rem' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', flexWrap: 'wrap' }}>
                <div>
                  <div className="label-text" style={{ marginBottom: '0.45rem' }}>SELECTED MODEL</div>
                  <div style={{ fontSize: '1.1rem', fontWeight: 600 }}>{selectedModelRecord.name}</div>
                  <div className="mono" style={{ marginTop: '0.3rem', color: 'var(--text-secondary)' }}>
                    {selectedModelRecord.source_uri}
                  </div>
                </div>
                <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', alignItems: 'flex-start' }}>
                  {selectedModelRecord.parameters && <span className="badge">{selectedModelRecord.parameters}</span>}
                  {selectedModelRecord.quantization && <span className="badge">{selectedModelRecord.quantization}</span>}
                  {selectedModelRecord.vram_required ? <span className="badge mono">{Math.ceil(selectedModelRecord.vram_required / 1024)}GB VRAM</span> : null}
                </div>
              </div>
              <div style={{ marginTop: '0.85rem', fontSize: '0.88rem', color: 'var(--text-secondary)', lineHeight: 1.6 }}>
                {pinnedModelCompatibleOfferings.length > 0
                  ? `This model fits ${pinnedModelCompatibleOfferings.length} live GPU option${pinnedModelCompatibleOfferings.length === 1 ? '' : 's'}${recommendedOffering ? `, starting from $${recommendedOffering.cost_per_hour.toFixed(2)}/hr on ${recommendedOffering.provider}.` : '.'}`
                  : 'No live GPU option currently satisfies the recorded VRAM requirement for this model.'}
              </div>
            </div>
          )}

          <div style={{ marginBottom: '2rem' }}>
            <div className="label-text">INSTANCE NAME</div>
            <input type="text" className="control-input" value={name} onChange={e => setName(e.target.value)} placeholder="infera-worker" style={{ marginTop: '0.5rem' }} />
          </div>

          <div className="provision-modal-grid">
            <div>
              <div className="label-text" style={{ marginBottom: '1rem' }}>GPU CONFIGURATION</div>
              {dedupedOfferings && dedupedOfferings.length > 0 ? (
                <div className="provision-options-grid" style={{ marginBottom: '2rem' }}>
                  {dedupedOfferings.map(o => {
                    const key = getOfferingKey(o);
                    const isSelected = selectedGPU === key;
                    return (
                      <button
                        key={key}
                        onClick={() => setSelectedGPU(prev => prev === key ? '' : key)}
                        style={{
                          padding: '1.25rem', textAlign: 'left', cursor: 'pointer',
                          border: isSelected ? '2px solid var(--text-primary)' : 'var(--grid-line)',
                          background: isSelected ? 'var(--bg-accent)' : 'transparent',
                          fontFamily: 'var(--font-main)',
                        }}
                      >
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.75rem', alignItems: 'flex-start' }}>
                          <div style={{ fontWeight: 600, fontSize: '1rem', marginBottom: '0.5rem' }}>
                            {o.gpu_count}x {o.gpu_type.replace('_', ' ')}
                          </div>
                          <span className="badge">{o.provider.toUpperCase()}</span>
                        </div>
                        <div style={{ display: 'flex', gap: '0.75rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
                          <span>{o.vcpu} vCPU</span>
                          <span>{o.memory_gb}GB</span>
                          <span>{o.region || 'default'}</span>
                        </div>
                        <div className="mono" style={{ marginTop: '0.75rem', fontSize: '1rem' }}>
                          ${o.cost_per_hour.toFixed(2)}<span style={{ fontSize: '0.75rem', color: 'var(--text-secondary)' }}>/hr</span>
                        </div>
                      </button>
                    );
                  })}
                </div>
              ) : (
                <div className="workspace-provider-card" style={{ marginBottom: '2rem' }}>
                  <div className="label-text" style={{ marginBottom: '0.6rem' }}>{provisioningState?.title || 'NO OFFERINGS AVAILABLE'}</div>
                  <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem', lineHeight: 1.6 }}>
                    {provisioningState?.detail || 'No GPU inventory is currently available for this workspace.'}
                  </div>
                </div>
              )}
            </div>

            <div>
              {/* Models */}
              {allVaultModels && allVaultModels.length > 0 && (
                <div style={{ marginBottom: '2rem' }}>
                  <div className="label-text" style={{ marginBottom: '0.5rem' }}>MODELS TO DEPLOY</div>
                  <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginBottom: '1rem' }}>
                    {selectedGPUVram ? `Showing models that fit within ${selectedGPUVram}GB VRAM` : 'Select a GPU to filter compatible models'}
                  </div>
                  <div className="provision-model-grid">
                    {(compatibleModels || []).map(model => {
                      const isSelected = selectedModels.includes(model.source_uri);
                      return (
                        <button
                          key={model.id}
                          onClick={() => toggleModel(model.source_uri)}
                          style={{
                            padding: '0.5rem 1rem', cursor: 'pointer',
                            border: isSelected ? '1px solid var(--text-primary)' : 'var(--grid-line)',
                            background: isSelected ? 'var(--bg-accent)' : 'transparent',
                            fontFamily: 'var(--font-main)', fontSize: '0.85rem',
                            display: 'flex', alignItems: 'center', gap: '0.5rem',
                          }}
                        >
                          {model.name}
                          {model.parameters && <span className="badge">{model.parameters}</span>}
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}

              {/* Spot toggle */}
              <label style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                <input type="checkbox" checked={spotInstance} onChange={e => setSpotInstance(e.target.checked)} />
                <span style={{ fontSize: '0.9rem' }}>Spot Instance (up to 70% cheaper, may be interrupted)</span>
              </label>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="provision-modal-footer" style={{ padding: '1.5rem 2rem', borderTop: 'var(--grid-line)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <button className="action-btn" onClick={onClose}>CANCEL</button>
          <button className="btn-primary" onClick={handleProvision} disabled={!selectedGPU || provisionMutation.isPending || !dedupedOfferings?.length}>
            {provisionMutation.isPending ? 'PROVISIONING...' : 'PROVISION NODE'}
          </button>
        </div>
      </div>
    </>
  );
}

export function Instances() {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const [showProvisionModal, setShowProvisionModal] = useState(false);
  const [statusFilter, setStatusFilter] = useState<string>('active');
  const [configuredProviders, setConfiguredProviders] = useState<string[]>([]);
  const isMobile = useIsMobile(900);
  const { session } = useAuthSession();
  const role = session?.key?.role ?? 'user';
  const { data: instances, isLoading } = useInstances();
  const { data: offerings } = useOfferings();
  const { data: providers } = useProviders();
  const { data: workers } = useWorkers();

  const [preselectedModel, setPreselectedModel] = useState<string | null>(null);

  const filteredInstances = instances?.filter(instance => {
    if (statusFilter === 'active') return !['terminated', 'terminating'].includes(instance.status);
    if (statusFilter === 'all') return true;
    return instance.status === statusFilter;
  }) || [];

  const healthyWorkers = workers?.filter(w => w.status === 'healthy') || [];
  const totalGpuUtil = healthyWorkers.length > 0
    ? Math.round(healthyWorkers.reduce((sum, w) => sum + w.gpu_utilization, 0) / healthyWorkers.length)
    : 0;
  const totalMemUsed = healthyWorkers.reduce((sum, w) => sum + w.memory_used, 0);
  const totalMemTotal = healthyWorkers.reduce((sum, w) => sum + w.memory_total, 0);
  const visibleProviderStatuses = useMemo(
    () => (providers || []).filter((status) => CONFIGURABLE_PROVIDERS.includes(status.provider as typeof CONFIGURABLE_PROVIDERS[number])),
    [providers],
  );
  const connectedProviders = visibleProviderStatuses.filter((status) => status.connected);
  const provisioningState = describeProvisioningState(configuredProviders, visibleProviderStatuses, offerings?.length ?? 0);
  const providerSummary = filteredInstances.length > 0
    ? [...new Set(filteredInstances.map((instance) => instance.provider))]
    : visibleProviderStatuses.filter((status) => configuredProviders.includes(status.provider)).map((status) => status.provider);

  // Auto-open provision modal if redirected from dashboard or registry.
  useEffect(() => {
    if (searchParams.get('provision') === 'true') {
      const model = searchParams.get('model');
      if (model) setPreselectedModel(model);
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

  return (
    <div className="instances-page animate-fade-in">
      {/* Metrics Row */}
      <div className="grid-row instances-metrics-row">
        <div className="cell" style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
          <div className="label-text">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
              <line x1="8" y1="21" x2="16" y2="21" /><line x1="12" y1="17" x2="12" y2="21" />
            </svg>
            TOTAL INSTANCES
          </div>
          <div className="value-text">{filteredInstances.length}</div>
          <div style={{ marginTop: '1rem', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
            {instances?.length || 0} total
          </div>
        </div>
        <div className="cell" style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
          <div className="label-text">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M12 2v20M2 12h20" />
            </svg>
            AVG GPU UTIL
          </div>
          <div className="value-text">{totalGpuUtil}%</div>
          <div className="progress-track">
            <div className="progress-fill" style={{ width: `${totalGpuUtil}%` }} />
          </div>
        </div>
        <div className="cell" style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
          <div className="label-text">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
            </svg>
            MEMORY USAGE
          </div>
          <div className="value-text">
            {totalMemTotal > 0 ? `${(totalMemUsed / 1073741824).toFixed(1)} / ${(totalMemTotal / 1073741824).toFixed(1)} GB` : '-'}
          </div>
          <div className="progress-track">
            <div className="progress-fill" style={{ width: totalMemTotal > 0 ? `${(totalMemUsed / totalMemTotal * 100)}%` : '0%' }} />
          </div>
        </div>
        <div className="cell" style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
          <div className="label-text">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
            </svg>
            STATUS
          </div>
          <div className="value-text" style={{ fontSize: '1.25rem' }}>
            {filteredInstances.filter(i => i.status === 'running').length} Running
          </div>
          <div style={{ marginTop: '1rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span className={`status-dot ${filteredInstances.some(i => i.status === 'running') ? '' : 'inactive'}`} />
            <span style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
              {filteredInstances.some(i => i.status === 'running') ? 'Operational' : 'No active nodes'}
            </span>
          </div>
        </div>
      </div>

      {/* Main Content Row */}
      <div className="grid-row instances-main-row" style={{ flexGrow: 1 }}>
        {/* Node Table */}
        <div className="cell instances-list-cell" style={{ gridColumn: 'span 3' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '2rem' }}>
            <div className="label-text">NODE OVERVIEW</div>
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
            </div>
          </div>

          {isLoading ? (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>Loading...</div>
          ) : filteredInstances.length === 0 ? (
            <div style={{ textAlign: 'center', padding: '4rem 0', color: 'var(--text-secondary)' }}>
              <div style={{ fontSize: '0.9rem', marginBottom: '0.75rem' }}>{provisioningState?.title || 'No instances found'}</div>
              <div style={{ maxWidth: '34rem', margin: '0 auto 1.25rem', lineHeight: 1.6 }}>
                {provisioningState?.detail || 'Provision your first node to start serving models from this workspace.'}
              </div>
              <button
                className="action-btn"
                onClick={() => {
                  if (provisioningState) {
                    navigate('/workspace');
                    return;
                  }
                  setShowProvisionModal(true);
                }}
              >
                {provisioningState?.action || 'PROVISION NEW NODE'}
              </button>
            </div>
          ) : isMobile ? (
            <div className="mobile-data-list">
              {filteredInstances.map(instance => (
                <InstanceMobileCard
                  key={instance.id}
                  instance={instance}
                  statusClass={getStatusClass(instance.status)}
                  statusLabel={getStatusLabel(instance.status)}
                  actions={<InstanceActions instance={instance} compact />}
                />
              ))}
            </div>
          ) : (
            <div className="responsive-scroll-x">
              <table className="data-table responsive-scroll-x-content">
                <thead>
                  <tr>
                    <th>NODE ID</th>
                    <th>STATUS</th>
                    <th>COST</th>
                    <th>ENDPOINT</th>
                    <th style={{ textAlign: 'right' }}>ACTIONS</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredInstances.map(instance => (
                    <InstanceRow key={instance.id} instance={instance} />
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <button className="action-btn" style={{ marginTop: '2rem' }} onClick={() => setShowProvisionModal(true)}>
            PROVISION NEW NODE
          </button>
        </div>

        {/* Sidebar */}
        <div className="cell instances-sidebar-cell" style={{ backgroundColor: 'var(--bg-accent)' }}>
          <div className="label-text" style={{ marginBottom: '2rem' }}>CLUSTER INFO</div>

          <div style={{ marginBottom: '2.5rem' }}>
            <div className="label-text">PROVIDERS</div>
            <div style={{ marginTop: '0.9rem', display: 'grid', gap: '0.65rem' }}>
              {CONFIGURABLE_PROVIDERS.map((providerName) => {
                const status = visibleProviderStatuses.find((provider) => provider.provider === providerName);
                const configured = configuredProviders.includes(providerName);
                const badge = providerStateBadge(status, configured);
                return (
                  <div key={providerName} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem' }}>
                    <span style={{ fontSize: '0.85rem' }}>{providerName}</span>
                    <span className={`badge ${badge.tone ? `status-${badge.tone}` : ''}`}>{badge.label}</span>
                  </div>
                );
              })}
            </div>
          </div>

          <div style={{ marginBottom: '2.5rem' }}>
            <div className="label-text">TOTAL WORKERS</div>
            <div className="mono" style={{ fontSize: '1.25rem', marginTop: '0.5rem' }}>
              {healthyWorkers.length}
            </div>
          </div>

          <div style={{ marginBottom: '2.5rem' }}>
            <div className="label-text">ACTIVE MODELS</div>
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
            <div className="label-text">CLUSTER HEALTH</div>
            <div style={{ marginTop: '1rem' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem', marginBottom: '0.5rem' }}>
                <span>Gateway</span>
                <span style={{ color: 'var(--color-success)' }}>OK</span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem', marginBottom: '0.5rem' }}>
                <span>Router</span>
                <span style={{ color: 'var(--color-success)' }}>OK</span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem' }}>
                <span>Workers</span>
                <span style={{ color: healthyWorkers.length > 0 ? 'var(--color-success)' : 'var(--color-warning)' }}>
                  {healthyWorkers.length > 0 ? 'OK' : 'NONE'}
                </span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem', marginTop: '0.5rem' }}>
                <span>Providers</span>
                <span style={{ color: connectedProviders.length > 0 ? 'var(--color-success)' : 'var(--color-warning)' }}>
                  {connectedProviders.length > 0 ? `${connectedProviders.length} live` : 'CHECK'}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Footer */}
      <div className="grid-row instances-footer-row">
        <div className="cell">
          <div className="label-text">PROVIDER</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {providerSummary.length > 0 ? providerSummary.join(', ') : '—'}
          </div>
        </div>
        <div className="cell">
          <div className="label-text">TOTAL COST</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            ${filteredInstances.reduce((sum, i) => sum + i.cost_per_hour, 0).toFixed(2)}/hr
          </div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text">TAGS</div>
          <div style={{ display: 'flex', gap: '0.75rem', marginTop: '0.5rem' }}>
            <span className="badge">INFERENCE</span>
            <span className="badge">GPU</span>
            <span className="badge">PRODUCTION</span>
          </div>
        </div>
      </div>

      <ProvisionModal
        isOpen={showProvisionModal}
        onClose={() => { setShowProvisionModal(false); setPreselectedModel(null); }}
        offerings={offerings}
        preselectedModel={preselectedModel}
        providerStatuses={visibleProviderStatuses}
        configuredProviders={configuredProviders}
      />
    </div>
  );
}
