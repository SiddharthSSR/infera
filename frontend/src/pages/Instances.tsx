import { useState, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import type { Instance, GPUOffering, GPUType, VaultModel } from '../types';
import { useInstances, useOfferings, useTerminateInstance, useStartInstance, useStopInstance, useProvisionInstance, useVaultModels, useWorkers } from '../hooks/useApi';
import { useIsMobile } from '../hooks/useIsMobile';

const GPU_VRAM_GB: Record<GPUType, number> = {
  RTX_4090: 24,
  RTX_4080: 16,
  A100_40GB: 40,
  A100_80GB: 80,
  H100: 80,
  L40S: 48,
};

function getStatusClass(status: string) {
  return status === 'running' ? '' :
    status === 'error' ? 'error' :
    ['stopping', 'pending', 'provisioning'].includes(status) ? 'warning' : 'inactive';
}

function getStatusLabel(status: string) {
  return status.charAt(0).toUpperCase() + status.slice(1);
}

function InstanceRow({ instance }: { instance: Instance }) {
  const terminateMutation = useTerminateInstance();
  const startMutation = useStartInstance();
  const stopMutation = useStopInstance();
  const isLoading = terminateMutation.isPending || startMutation.isPending || stopMutation.isPending;

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
        {instance.status === 'stopped' && (
          <button
            className="action-btn"
            style={{ fontSize: '0.65rem', marginRight: '1rem' }}
            disabled={isLoading}
            onClick={async () => {
              try { await startMutation.mutateAsync(instance.id); toast.success('Instance started'); }
              catch { toast.error('Failed to start'); }
            }}
          >START</button>
        )}
        {instance.status === 'running' && (
          <button
            className="action-btn"
            style={{ fontSize: '0.65rem', marginRight: '1rem' }}
            disabled={isLoading}
            onClick={async () => {
              try { await stopMutation.mutateAsync(instance.id); toast.success('Instance stopped'); }
              catch { toast.error('Failed to stop'); }
            }}
          >STOP</button>
        )}
        <button
          className="action-btn destructive"
          style={{ fontSize: '0.65rem' }}
          disabled={isLoading}
          onClick={async () => {
            if (!confirm('Terminate this instance?')) return;
            try { await terminateMutation.mutateAsync(instance.id); toast.success('Terminated'); }
            catch { toast.error('Failed to terminate'); }
          }}
        >TERMINATE</button>
      </td>
    </tr>
  );
}

function ProvisionModal({ isOpen, onClose, offerings, preselectedModel }: {
  isOpen: boolean;
  onClose: () => void;
  offerings: GPUOffering[] | undefined;
  preselectedModel?: string | null;
}) {
  const [selectedGPU, setSelectedGPU] = useState<string>('');
  const [name, setName] = useState('');
  const [spotInstance, setSpotInstance] = useState(false);
  const [selectedModels, setSelectedModels] = useState<string[]>([]);
  const provisionMutation = useProvisionInstance();
  const { data: vaultModels } = useVaultModels({ status: 'available' });

  // Pre-select model when opening from registry DEPLOY
  useEffect(() => {
    if (preselectedModel && isOpen) {
      setSelectedModels(prev => prev.includes(preselectedModel) ? prev : [preselectedModel, ...prev]);
    }
  }, [preselectedModel, isOpen]);

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

  const selectedOffering = dedupedOfferings?.find(o => getOfferingKey(o) === selectedGPU);
  const selectedGPUVram = selectedOffering ? GPU_VRAM_GB[selectedOffering.gpu_type] : undefined;

  const allVaultModels = vaultModels?.models;
  const compatibleModels = allVaultModels?.filter((m: VaultModel) => {
    if (!selectedGPUVram) return true;
    return m.vram_required <= selectedGPUVram * 1024;
  });

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
      toast.success('Instance provisioned');
      onClose();
      setName(''); setSelectedGPU(''); setSelectedModels([]);
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
        position: 'fixed', inset: '1rem', maxWidth: 900, margin: '0 auto',
        background: 'var(--bg-paper)', border: 'var(--grid-line)', zIndex: 50,
        display: 'flex', flexDirection: 'column', overflow: 'hidden'
      }}>
        {/* Header */}
        <div style={{ padding: '2rem', borderBottom: 'var(--grid-line)' }}>
          <div className="label-text" style={{ marginBottom: '0.5rem' }}>PROVISION NEW NODE</div>
          <div style={{ fontSize: '0.9rem', color: 'var(--text-secondary)' }}>Select GPU configuration and models to deploy</div>
        </div>

        {/* Content */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '2rem' }}>
          <div style={{ marginBottom: '2rem' }}>
            <div className="label-text">INSTANCE NAME</div>
            <input type="text" className="control-input" value={name} onChange={e => setName(e.target.value)} placeholder="infera-worker" style={{ marginTop: '0.5rem' }} />
          </div>

          <div className="label-text" style={{ marginBottom: '1rem' }}>GPU CONFIGURATION</div>
          <div className="provision-options-grid" style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '0.75rem', marginBottom: '2rem' }}>
            {dedupedOfferings?.map(o => {
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
                  <div style={{ fontWeight: 600, fontSize: '1rem', marginBottom: '0.5rem' }}>
                    {o.gpu_count}x {o.gpu_type.replace('_', ' ')}
                  </div>
                  <div style={{ display: 'flex', gap: '0.75rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
                    <span>{o.vcpu} vCPU</span>
                    <span>{o.memory_gb}GB</span>
                  </div>
                  <div className="mono" style={{ marginTop: '0.75rem', fontSize: '1rem' }}>
                    ${o.cost_per_hour.toFixed(2)}<span style={{ fontSize: '0.75rem', color: 'var(--text-secondary)' }}>/hr</span>
                  </div>
                </button>
              );
            })}
          </div>

          {/* Models */}
          {allVaultModels && allVaultModels.length > 0 && (
            <div style={{ marginBottom: '2rem' }}>
              <div className="label-text" style={{ marginBottom: '0.5rem' }}>MODELS TO DEPLOY</div>
              <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginBottom: '1rem' }}>
                {selectedGPUVram ? `Showing models that fit within ${selectedGPUVram}GB VRAM` : 'Select a GPU to filter compatible models'}
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.5rem' }}>
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
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            <input type="checkbox" checked={spotInstance} onChange={e => setSpotInstance(e.target.checked)} />
            <span style={{ fontSize: '0.9rem' }}>Spot Instance (up to 70% cheaper, may be interrupted)</span>
          </div>
        </div>

        {/* Footer */}
        <div className="provision-modal-footer" style={{ padding: '1.5rem 2rem', borderTop: 'var(--grid-line)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <button className="action-btn" onClick={onClose}>CANCEL</button>
          <button className="btn-primary" onClick={handleProvision} disabled={!selectedGPU || provisionMutation.isPending}>
            {provisionMutation.isPending ? 'PROVISIONING...' : 'PROVISION NODE'}
          </button>
        </div>
      </div>
    </>
  );
}

function InstanceCard({ instance }: { instance: Instance }) {
  const terminateMutation = useTerminateInstance();
  const startMutation = useStartInstance();
  const stopMutation = useStopInstance();
  const isLoading = terminateMutation.isPending || startMutation.isPending || stopMutation.isPending;
  const statusClass = getStatusClass(instance.status);
  const statusLabel = getStatusLabel(instance.status);

  return (
    <div className="mobile-data-card">
      <div className="mobile-data-card-header">
        <div>
          <div className="mobile-data-title mono" style={{ fontSize: '0.9rem' }}>{instance.name || instance.id.slice(0, 16)}</div>
          <div className="mobile-data-subtitle">
            {instance.gpu_count}x {instance.gpu_type.replace('_', ' ')}
          </div>
        </div>
        <div className="mobile-status-inline">
          <span className={`status-dot ${statusClass}`} />
          {statusLabel}
        </div>
      </div>
      <div className="mobile-data-meta">
        <div><span className="label-text">COST</span> <span className="mono">${instance.cost_per_hour.toFixed(2)}/hr</span></div>
        <div><span className="label-text">ENDPOINT</span> <span className="mono">{instance.public_ip || '-'}</span></div>
      </div>
      <div className="mobile-data-actions">
        {instance.status === 'stopped' && (
          <button
            className="action-btn"
            style={{ fontSize: '0.65rem' }}
            disabled={isLoading}
            onClick={async () => {
              try { await startMutation.mutateAsync(instance.id); toast.success('Instance started'); }
              catch { toast.error('Failed to start'); }
            }}
          >START</button>
        )}
        {instance.status === 'running' && (
          <button
            className="action-btn"
            style={{ fontSize: '0.65rem' }}
            disabled={isLoading}
            onClick={async () => {
              try { await stopMutation.mutateAsync(instance.id); toast.success('Instance stopped'); }
              catch { toast.error('Failed to stop'); }
            }}
          >STOP</button>
        )}
        <button
          className="action-btn destructive"
          style={{ fontSize: '0.65rem' }}
          disabled={isLoading}
          onClick={async () => {
            if (!confirm('Terminate this instance?')) return;
            try { await terminateMutation.mutateAsync(instance.id); toast.success('Terminated'); }
            catch { toast.error('Failed to terminate'); }
          }}
        >TERMINATE</button>
      </div>
    </div>
  );
}

export function Instances() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [showProvisionModal, setShowProvisionModal] = useState(false);
  const [statusFilter, setStatusFilter] = useState<string>('active');
  const isMobile = useIsMobile(900);
  const { data: instances, isLoading } = useInstances();
  const { data: offerings } = useOfferings();
  const { data: workers } = useWorkers();

  const [preselectedModel, setPreselectedModel] = useState<string | null>(null);

  // Auto-open provision modal if redirected from dashboard or registry
  useEffect(() => {
    if (searchParams.get('provision') === 'true') {
      const model = searchParams.get('model');
      if (model) setPreselectedModel(model);
      setShowProvisionModal(true);
      setSearchParams({}, { replace: true });
    }
  }, [searchParams, setSearchParams]);

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

  return (
    <div className="animate-fade-in">
      {/* Metrics Row */}
      <div className="grid-row">
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
      <div className="grid-row" style={{ flexGrow: 1 }}>
        {/* Node Table */}
        <div className="cell" style={{ gridColumn: 'span 3' }}>
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
              <div style={{ fontSize: '0.9rem', marginBottom: '1rem' }}>No instances found</div>
              <button className="action-btn" onClick={() => setShowProvisionModal(true)}>PROVISION NEW NODE</button>
            </div>
          ) : isMobile ? (
            <div className="mobile-data-list">
              {filteredInstances.map(instance => (
                <InstanceCard key={instance.id} instance={instance} />
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
        <div className="cell" style={{ backgroundColor: 'var(--bg-accent)' }}>
          <div className="label-text" style={{ marginBottom: '2rem' }}>CLUSTER INFO</div>

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
            </div>
          </div>
        </div>
      </div>

      {/* Footer */}
      <div className="grid-row" style={{ borderBottom: 'none' }}>
        <div className="cell">
          <div className="label-text">PROVIDER</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {filteredInstances[0]?.provider || 'runpod'}
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
      />
    </div>
  );
}
