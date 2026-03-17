import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import type { GPUOffering, Model, ProviderStatus, Instance, Worker } from '../types';
import { sendChatCompletion } from '../lib/api';
import {
  summarizeDeploymentAttempt,
  type DeploymentAttemptRecord,
  type DeploymentAttemptSummary,
} from '../lib/deploymentHistory';
import { getInstanceReadiness } from '../lib/instanceReadiness';
import { deriveModelRuntimeDrilldown } from '../lib/modelRuntimeDrilldown';
import { useDeploymentAttempts, useModels, useVaultModels, useRegisterVaultModel, useDeleteVaultModel, useOfferings, useProviders, useInstances, useUpdateDeploymentVerification, useWorkers } from '../hooks/useApi';
import { useIsMobile } from '../hooks/useIsMobile';
import { useAuthSession } from '../lib/auth-context';

const FAMILY_OPTIONS = ['mistral', 'llama', 'qwen', 'phi', 'gemma', 'deepseek', 'falcon', 'mixtral', 'yi', 'command-r'];
const QUANT_OPTIONS = ['none', 'GPTQ', 'AWQ', 'GGUF', 'FP8', 'INT8', 'INT4'];
const CONFIGURABLE_PROVIDERS = ['runpod', 'vastai'] as const;
const RECOMMENDED_MODEL_IDS = [
  'Qwen/Qwen3-4B-Thinking-2507',
  'moonshotai/Kimi-K2.5-Instruct',
] as const;
const RECOMMENDED_MODEL_LABELS: Record<string, string> = {
  'Qwen/Qwen3-4B-Thinking-2507': 'Budget Reasoning',
  'moonshotai/Kimi-K2.5-Instruct': 'High-Capacity',
};
const GPU_VRAM_GB: Record<string, number> = {
  RTX_4090: 24,
  RTX_4080: 16,
  A100_40GB: 40,
  A100_80GB: 80,
  H100: 80,
  L40S: 48,
};

type ModelServingOverview = {
  state: 'not_deployed' | 'runtime_pending' | 'serving_unverified' | 'serving_verified' | 'serving_failed' | 'degraded';
  summary: string;
  badgeLabel: string;
  badgeTone: '' | 'warning' | 'error' | 'inactive';
  activeInstances: number;
  verifiedAt?: string;
  latestVerificationError?: string;
  latestVerificationLatencyMs?: number;
  latestAttempt?: DeploymentAttemptSummary | null;
};

function describeDeployReadiness(model: Model, offerings: GPUOffering[], providers: ProviderStatus[]) {
  const connectedProviders = providers.filter((provider) => provider.connected);
  const requiredMB = model.vram_required || 0;
  const compatibleOfferings = offerings.filter((offering) => {
    if (!requiredMB) return true;
    const vramGB = GPU_VRAM_GB[offering.gpu_type] || 0;
    return vramGB * 1024 >= requiredMB;
  });
  const cheapest = compatibleOfferings.reduce<GPUOffering | null>((best, offering) => {
    if (!best || offering.cost_per_hour < best.cost_per_hour) return offering;
    return best;
  }, null);
  const providerNames = [...new Set(compatibleOfferings.map((offering) => offering.provider))];

  if (model.loaded !== false) {
    return {
      state: 'active' as const,
      summary: 'Already loaded on active infrastructure.',
      actionLabel: 'MANAGE',
      actionTarget: '/instances',
    };
  }

  if (model.vault_status === 'testing') {
    return {
      state: 'deploying' as const,
      summary: 'Provisioning or model load is already in progress.',
      actionLabel: 'VIEW CLUSTERS',
      actionTarget: '/instances',
    };
  }

  if (connectedProviders.length === 0) {
    return {
      state: 'setup' as const,
      summary: 'No live provider is connected for this workspace yet.',
      actionLabel: 'SETUP PROVIDER',
      actionTarget: '/workspace',
    };
  }

  if (compatibleOfferings.length === 0) {
    return {
      state: 'capacity' as const,
      summary: requiredMB
        ? `Needs about ${Math.ceil(requiredMB / 1024)}GB VRAM. No matching capacity is live right now.`
        : 'Provider capacity is connected, but no compatible inventory is live right now.',
      actionLabel: 'VIEW CAPACITY',
      actionTarget: '/instances',
    };
  }

  return {
    state: 'ready' as const,
    summary: `Ready on ${compatibleOfferings.length} GPU config${compatibleOfferings.length === 1 ? '' : 's'} via ${providerNames.join(', ')}${cheapest ? ` from $${cheapest.cost_per_hour.toFixed(2)}/hr` : ''}.`,
    actionLabel: 'DEPLOY',
    actionTarget: `/instances?provision=true&model=${encodeURIComponent(model.id)}`,
  };
}

function formatVerificationMeta(verifiedAt?: string, latencyMs?: number) {
  if (!verifiedAt) return null;
  const label = new Date(verifiedAt).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });
  if (latencyMs == null) return label;
  return `${label} in ${latencyMs < 1000 ? `${latencyMs}ms` : `${(latencyMs / 1000).toFixed(2)}s`}`;
}

function deriveModelServingOverview(
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

function RegisterModelModal({ isOpen, onClose }: { isOpen: boolean; onClose: () => void }) {
  const registerMutation = useRegisterVaultModel();
  const [form, setForm] = useState({
    name: '',
    source_uri: '',
    family: '',
    parameters: '',
    quantization: 'none',
    max_context: 4096,
    vram_required: 0,
    tags: '',
  });

  const set = (field: string, value: string | number) =>
    setForm(prev => ({ ...prev, [field]: value }));

  // Auto-fill name from source_uri
  const handleSourceUriChange = (uri: string) => {
    set('source_uri', uri);
    if (!form.name && uri.includes('/')) {
      const modelName = uri.split('/').pop() || '';
      set('name', modelName.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase()));
    }
  };

  const handleSubmit = async () => {
    if (!form.source_uri.trim()) {
      toast.error('HuggingFace model ID is required');
      return;
    }
    if (!form.name.trim()) {
      toast.error('Model name is required');
      return;
    }

    try {
      await registerMutation.mutateAsync({
        name: form.name.trim(),
        source_uri: form.source_uri.trim(),
        family: form.family || undefined,
        parameters: form.parameters || undefined,
        quantization: form.quantization !== 'none' ? form.quantization : undefined,
        max_context: form.max_context || undefined,
        vram_required: form.vram_required || undefined,
        tags: form.tags ? form.tags.split(',').map(t => t.trim()).filter(Boolean) : undefined,
      });
      toast.success(`Registered ${form.name}`);
      setForm({ name: '', source_uri: '', family: '', parameters: '', quantization: 'none', max_context: 4096, vram_required: 0, tags: '' });
      onClose();
    } catch {
      toast.error('Failed to register model');
    }
  };

  if (!isOpen) return null;

  const inputStyle = {
    width: '100%', padding: '0.6rem 0', background: 'transparent',
    border: 'none', borderBottom: '1px solid var(--border-color)',
    fontFamily: 'var(--font-main)', fontSize: '0.9rem', outline: 'none',
    color: 'var(--text-primary)',
  };

  return (
    <>
      <div
        style={{ position: 'fixed', inset: 0, background: 'rgba(253,251,248,0.8)', backdropFilter: 'blur(4px)', zIndex: 50 }}
        onClick={onClose}
      />
      <div style={{
        position: 'fixed', top: '50%', left: '50%', transform: 'translate(-50%, -50%)',
        width: 'min(580px, calc(100vw - 2rem))', maxHeight: '85vh',
        background: 'var(--bg-paper)', border: 'var(--grid-line)', zIndex: 50,
        display: 'flex', flexDirection: 'column', overflow: 'hidden',
      }}>
        {/* Header */}
        <div style={{ padding: '2rem 2rem 1.5rem', borderBottom: 'var(--grid-line)' }}>
          <div className="label-text" style={{ marginBottom: '0.5rem' }}>REGISTER MODEL</div>
          <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
            Add a HuggingFace model to the registry
          </div>
        </div>

        {/* Form */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '1.5rem 2rem' }}>
          {/* Source URI - primary field */}
          <div style={{ marginBottom: '1.75rem' }}>
            <div className="label-text" style={{ marginBottom: '0.5rem' }}>HUGGINGFACE MODEL ID *</div>
            <input
              type="text"
              value={form.source_uri}
              onChange={e => handleSourceUriChange(e.target.value)}
              placeholder="mistralai/Mistral-7B-Instruct-v0.3"
              style={inputStyle}
            />
            <div style={{ fontSize: '0.7rem', color: 'var(--text-secondary)', marginTop: '0.4rem' }}>
              Exact org/model-name from HuggingFace. Must match what vLLM loads.
            </div>
          </div>

          {/* Name */}
          <div style={{ marginBottom: '1.75rem' }}>
            <div className="label-text" style={{ marginBottom: '0.5rem' }}>DISPLAY NAME *</div>
            <input
              type="text"
              value={form.name}
              onChange={e => set('name', e.target.value)}
              placeholder="Mistral 7B Instruct v0.3"
              style={inputStyle}
            />
          </div>

          {/* Two-column row */}
          <div className="modal-two-col-row" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem', marginBottom: '1.75rem' }}>
            <div>
              <div className="label-text" style={{ marginBottom: '0.5rem' }}>FAMILY</div>
              <select
                value={form.family}
                onChange={e => set('family', e.target.value)}
                style={{ ...inputStyle, cursor: 'pointer' }}
              >
                <option value="">Select family...</option>
                {FAMILY_OPTIONS.map(f => (
                  <option key={f} value={f}>{f}</option>
                ))}
              </select>
            </div>
            <div>
              <div className="label-text" style={{ marginBottom: '0.5rem' }}>PARAMETERS</div>
              <input
                type="text"
                value={form.parameters}
                onChange={e => set('parameters', e.target.value)}
                placeholder="7B"
                style={inputStyle}
              />
            </div>
          </div>

          {/* Two-column row */}
          <div className="modal-two-col-row" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem', marginBottom: '1.75rem' }}>
            <div>
              <div className="label-text" style={{ marginBottom: '0.5rem' }}>QUANTIZATION</div>
              <select
                value={form.quantization}
                onChange={e => set('quantization', e.target.value)}
                style={{ ...inputStyle, cursor: 'pointer' }}
              >
                {QUANT_OPTIONS.map(q => (
                  <option key={q} value={q}>{q === 'none' ? 'None (FP16/BF16)' : q}</option>
                ))}
              </select>
            </div>
            <div>
              <div className="label-text" style={{ marginBottom: '0.5rem' }}>MAX CONTEXT</div>
              <input
                type="number"
                value={form.max_context}
                onChange={e => set('max_context', parseInt(e.target.value) || 0)}
                placeholder="32768"
                style={inputStyle}
              />
            </div>
          </div>

          {/* VRAM */}
          <div style={{ marginBottom: '1.75rem' }}>
            <div className="label-text" style={{ marginBottom: '0.5rem' }}>VRAM REQUIRED (MB)</div>
            <input
              type="number"
              value={form.vram_required || ''}
              onChange={e => set('vram_required', parseInt(e.target.value) || 0)}
              placeholder="16384"
              style={inputStyle}
            />
            <div style={{ fontSize: '0.7rem', color: 'var(--text-secondary)', marginTop: '0.4rem' }}>
              Approximate GPU memory needed. Used to filter compatible GPUs during provisioning.
            </div>
          </div>

          {/* Tags */}
          <div style={{ marginBottom: '1rem' }}>
            <div className="label-text" style={{ marginBottom: '0.5rem' }}>TAGS</div>
            <input
              type="text"
              value={form.tags}
              onChange={e => set('tags', e.target.value)}
              placeholder="chat, instruct, code"
              style={inputStyle}
            />
            <div style={{ fontSize: '0.7rem', color: 'var(--text-secondary)', marginTop: '0.4rem' }}>
              Comma-separated labels for filtering.
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="generic-modal-footer" style={{
          padding: '1.5rem 2rem', borderTop: 'var(--grid-line)',
          display: 'flex', justifyContent: 'space-between', alignItems: 'center',
        }}>
          <button className="action-btn" onClick={onClose}>CANCEL</button>
          <button
            className="btn-primary"
            onClick={handleSubmit}
            disabled={registerMutation.isPending || !form.source_uri.trim() || !form.name.trim()}
          >
            {registerMutation.isPending ? 'REGISTERING...' : 'REGISTER MODEL'}
          </button>
        </div>
      </div>
    </>
  );
}

export function Models() {
  const navigate = useNavigate();
  const isMobile = useIsMobile(900);
  const { session } = useAuthSession();
  const workspaceID = session?.workspace?.id;
  const { data: models } = useModels();
  const { data: vaultData } = useVaultModels({});
  const { data: offerings } = useOfferings();
  const { data: providers } = useProviders();
  const { data: instances } = useInstances();
  const { data: workers } = useWorkers();
  const deleteMutation = useDeleteVaultModel();
  const [searchQuery, setSearchQuery] = useState('');
  const [showRegisterModal, setShowRegisterModal] = useState(false);
  const [verifyingModelID, setVerifyingModelID] = useState<string | null>(null);
  const { data: deploymentAttempts = [] } = useDeploymentAttempts(workspaceID);
  const updateDeploymentVerification = useUpdateDeploymentVerification(workspaceID);

  const allModels = models || [];
  const vaultModels = vaultData?.models || [];
  const liveInstances = instances || [];

  // Build a lookup of vault model IDs by source_uri for delete actions
  const vaultIdByUri = new Map(vaultModels.map(vm => [vm.source_uri, vm.id]));

  // Merge: use /v1/models (which includes vault data) as primary, fall back to vault-only
  const displayModels = allModels.length > 0 ? allModels : vaultModels.map(vm => ({
    id: vm.source_uri,
    object: 'model',
    created: 0,
    owned_by: vm.source,
    loaded: false,
    family: vm.family,
    parameters: vm.parameters,
    quantization: vm.quantization,
    vram_required: vm.vram_required,
    max_context: vm.max_context,
    tags: vm.tags,
    vault_status: vm.status,
  }));

  const filtered = displayModels.filter(m => {
    if (!searchQuery) return true;
    const q = searchQuery.toLowerCase();
    return m.id.toLowerCase().includes(q) || m.family?.toLowerCase().includes(q) || m.owned_by?.toLowerCase().includes(q);
  });

  const visibleProviders = useMemo(
    () => (providers || []).filter((provider) => CONFIGURABLE_PROVIDERS.includes(provider.provider as typeof CONFIGURABLE_PROVIDERS[number])),
    [providers],
  );
  const visibleOfferings = useMemo(
    () => (offerings || []).filter((offering) => CONFIGURABLE_PROVIDERS.includes(offering.provider as typeof CONFIGURABLE_PROVIDERS[number])),
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
  const readyCount = filtered.filter((model) => describeDeployReadiness(model, visibleOfferings, visibleProviders).state === 'ready').length;
  const activeCount = filtered.filter((model) => (modelOverviewByID.get(model.id)?.activeInstances || 0) > 0).length;
  const servingVerifiedCount = filtered.filter((model) => modelOverviewByID.get(model.id)?.state === 'serving_verified').length;
  const recommendedModels = useMemo(
    () => RECOMMENDED_MODEL_IDS
      .map((id) => displayModels.find((model) => model.id === id))
      .filter((model): model is Model => Boolean(model)),
    [displayModels],
  );

  const handleRemove = async (modelId: string) => {
    const vaultId = vaultIdByUri.get(modelId);
    if (!vaultId) {
      toast.error('Model not found in vault registry');
      return;
    }
    if (!confirm('Remove this model from the registry?')) return;
    try {
      await deleteMutation.mutateAsync(vaultId);
      toast.success('Model removed from registry');
    } catch {
      toast.error('Failed to remove model');
    }
  };

  const handleVerifyServing = async (model: Model) => {
    const overview = modelOverviewByID.get(model.id);
    const attempt = overview?.latestAttempt?.attempt;

    setVerifyingModelID(model.id);
    const startedAt = Date.now();

    try {
      const response = await sendChatCompletion({
        model: model.id,
        messages: [
          { role: 'system', content: 'Reply with a short readiness confirmation.' },
          { role: 'user', content: 'Return a short response confirming that inference is working.' },
        ],
        temperature: 0,
        max_tokens: 16,
      });

      const latencyMs = Date.now() - startedAt;
      const content = response.choices?.[0]?.message?.content?.trim() || '';
      if (attempt) {
        await updateDeploymentVerification.mutateAsync({
          attemptId: attempt.id,
          verification: {
            status: 'passed',
            verified_at: new Date().toISOString(),
            latency_ms: latencyMs,
            model: model.id,
            response_preview: content.slice(0, 120),
          },
        });
      }
      toast.success(`Serving verified for ${model.id.split('/').pop()} in ${latencyMs < 1000 ? `${latencyMs}ms` : `${(latencyMs / 1000).toFixed(2)}s`}`);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Verification request failed';
      if (attempt) {
        await updateDeploymentVerification.mutateAsync({
          attemptId: attempt.id,
          verification: {
            status: 'failed',
            verified_at: new Date().toISOString(),
            model: model.id,
            error: message,
          },
        });
      }
      toast.error(message);
    } finally {
      setVerifyingModelID(null);
    }
  };

  return (
    <div className="animate-fade-in">
      {/* Search Bar */}
      <div style={{
        padding: '1rem 2rem',
        display: 'flex',
        justifyContent: 'space-between',
        flexWrap: 'wrap',
        gap: '1rem',
        alignItems: 'center',
        borderBottom: 'var(--grid-line)',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" />
          </svg>
          <input
            type="text"
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            placeholder="Filter by model name or provider..."
            style={{
              background: 'transparent', border: 'none', fontFamily: 'var(--font-main)',
              fontSize: '0.9rem', width: 'min(300px, 62vw)', outline: 'none', color: 'var(--text-primary)',
            }}
          />
        </div>
        <button className="btn-primary" onClick={() => setShowRegisterModal(true)}>
          ADD MODEL
        </button>
      </div>

      <div className="grid-row">
        <div className="cell" style={{ gridColumn: 'span 4' }}>
          <div className="help-callout">
            <div className="label-text">MODEL STATUS GUIDE</div>
            <div className="help-callout-copy">
              <strong>Serving verified</strong> means a live inference check passed for this model. <strong>Fresh verify</strong> means that proof is recent enough to trust immediately. <strong>Stale verify</strong> means the model was verified before, but the proof is old. Use <strong>View deployments</strong> or <strong>Open degraded nodes</strong> for node-level recovery and <strong>Verify now</strong> when you want a new explicit inference check from the registry.
            </div>
            <div className="help-actions">
              <button className="action-btn" onClick={() => navigate('/instances')}>OPEN CLUSTERS</button>
              <button className="action-btn" onClick={() => navigate('/docs')}>READ API DOCS</button>
            </div>
          </div>
        </div>
      </div>

      {recommendedModels.length > 0 && (
        <div className="grid-row">
          <div className="cell" style={{ gridColumn: 'span 4' }}>
            <div className="help-callout">
              <div className="label-text">RECOMMENDED NOW</div>
              <div className="help-callout-copy">
                Curated picks from the expanded catalog. Qwen is the lighter reasoning option to trial quickly; Kimi is the high-capacity frontier candidate and will usually need larger infrastructure than the current default fleet.
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: '0.85rem', marginTop: '1rem' }}>
                {recommendedModels.map((model) => {
                  const deployState = describeDeployReadiness(model, visibleOfferings, visibleProviders);
                  const overview = modelOverviewByID.get(model.id);
                  return (
                    <div key={model.id} className="workspace-provider-card">
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.75rem', alignItems: 'flex-start' }}>
                        <div>
                          <div style={{ fontSize: '1rem', fontWeight: 600 }}>{model.id.split('/').pop() || model.id}</div>
                          <div className="mono" style={{ marginTop: '0.3rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
                            {model.parameters || 'N/A'} · {model.family || model.owned_by || 'model'}
                          </div>
                        </div>
                        <div style={{ display: 'flex', gap: '0.35rem', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                          {RECOMMENDED_MODEL_LABELS[model.id] ? <span className="badge">{RECOMMENDED_MODEL_LABELS[model.id]}</span> : null}
                          {overview ? <span className={`badge ${overview.badgeTone ? `status-${overview.badgeTone}` : ''}`}>{overview.badgeLabel}</span> : null}
                        </div>
                      </div>
                      <div style={{ marginTop: '0.8rem', color: 'var(--text-secondary)', fontSize: '0.82rem', lineHeight: 1.6 }}>
                        {deployState.summary}
                      </div>
                      <div style={{ marginTop: '0.9rem', display: 'flex', gap: '0.6rem', flexWrap: 'wrap' }}>
                        <button className="action-btn" onClick={() => navigate(deployState.actionTarget)}>
                          {deployState.actionLabel}
                        </button>
                        <button className="action-btn" onClick={() => setSearchQuery(model.id.split('/').pop() || model.id)}>
                          FILTER
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        </div>
      )}

      {isMobile ? (
        <div className="mobile-data-list" style={{ padding: '1rem' }}>
          {filtered.length === 0 ? (
            <div style={{ padding: '2rem 0', textAlign: 'center', color: 'var(--text-secondary)' }}>
              {searchQuery ? 'No models match your search.' : 'No models in registry. Add one to get started.'}
              {!searchQuery && (
                <div className="help-actions" style={{ justifyContent: 'center' }}>
                  <button className="action-btn" onClick={() => setShowRegisterModal(true)}>ADD MODEL</button>
                  <button className="action-btn" onClick={() => navigate('/instances')}>OPEN CLUSTERS</button>
                  <button className="action-btn" onClick={() => navigate('/getting-started')}>OPEN QUICKSTART</button>
                </div>
              )}
            </div>
          ) : (
            filtered.map(model => {
              const isLoaded = model.loaded !== false;
              const isDeploying = model.vault_status === 'testing';
              const statusDotClass = isLoaded ? '' : isDeploying ? 'warning' : 'inactive';
              const statusLabel = isLoaded ? 'Active' : isDeploying ? 'Deploying...' : 'Available';
              const deployState = describeDeployReadiness(model, visibleOfferings, visibleProviders);
              const overview = modelOverviewByID.get(model.id)!;
              const runtime = modelRuntimeByID.get(model.id)!;
              const shortName = model.id.split('/').pop() || model.id;
              const provider = model.owned_by || model.family || '';
              const hasVaultEntry = vaultIdByUri.has(model.id);
              const deploymentsTarget = `/instances?model=${encodeURIComponent(model.id)}`;
              const degradedTarget = `/instances?model=${encodeURIComponent(model.id)}&focus=degraded`;

              return (
                <div key={model.id} className="mobile-data-card">
                  <div className="mobile-data-card-header">
                    <div>
                      <div className="mobile-data-title">{shortName}</div>
                      <div className="mobile-data-subtitle mono">
                        {model.parameters && `${model.parameters} — `}{provider}
                      </div>
                    </div>
                    <div className="mobile-status-inline">
                      <span className={`status-dot ${statusDotClass}`} />
                      {statusLabel}
                    </div>
                  </div>

                  <div className="mobile-data-meta">
                    <div><span className="label-text">QUANT</span> <span>{model.quantization || 'FP16'}</span></div>
                    <div><span className="label-text">CONTEXT</span> <span className="mono">{model.max_context ? model.max_context.toLocaleString() : 'N/A'}</span></div>
                    <div><span className="label-text">SERVING</span> <span className={`badge ${overview.badgeTone ? `status-${overview.badgeTone}` : ''}`}>{overview.badgeLabel}</span></div>
                    <div><span className="label-text">DEPLOYMENTS</span> <span className="mono">{runtime.activeNodes}</span></div>
                    <div><span className="label-text">VERIFY</span> <span className={`badge ${runtime.verificationFreshness === 'stale' ? 'status-warning' : runtime.verificationFreshness === 'never' ? 'status-inactive' : ''}`}>{runtime.verificationLabel}</span></div>
                    {runtime.degradedNodes > 0 && (
                      <div><span className="label-text">DEGRADED</span> <span className="mono">{runtime.degradedNodes} node{runtime.degradedNodes === 1 ? '' : 's'}</span></div>
                    )}
                    <div><span className="label-text">DEPLOY</span> <span>{deployState.summary}</span></div>
                    <div><span className="label-text">STATUS</span> <span>{overview.summary}</span></div>
                    {overview.verifiedAt && (
                      <div><span className="label-text">LAST VERIFY</span> <span>{formatVerificationMeta(overview.verifiedAt, overview.latestVerificationLatencyMs)}</span></div>
                    )}
                    {runtime.latestIssue && (
                      <div><span className="label-text">LATEST ISSUE</span> <span>{runtime.latestIssue}</span></div>
                    )}
                  </div>

                  <div className="mobile-data-actions">
                    <button
                      type="button"
                      className="mobile-data-action"
                      onClick={() => navigate(runtime.activeNodes > 0 ? deploymentsTarget : deployState.actionTarget)}
                    >
                      {runtime.activeNodes > 0 ? 'VIEW DEPLOYMENTS' : deployState.actionLabel}
                    </button>
                    {runtime.activeNodes > 0 ? (
                      <button
                        type="button"
                        className={`mobile-data-action${overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh' ? ' muted' : ''}`}
                        disabled={verifyingModelID === model.id}
                        onClick={() => overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh'
                          ? navigate(`/instances?provision=true&model=${encodeURIComponent(model.id)}`)
                          : handleVerifyServing(model)}
                      >
                        {verifyingModelID === model.id ? 'VERIFYING...' : overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh' ? 'DEPLOY MORE' : 'VERIFY NOW'}
                      </button>
                    ) : null}
                    {runtime.degradedNodes > 0 && (
                      <button
                        type="button"
                        className="mobile-data-action muted"
                        onClick={() => navigate(degradedTarget)}
                      >
                        OPEN DEGRADED NODES
                      </button>
                    )}
                    {hasVaultEntry && !isLoaded && !isDeploying && (
                      <button
                        type="button"
                        className="mobile-data-action danger"
                        onClick={() => handleRemove(model.id)}
                      >
                        REMOVE
                      </button>
                    )}
                  </div>
                </div>
              );
            })
          )}
        </div>
      ) : (
      <div className="responsive-scroll-x">
        <div className="responsive-scroll-x-content">
          {/* Table Header */}
          <div style={{
            display: 'grid',
            gridTemplateColumns: '2fr 1fr 1fr 1fr 120px',
            padding: '1rem 2rem',
            backgroundColor: 'var(--bg-accent)',
            borderBottom: 'var(--grid-line)',
          }}>
            <div className="label-text">MODEL NAME &amp; VERSION</div>
            <div className="label-text">STATUS</div>
            <div className="label-text">QUANTIZATION</div>
            <div className="label-text">CONTEXT</div>
            <div className="label-text">ACTION</div>
          </div>

          {/* Table Rows */}
          <div style={{ flexGrow: 1 }}>
            {filtered.length === 0 ? (
              <div style={{ padding: '4rem 2rem', textAlign: 'center', color: 'var(--text-secondary)' }}>
                {searchQuery ? 'No models match your search.' : 'No models in registry. Add one to get started.'}
                {!searchQuery && (
                  <div className="help-actions" style={{ justifyContent: 'center' }}>
                    <button className="action-btn" onClick={() => setShowRegisterModal(true)}>ADD MODEL</button>
                    <button className="action-btn" onClick={() => navigate('/instances')}>OPEN CLUSTERS</button>
                    <button className="action-btn" onClick={() => navigate('/getting-started')}>OPEN QUICKSTART</button>
                  </div>
                )}
              </div>
            ) : (
              filtered.map(model => {
            const isLoaded = model.loaded !== false;
            const isDeploying = model.vault_status === 'testing';
            const statusDotClass = isLoaded ? '' : isDeploying ? 'warning' : 'inactive';
            const statusLabel = isLoaded ? 'Active' : isDeploying ? 'Deploying...' : 'Available';
            const deployState = describeDeployReadiness(model, visibleOfferings, visibleProviders);
            const overview = modelOverviewByID.get(model.id)!;
            const runtime = modelRuntimeByID.get(model.id)!;
            const shortName = model.id.split('/').pop() || model.id;
            const provider = model.owned_by || model.family || '';
            const hasVaultEntry = vaultIdByUri.has(model.id);
            const deploymentsTarget = `/instances?model=${encodeURIComponent(model.id)}`;
            const degradedTarget = `/instances?model=${encodeURIComponent(model.id)}&focus=degraded`;

            return (
              <div
                key={model.id}
                className={`model-row${isLoaded ? ' model-row-active' : ''}`}
              >
                <div>
                  <div style={{ fontSize: '1.25rem', fontWeight: 500 }}>{shortName}</div>
                  <div className="mono" style={{ color: 'var(--text-secondary)', marginTop: '0.25rem' }}>
                    {model.parameters && `${model.parameters} — `}{provider}
                  </div>
                </div>
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem' }}>
                    <span className={`status-dot ${statusDotClass}`} />
                    {statusLabel}
                  </div>
                  <div style={{ marginTop: '0.45rem', display: 'flex', gap: '0.45rem', flexWrap: 'wrap' }}>
                    <span className={`badge ${overview.badgeTone ? `status-${overview.badgeTone}` : ''}`}>{overview.badgeLabel}</span>
                    {runtime.activeNodes > 0 && <span className="badge">{runtime.activeNodes} DEPLOYMENT{runtime.activeNodes === 1 ? '' : 'S'}</span>}
                    <span className={`badge ${runtime.verificationFreshness === 'stale' ? 'status-warning' : runtime.verificationFreshness === 'never' ? 'status-inactive' : ''}`}>{runtime.verificationLabel}</span>
                    {runtime.degradedNodes > 0 && <span className="badge status-error">{runtime.degradedNodes} DEGRADED NODE{runtime.degradedNodes === 1 ? '' : 'S'}</span>}
                  </div>
                  <div style={{ marginTop: '0.45rem', fontSize: '0.78rem', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
                    {overview.summary}
                  </div>
                  {overview.verifiedAt && (
                    <div style={{ marginTop: '0.45rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
                      Last verify: {formatVerificationMeta(overview.verifiedAt, overview.latestVerificationLatencyMs)}
                    </div>
                  )}
                  {runtime.latestIssue && (
                    <div style={{ marginTop: '0.45rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
                      Latest issue: {runtime.latestIssue}
                    </div>
                  )}
                </div>
                <div>
                  <span className="badge">{model.quantization || 'FP16'}</span>
                  {model.vram_required ? (
                    <div style={{ marginTop: '0.5rem' }}>
                      <span className="badge mono">{Math.ceil(model.vram_required / 1024)}GB VRAM</span>
                    </div>
                  ) : null}
                </div>
                <div className="mono" style={{ color: 'var(--text-secondary)' }}>
                  {model.max_context ? model.max_context.toLocaleString() : 'N/A'}
                </div>
                <div style={{ display: 'flex', gap: '0.75rem' }}>
                  <button
                    type="button"
                    className="action-link"
                    onClick={() => navigate(runtime.activeNodes > 0 ? deploymentsTarget : deployState.actionTarget)}
                  >
                    {runtime.activeNodes > 0 ? 'VIEW DEPLOYMENTS' : deployState.actionLabel}
                  </button>
                  {runtime.activeNodes > 0 && (
                    <button
                      type="button"
                      className={`action-link${overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh' ? ' muted' : ''}`}
                      disabled={verifyingModelID === model.id}
                      onClick={() => overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh'
                        ? navigate(`/instances?provision=true&model=${encodeURIComponent(model.id)}`)
                        : handleVerifyServing(model)}
                    >
                      {verifyingModelID === model.id ? 'VERIFYING...' : overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh' ? 'DEPLOY MORE' : 'VERIFY NOW'}
                    </button>
                  )}
                  {runtime.activeNodes === 0 && (
                    <button
                      type="button"
                      className={`action-link${deployState.state === 'capacity' ? ' muted' : ''}`}
                      onClick={() => navigate('/instances')}
                    >
                      OPEN CLUSTERS
                    </button>
                  )}
                  {runtime.degradedNodes > 0 && (
                    <button
                      type="button"
                      className="action-link danger"
                      onClick={() => navigate(degradedTarget)}
                    >
                      OPEN DEGRADED NODES
                    </button>
                  )}
                  {hasVaultEntry && !isLoaded && !isDeploying && (
                    <button
                      type="button"
                      className="action-link danger"
                      onClick={() => handleRemove(model.id)}
                    >
                      REMOVE
                    </button>
                  )}
                </div>
              </div>
              );
            })
            )}
          </div>
        </div>
      </div>
      )}

      {/* Footer */}
      <div className="grid-row" style={{ backgroundColor: 'var(--bg-accent)' }}>
        <div className="cell">
          <div className="label-text">REGISTRY MODELS</div>
          <div className="mono" style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {displayModels.length}
          </div>
        </div>
        <div className="cell">
          <div className="label-text">ACTIVE</div>
          <div className="mono" style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {activeCount}
          </div>
        </div>
        <div className="cell">
          <div className="label-text">READY TO DEPLOY</div>
          <div className="mono" style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {readyCount}
          </div>
        </div>
        <div className="cell">
          <div className="label-text">SERVING VERIFIED</div>
          <div className="mono" style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {servingVerifiedCount}
          </div>
        </div>
        <div className="cell">
          <div className="label-text">DEPLOYMENT SIGNAL</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.85rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span className={`status-dot ${visibleProviders.some((provider) => provider.connected) ? '' : 'inactive'}`} />
            {visibleProviders.some((provider) => provider.connected)
              ? `${visibleProviders.filter((provider) => provider.connected).length} provider${visibleProviders.filter((provider) => provider.connected).length === 1 ? '' : 's'} live.`
              : 'No live provider is currently connected for deployments.'}
          </div>
        </div>
      </div>

      <RegisterModelModal isOpen={showRegisterModal} onClose={() => setShowRegisterModal(false)} />
    </div>
  );
}
