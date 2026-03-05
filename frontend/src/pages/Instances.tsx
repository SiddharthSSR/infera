import { useState } from 'react';
import {
  Server, Plus, Cpu, DollarSign, Clock,
  AlertCircle, CheckCircle2, Loader2, Play, Square, Trash2,
  Copy, Check, ExternalLink, Zap, Filter
} from 'lucide-react';
import { toast } from 'sonner';
import { cn } from '../lib/utils';
import type { Instance, GPUOffering, GPUType, InstanceStatus, VaultModel } from '../types';
import { useInstances, useOfferings, useTerminateInstance, useStartInstance, useStopInstance, useProvisionInstance, useVaultModels } from '../hooks/useApi';

function StatusBadge({ status }: { status: InstanceStatus }) {
  const config: Record<InstanceStatus, { variant: string; icon: typeof CheckCircle2; spin?: boolean }> = {
    pending: { variant: 'bg-warning/10 text-warning border-warning/20', icon: Clock },
    provisioning: { variant: 'bg-primary/10 text-primary border-primary/20', icon: Loader2, spin: true },
    running: { variant: 'bg-success/10 text-success border-success/20', icon: CheckCircle2 },
    stopping: { variant: 'bg-warning/10 text-warning border-warning/20', icon: Clock },
    stopped: { variant: 'bg-muted text-muted-foreground border-border', icon: Square },
    terminating: { variant: 'bg-destructive/10 text-destructive border-destructive/20', icon: Loader2, spin: true },
    terminated: { variant: 'bg-muted text-muted-foreground border-border', icon: Square },
    error: { variant: 'bg-destructive/10 text-destructive border-destructive/20', icon: AlertCircle },
  };

  const { variant, icon: Icon, spin } = config[status];

  return (
    <span className={cn("inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border", variant)}>
      <Icon className={cn("w-3 h-3", spin && "animate-spin")} />
      <span className="capitalize">{status}</span>
    </span>
  );
}

function InstanceCard({ instance }: { instance: Instance }) {
  const [copied, setCopied] = useState(false);
  const terminateMutation = useTerminateInstance();
  const startMutation = useStartInstance();
  const stopMutation = useStopInstance();

  const handleTerminate = async () => {
    if (!confirm('Terminate this instance?')) return;
    try {
      await terminateMutation.mutateAsync(instance.id);
      toast.success('Instance terminated');
    } catch {
      toast.error('Failed to terminate instance');
    }
  };

  const handleStart = async () => {
    try {
      await startMutation.mutateAsync(instance.id);
      toast.success('Instance started');
    } catch {
      toast.error('Failed to start instance');
    }
  };

  const handleStop = async () => {
    try {
      await stopMutation.mutateAsync(instance.id);
      toast.success('Instance stopped');
    } catch {
      toast.error('Failed to stop instance');
    }
  };

  const copyEndpoint = () => {
    navigator.clipboard.writeText(`${instance.public_ip}:${instance.http_port}`);
    setCopied(true);
    toast.success('Copied to clipboard');
    setTimeout(() => setCopied(false), 2000);
  };

  const isRunning = instance.status === 'running';
  const isStopped = instance.status === 'stopped';
  const isLoading = terminateMutation.isPending || startMutation.isPending || stopMutation.isPending;

  return (
    <div className="bg-card border border-border rounded-xl p-6 hover:border-primary/30 transition-all animate-fade-in">
      <div className="flex items-start justify-between mb-4">
        <div className="flex items-center gap-3">
          <div className={cn("w-12 h-12 rounded-xl flex items-center justify-center", isRunning ? "bg-success/10 border border-success/20" : "bg-muted border border-border")}>
            <Server className={cn("w-6 h-6", isRunning ? "text-success" : "text-muted-foreground")} />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-semibold text-foreground">{instance.name}</span>
              {instance.spot_instance && (
                <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-warning/10 text-warning border border-warning/20">
                  <Zap className="w-2.5 h-2.5" />Spot
                </span>
              )}
            </div>
            <div className="flex items-center gap-2 text-xs text-muted-foreground mt-0.5">
              <span className="capitalize">{instance.provider}</span>
              <span>•</span>
              <span className="font-mono">{instance.id.slice(0, 12)}</span>
            </div>
          </div>
        </div>
        <StatusBadge status={instance.status} />
      </div>

      <div className="flex flex-wrap items-center gap-2 mb-4">
        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-accent text-accent-foreground border border-border">
          <Cpu className="w-3 h-3" />
          {instance.gpu_count}x {instance.gpu_type.replace('_', ' ')}
        </span>
        {instance.models && instance.models.length > 0 && instance.models.map(model => (
          <span key={model} className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-primary/10 text-primary border border-primary/20">
            {model.split('/').pop()}
          </span>
        ))}
      </div>

      {isRunning && instance.public_ip && (
        <div className="mb-4 p-3 bg-muted/50 rounded-lg border border-border">
          <div className="flex items-center justify-between">
            <div>
              <div className="text-xs text-muted-foreground mb-1">HTTP Endpoint</div>
              <code className="text-sm text-primary font-mono">{instance.public_ip}:{instance.http_port}</code>
            </div>
            <div className="flex items-center gap-1">
              <button onClick={copyEndpoint} className="p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
                {copied ? <Check className="w-4 h-4 text-success" /> : <Copy className="w-4 h-4" />}
              </button>
              <a href={`http://${instance.public_ip}:${instance.http_port}/health`} target="_blank" rel="noopener noreferrer" className="p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
                <ExternalLink className="w-4 h-4" />
              </a>
            </div>
          </div>
        </div>
      )}

      {instance.error && (
        <div className="mb-4 p-3 bg-destructive/5 border border-destructive/20 rounded-lg">
          <div className="flex items-start gap-2">
            <AlertCircle className="w-4 h-4 text-destructive flex-shrink-0 mt-0.5" />
            <p className="text-sm text-destructive">{instance.error}</p>
          </div>
        </div>
      )}

      <div className="flex items-center justify-between pt-4 border-t border-border">
        <div className="flex items-center gap-1.5">
          <DollarSign className="w-4 h-4 text-success" />
          <span className="text-lg font-semibold text-foreground font-mono tabular-nums">{instance.cost_per_hour.toFixed(2)}</span>
          <span className="text-sm text-muted-foreground">/hr</span>
        </div>

        <div className="flex items-center gap-2">
          {isStopped && (
            <button onClick={handleStart} disabled={isLoading} className="inline-flex items-center gap-2 px-3 py-1.5 text-sm rounded-lg bg-success/10 text-success border border-success/20 hover:bg-success hover:text-success-foreground transition-colors disabled:opacity-50">
              {startMutation.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}
              <span>Start</span>
            </button>
          )}
          {isRunning && (
            <button onClick={handleStop} disabled={isLoading} className="inline-flex items-center gap-2 px-3 py-1.5 text-sm rounded-lg bg-secondary text-secondary-foreground border border-border hover:bg-accent transition-colors disabled:opacity-50">
              {stopMutation.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Square className="w-4 h-4" />}
              <span>Stop</span>
            </button>
          )}
          <button onClick={handleTerminate} disabled={isLoading} className="inline-flex items-center gap-2 px-3 py-1.5 text-sm rounded-lg bg-destructive/10 text-destructive border border-destructive/20 hover:bg-destructive hover:text-destructive-foreground transition-colors disabled:opacity-50">
            {terminateMutation.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Trash2 className="w-4 h-4" />}
            <span>Terminate</span>
          </button>
        </div>
      </div>
    </div>
  );
}

const GPU_VRAM_GB: Record<GPUType, number> = {
  RTX_4090: 24,
  RTX_4080: 16,
  A100_40GB: 40,
  A100_80GB: 80,
  H100: 80,
  L40S: 48,
};

function ProvisionModal({ isOpen, onClose, offerings }: { isOpen: boolean; onClose: () => void; offerings: GPUOffering[] | undefined }) {
  const [selectedGPU, setSelectedGPU] = useState<string>('');
  const [name, setName] = useState('');
  const [spotInstance, setSpotInstance] = useState(false);
  const [selectedModels, setSelectedModels] = useState<string[]>([]);
  const provisionMutation = useProvisionInstance();
  const { data: vaultModels } = useVaultModels({ status: 'available' });

  const getOfferingKey = (o: GPUOffering) =>
    `${o.provider}-${o.gpu_type}-${o.gpu_count}-${o.memory_gb}-${o.vcpu}`;

  // Get selected GPU type for VRAM filtering
  const selectedOffering = offerings?.find(o => getOfferingKey(o) === selectedGPU);
  const selectedGPUVram = selectedOffering ? GPU_VRAM_GB[selectedOffering.gpu_type] : undefined;

  // Filter vault models by VRAM compatibility
  const allVaultModels = vaultModels?.models;
  const compatibleModels = allVaultModels?.filter((m: VaultModel) => {
    if (!selectedGPUVram) return true;
    // vram_required is in MB, GPU_VRAM_GB is in GB
    return m.vram_required <= selectedGPUVram * 1024;
  });

  const toggleModel = (sourceUri: string) => {
    setSelectedModels(prev =>
      prev.includes(sourceUri)
        ? prev.filter(id => id !== sourceUri)
        : [...prev, sourceUri]
    );
  };

  const handleProvision = async () => {
    if (!selectedGPU) return;
    const offering = offerings?.find(o => getOfferingKey(o) === selectedGPU);
    if (!offering) return;

    try {
      await provisionMutation.mutateAsync({
        name: name || 'infera-worker',
        provider: offering.provider,
        gpu_type: offering.gpu_type,
        gpu_count: offering.gpu_count,
        spot_instance: spotInstance,
        models: selectedModels.length > 0 ? selectedModels : undefined,
      });
      toast.success('Instance provisioned');
      onClose();
      setName('');
      setSelectedGPU('');
      setSelectedModels([]);
    } catch {
      toast.error('Failed to provision instance');
    }
  };

  const handleSelectGPU = (key: string) => {
    setSelectedGPU(prev => prev === key ? '' : key);
  };

  if (!isOpen) return null;

  // Deduplicate offerings by key, keeping the cheapest for each unique config
  const dedupedOfferings = offerings ? Array.from(
    offerings.reduce((map, o) => {
      const key = getOfferingKey(o);
      const existing = map.get(key);
      if (!existing || o.cost_per_hour < existing.cost_per_hour) {
        map.set(key, o);
      }
      return map;
    }, new Map<string, GPUOffering>()).values()
  ) : undefined;

  const groupedOfferings = dedupedOfferings?.reduce((acc, o) => {
    if (!acc[o.provider]) acc[o.provider] = [];
    acc[o.provider].push(o);
    return acc;
  }, {} as Record<string, GPUOffering[]>);

  return (
    <>
      <div className="fixed inset-0 bg-background/80 backdrop-blur-sm z-50 animate-fade-in" onClick={onClose} />
      <div className="fixed inset-4 md:inset-8 lg:inset-12 bg-card border border-border rounded-2xl shadow-2xl z-50 overflow-hidden flex flex-col animate-scale-in">
        {/* Header */}
        <div className="p-6 border-b border-border flex-shrink-0">
          <h2 className="text-2xl font-semibold text-foreground">Provision GPU Instance</h2>
          <p className="text-sm text-muted-foreground mt-1">Select a GPU configuration to deploy your inference worker</p>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-6">
          <div className="max-w-5xl mx-auto space-y-8">
            <div className="max-w-md">
              <label className="block text-sm font-medium text-foreground mb-2">Instance Name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="infera-worker"
                className="w-full bg-input border border-border rounded-lg px-4 py-3 text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-4">GPU Configuration</label>

              {groupedOfferings && Object.entries(groupedOfferings).map(([provider, providerOfferings]) => (
                <div key={provider} className="mb-6">
                  <div className="flex items-center gap-2 mb-3">
                    <div className="w-2 h-2 rounded-full bg-primary" />
                    <span className="text-sm font-semibold text-foreground uppercase tracking-wide">{provider}</span>
                    <span className="text-xs text-muted-foreground">({providerOfferings.length} options)</span>
                  </div>

                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
                    {providerOfferings.map((offering) => {
                      const key = getOfferingKey(offering);
                      const isSelected = selectedGPU === key;
                      return (
                        <button
                          key={key}
                          onClick={() => handleSelectGPU(key)}
                          className={cn(
                            "p-4 rounded-xl border-2 text-left transition-all duration-200",
                            isSelected
                              ? "bg-primary/15 border-primary shadow-[0_0_0_1px_var(--primary),0_0_16px_-4px_var(--primary)] scale-[1.02]"
                              : "bg-card border-border hover:border-primary/50 hover:bg-muted/30"
                          )}
                        >
                          <div className="flex items-start justify-between mb-3">
                            <div className={cn(
                              "w-12 h-12 rounded-xl flex items-center justify-center transition-colors",
                              isSelected ? "bg-primary text-primary-foreground" : "bg-muted"
                            )}>
                              <Cpu className={cn("w-6 h-6", isSelected ? "text-primary-foreground" : "text-muted-foreground")} />
                            </div>
                            {isSelected && (
                              <div className="flex items-center gap-1 px-2 py-1 rounded-full bg-primary/20 text-primary text-xs font-medium">
                                <Check className="w-3 h-3" />
                                Selected
                              </div>
                            )}
                          </div>

                          <div className={cn("font-semibold text-lg mb-1", isSelected ? "text-primary" : "text-foreground")}>
                            {offering.gpu_count}x {offering.gpu_type.replace('_', ' ')}
                          </div>

                          <div className="flex flex-wrap gap-2 mb-3">
                            <span className="px-2 py-0.5 rounded-md bg-muted text-xs text-muted-foreground">
                              {offering.vcpu} vCPU
                            </span>
                            <span className="px-2 py-0.5 rounded-md bg-muted text-xs text-muted-foreground">
                              {offering.memory_gb}GB RAM
                            </span>
                          </div>

                          <div className={cn("text-xl font-bold font-mono tabular-nums", isSelected ? "text-primary" : "text-success")}>
                            ${offering.cost_per_hour.toFixed(2)}
                            <span className="text-sm font-normal text-muted-foreground">/hr</span>
                          </div>
                        </button>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>

            {/* Models to Deploy */}
            {allVaultModels && allVaultModels.length > 0 && (
              <div>
                <label className="block text-sm font-medium text-foreground mb-2">Models to Deploy</label>
                <p className="text-xs text-muted-foreground mb-3">
                  {selectedGPUVram
                    ? `Showing models that fit within ${selectedGPUVram}GB VRAM`
                    : 'Select a GPU to filter compatible models'}
                </p>
                <div className="flex flex-wrap gap-2">
                  {(compatibleModels || []).map(model => {
                    const isSelected = selectedModels.includes(model.source_uri);
                    const vramGB = (model.vram_required / 1024).toFixed(0);
                    return (
                      <button
                        key={model.id}
                        onClick={() => toggleModel(model.source_uri)}
                        className={cn(
                          "inline-flex items-center gap-2 px-3 py-2 rounded-lg border text-sm transition-all",
                          isSelected
                            ? "bg-primary/15 border-primary text-primary"
                            : "bg-card border-border text-foreground hover:border-primary/50"
                        )}
                      >
                        {isSelected && <Check className="w-3.5 h-3.5" />}
                        <span className="font-medium">{model.name}</span>
                        {model.parameters && (
                          <span className="text-xs text-muted-foreground">{model.parameters}</span>
                        )}
                        <span className="px-1.5 py-0.5 rounded bg-muted text-xs text-muted-foreground">
                          {vramGB}GB
                        </span>
                      </button>
                    );
                  })}
                  {compatibleModels?.length === 0 && selectedGPUVram && (
                    <p className="text-sm text-muted-foreground">No models fit within {selectedGPUVram}GB VRAM</p>
                  )}
                </div>
              </div>
            )}

            <div className="flex items-center justify-between p-5 bg-muted/30 rounded-xl border border-border max-w-md">
              <div>
                <div className="flex items-center gap-2">
                  <Zap className="w-5 h-5 text-warning" />
                  <span className="font-semibold text-foreground">Spot Instance</span>
                </div>
                <p className="text-sm text-muted-foreground mt-1">Up to 70% cheaper, but may be interrupted</p>
              </div>
              <button
                onClick={() => setSpotInstance(!spotInstance)}
                className={cn(
                  "w-14 h-7 rounded-full transition-colors relative",
                  spotInstance ? "bg-primary" : "bg-muted"
                )}
              >
                <div className={cn(
                  "absolute w-6 h-6 bg-background rounded-full top-0.5 transition-all shadow-md",
                  spotInstance ? "left-7" : "left-0.5"
                )} />
              </button>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="p-6 border-t border-border flex-shrink-0 flex items-center justify-between bg-muted/20">
          <div className="text-sm text-muted-foreground">
            {selectedGPU ? (
              <span className="text-foreground">
                Selected: <span className="font-medium text-primary">{offerings?.find(o => getOfferingKey(o) === selectedGPU)?.gpu_type.replace('_', ' ')}</span>
              </span>
            ) : (
              'No GPU selected'
            )}
          </div>
          <div className="flex gap-3">
            <button
              onClick={onClose}
              className="px-5 py-2.5 rounded-lg bg-secondary text-secondary-foreground border border-border hover:bg-accent transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={handleProvision}
              disabled={!selectedGPU || provisionMutation.isPending}
              className="inline-flex items-center gap-2 px-5 py-2.5 rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {provisionMutation.isPending ? (
                <><Loader2 className="w-4 h-4 animate-spin" />Provisioning...</>
              ) : (
                <><Plus className="w-4 h-4" />Provision Instance</>
              )}
            </button>
          </div>
        </div>
      </div>
    </>
  );
}

export function Instances() {
  const [showProvisionModal, setShowProvisionModal] = useState(false);
  const [statusFilter, setStatusFilter] = useState<string>('active');
  const { data: instances, isLoading } = useInstances();
  const { data: offerings } = useOfferings();

  const filteredInstances = instances?.filter(instance => {
    if (statusFilter === 'active') return !['terminated', 'terminating'].includes(instance.status);
    if (statusFilter === 'all') return true;
    return instance.status === statusFilter;
  }) || [];

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <div className="relative">
            <select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} className="appearance-none bg-input border border-border rounded-lg px-4 py-2 pr-10 text-sm text-foreground focus:outline-none cursor-pointer">
              <option value="active">Active</option>
              <option value="running">Running</option>
              <option value="stopped">Stopped</option>
              <option value="all">All</option>
            </select>
            <Filter className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
          </div>
          <span className="text-sm text-muted-foreground">{filteredInstances.length} instance{filteredInstances.length !== 1 ? 's' : ''}</span>
        </div>

        <button onClick={() => setShowProvisionModal(true)} className="inline-flex items-center gap-2 px-4 py-2.5 rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 transition-colors">
          <Plus className="w-4 h-4" />New Instance
        </button>
      </div>

      {isLoading ? (
        <div className="grid md:grid-cols-2 xl:grid-cols-3 gap-4">
          {[1, 2, 3].map(i => <div key={i} className="h-64 bg-muted rounded-xl animate-pulse" />)}
        </div>
      ) : filteredInstances.length === 0 ? (
        <div className="bg-card border border-border rounded-xl p-6 text-center py-16">
          <div className="w-16 h-16 rounded-xl bg-muted flex items-center justify-center mx-auto mb-4">
            <Server className="w-8 h-8 text-muted-foreground" />
          </div>
          <h3 className="text-lg font-semibold text-foreground mb-2">No instances</h3>
          <p className="text-muted-foreground text-sm mb-6">Provision a GPU instance to get started</p>
          <button onClick={() => setShowProvisionModal(true)} className="inline-flex items-center gap-2 px-4 py-2.5 rounded-lg bg-primary text-primary-foreground">
            <Plus className="w-4 h-4" />Provision Instance
          </button>
        </div>
      ) : (
        <div className="grid md:grid-cols-2 xl:grid-cols-3 gap-4">
          {filteredInstances.map(instance => <InstanceCard key={instance.id} instance={instance} />)}
        </div>
      )}

      <ProvisionModal isOpen={showProvisionModal} onClose={() => setShowProvisionModal(false)} offerings={offerings} />
    </div>
  );
}
