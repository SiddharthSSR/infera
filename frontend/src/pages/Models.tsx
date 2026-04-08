import { useEffect, useCallback, useMemo, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import type { GPUOffering, Model, ProviderStatus, Instance, Worker } from '../types';
import { sendChatCompletion } from '../lib/api';
import { GridRow, Cell, LabelText, Badge, ActionButton } from '../components/shared';
import { ModelsSkeleton } from '../components/skeletons';
import { ActionGroup } from '../components/ActionGroup';
import { CollapsibleSection } from '../components/CollapsibleSection';
import { MetadataList } from '../components/MetadataList';
import { SectionHeader } from '../components/SectionHeader';
import {
  summarizeDeploymentAttempt,
  type DeploymentAttemptRecord,
  type DeploymentAttemptSummary,
} from '../lib/deploymentHistory';
import { getInstanceReadiness } from '../lib/instanceReadiness';
import { deriveModelRuntimeDrilldown } from '../lib/modelRuntimeDrilldown';
import { useDeploymentAttempts, useModels, useVaultModels, useRegisterVaultModel, useDeleteVaultModel, useOfferings, useProviders, useInstances, useUpdateDeploymentVerification, useWorkers } from '../hooks/useApi';
import { useIsMobile } from '../hooks/useIsMobile';
import { useDebouncedValue } from '../hooks/useDebouncedValue';
import { useAuthSession } from '../lib/auth-context';
import { getProviderDisplayName, isInventoryProviderType } from '../lib/providerInventory';
import { formatVerificationMeta } from '../lib/formatting';
import { verificationToneClass } from '../lib/labels';

const FAMILY_OPTIONS = ['mistral', 'llama', 'qwen', 'phi', 'gemma', 'deepseek', 'falcon', 'mixtral', 'yi', 'command-r'];
const QUANT_OPTIONS = ['none', 'GPTQ', 'AWQ', 'GGUF', 'FP8', 'INT8', 'INT4'];
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

function HighlightMatch({ text, query }: { text: string; query: string }): ReactNode {
  if (!query) return text;
  const lowerText = text.toLowerCase();
  const lowerQuery = query.toLowerCase();
  const idx = lowerText.indexOf(lowerQuery);
  if (idx === -1) return text;
  return (
    <>
      {text.slice(0, idx)}
      <mark style={{ background: 'var(--bg-accent)', color: 'inherit', padding: '1px 2px', borderRadius: 2 }}>
        {text.slice(idx, idx + query.length)}
      </mark>
      {text.slice(idx + query.length)}
    </>
  );
}

/* ------------------------------------------------------------------ */
/*  Model slide-over panel                                             */
/* ------------------------------------------------------------------ */

type SlideOverModel = {
  model: Model;
  overview: ModelServingOverview;
  runtime: ReturnType<typeof deriveModelRuntimeDrilldown>;
  deployState: ReturnType<typeof describeDeployReadiness>;
};

function ModelSlideOver({
  data,
  workers,
  onClose,
  onDelete,
  onVerify,
  verifying,
}: {
  data: SlideOverModel;
  workers: Worker[] | undefined;
  onClose: () => void;
  onDelete: (modelId: string) => void;
  onVerify: (model: Model) => void;
  verifying: boolean;
}) {
  const navigate = useNavigate();
  const [confirmDelete, setConfirmDelete] = useState(false);
  const { model, overview, runtime, deployState } = data;

  const shortName = model.id.split('/').pop() || model.id;
  const provider = model.owned_by || model.family || '';
  const isLoaded = model.loaded !== false;
  const isDeploying = model.vault_status === 'testing';
  const statusLabel = isLoaded ? 'Active' : isDeploying ? 'Deploying' : 'Available';

  // Serving workers for this model
  const modelWorkers = (workers || []).filter(
    (w) => w.models?.some((m) => m === model.id || m.endsWith(`/${shortName}`)),
  );

  // Close on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [onClose]);

  const metaRows: { label: string; value: string; mono?: boolean }[] = [
    { label: 'ID', value: model.id, mono: true },
    { label: 'PROVIDER', value: provider || 'Unknown' },
    { label: 'FAMILY', value: model.family || '-' },
    { label: 'PARAMETERS', value: model.parameters || '-' },
    { label: 'QUANTIZATION', value: model.quantization || 'FP16' },
    { label: 'CONTEXT', value: model.max_context ? `${(model.max_context / 1000).toFixed(0)}k tokens` : 'N/A', mono: true },
    { label: 'VRAM REQUIRED', value: model.vram_required ? `${Math.ceil(model.vram_required / 1024)} GB` : 'Not specified', mono: true },
    { label: 'STATUS', value: statusLabel },
  ];

  const deployRows: { label: string; value: string; mono?: boolean }[] = [
    { label: 'ACTIVE NODES', value: String(runtime.activeNodes), mono: true },
    { label: 'DEGRADED NODES', value: String(runtime.degradedNodes), mono: true },
    { label: 'SERVING STATE', value: overview.badgeLabel },
    { label: 'DEPLOY READINESS', value: deployState.summary },
    { label: 'LATEST ISSUE', value: runtime.latestIssue || 'None' },
  ];

  const verificationRows: { label: string; value: string }[] = [
    { label: 'VERIFICATION', value: runtime.verificationLabel },
    { label: 'LAST VERIFIED', value: overview.verifiedAt ? new Date(overview.verifiedAt).toLocaleString() : 'Never' },
    { label: 'LATENCY', value: overview.latestVerificationLatencyMs != null ? `${overview.latestVerificationLatencyMs}ms` : '-' },
    { label: 'LAST ERROR', value: overview.latestVerificationError || 'None' },
  ];

  return (
    <>
      {/* Backdrop */}
      <div
        className="slideover-backdrop"
        onClick={onClose}
        aria-hidden="true"
      />
      {/* Panel */}
      <aside
        className="slideover-panel"
        role="dialog"
        aria-label={`Manage ${shortName}`}
      >
        {/* Header */}
        <div className="slideover-header">
          <div>
            <LabelText as="div">MODEL DETAILS</LabelText>
            <div style={{ fontSize: '1.15rem', fontWeight: 600, marginTop: '0.4rem', lineHeight: 1.2 }}>
              {shortName}
            </div>
            <div className="chip-row" style={{ marginTop: '0.5rem' }}>
              <Badge tone={overview.badgeTone || undefined}>{overview.badgeLabel}</Badge>
              {model.tags?.map((tag) => <Badge key={tag}>{tag}</Badge>)}
            </div>
          </div>
          <button
            type="button"
            className="slideover-close"
            onClick={onClose}
            aria-label="Close panel"
          >
            ×
          </button>
        </div>

        {/* Scrollable body */}
        <div className="slideover-body">
          {/* Model metadata */}
          <section className="slideover-section">
            <LabelText as="div" style={{ marginBottom: '0.75rem' }}>REGISTRY METADATA</LabelText>
            {metaRows.map((row) => (
              <div key={row.label} className="slideover-row">
                <span className="slideover-row-label">{row.label}</span>
                <span className={row.mono ? 'mono' : ''} style={{ fontSize: '0.85rem' }}>{row.value}</span>
              </div>
            ))}
          </section>

          {/* Deployment / resource usage */}
          <section className="slideover-section">
            <LabelText as="div" style={{ marginBottom: '0.75rem' }}>RESOURCE USAGE</LabelText>
            {deployRows.map((row) => (
              <div key={row.label} className="slideover-row">
                <span className="slideover-row-label">{row.label}</span>
                <span className={row.mono ? 'mono' : ''} style={{ fontSize: '0.85rem' }}>{row.value}</span>
              </div>
            ))}
            {modelWorkers.length > 0 && (
              <div style={{ marginTop: '0.75rem' }}>
                <LabelText as="div" style={{ marginBottom: '0.5rem' }}>SERVING WORKERS</LabelText>
                {modelWorkers.slice(0, 4).map((w) => (
                  <div key={w.worker_id} className="slideover-row" style={{ fontFamily: 'var(--font-mono)', fontSize: '0.78rem' }}>
                    <span>{w.worker_id.slice(0, 12)}</span>
                    <span>GPU {w.gpu_utilization}% · {((w.memory_used / (w.memory_total || 1)) * 100).toFixed(0)}% mem</span>
                  </div>
                ))}
              </div>
            )}
          </section>

          {/* Verification history */}
          <section className="slideover-section">
            <LabelText as="div" style={{ marginBottom: '0.75rem' }}>VERIFICATION HISTORY</LabelText>
            {verificationRows.map((row) => (
              <div key={row.label} className="slideover-row">
                <span className="slideover-row-label">{row.label}</span>
                <span style={{ fontSize: '0.85rem' }}>{row.value}</span>
              </div>
            ))}
          </section>

          {/* Actions */}
          <section className="slideover-section slideover-actions">
            <ActionButton onClick={() => navigate(`/instances?model=${encodeURIComponent(model.id)}`)}>
              VIEW DEPLOYMENTS
            </ActionButton>
            {isLoaded && (
              <ActionButton
                disabled={verifying}
                onClick={() => onVerify(model)}
              >
                {verifying ? 'VERIFYING...' : 'VERIFY SERVING'}
              </ActionButton>
            )}
            <ActionButton onClick={() => navigate(`/instances?provision=true&model=${encodeURIComponent(model.id)}&from=models`)}>
              DEPLOY MORE
            </ActionButton>
          </section>

          {/* Delete */}
          <section className="slideover-section slideover-danger-zone">
            <LabelText as="div" style={{ marginBottom: '0.5rem' }}>DANGER ZONE</LabelText>
            {!confirmDelete ? (
              <ActionButton variant="destructive" onClick={() => setConfirmDelete(true)}>
                DELETE MODEL
              </ActionButton>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                <div style={{ fontSize: '0.85rem', color: 'var(--color-error)', lineHeight: 1.5 }}>
                  This will remove <strong>{shortName}</strong> from the registry. Active deployments will not be terminated but the model will no longer appear in the catalog.
                </div>
                <div style={{ display: 'flex', gap: '0.5rem' }}>
                  <ActionButton variant="destructive" onClick={() => { onDelete(model.id); onClose(); }}>
                    CONFIRM DELETE
                  </ActionButton>
                  <ActionButton onClick={() => setConfirmDelete(false)}>CANCEL</ActionButton>
                </div>
              </div>
            )}
          </section>
        </div>
      </aside>
    </>
  );
}

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
      actionLabel: 'VIEW NODES',
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
    actionTarget: `/instances?provision=true&model=${encodeURIComponent(model.id)}&from=models`,
  };
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
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to register model';
      toast.error(message);
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
          <LabelText as="div" style={{ marginBottom: '0.5rem' }}>REGISTER MODEL</LabelText>
          <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
            Add a HuggingFace model to the registry
          </div>
        </div>

        {/* Form */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '1.5rem 2rem' }}>
          {/* Source URI - primary field */}
          <div style={{ marginBottom: '1.75rem' }}>
            <LabelText as="div" style={{ marginBottom: '0.5rem' }}>HUGGINGFACE MODEL ID *</LabelText>
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
            <LabelText as="div" style={{ marginBottom: '0.5rem' }}>DISPLAY NAME *</LabelText>
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
              <LabelText as="div" style={{ marginBottom: '0.5rem' }}>FAMILY</LabelText>
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
              <LabelText as="div" style={{ marginBottom: '0.5rem' }}>PARAMETERS</LabelText>
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
              <LabelText as="div" style={{ marginBottom: '0.5rem' }}>QUANTIZATION</LabelText>
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
              <LabelText as="div" style={{ marginBottom: '0.5rem' }}>MAX CONTEXT</LabelText>
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
            <LabelText as="div" style={{ marginBottom: '0.5rem' }}>VRAM REQUIRED (MB)</LabelText>
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
            <LabelText as="div" style={{ marginBottom: '0.5rem' }}>TAGS</LabelText>
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
          <ActionButton onClick={onClose}>CANCEL</ActionButton>
          <ActionButton
            variant="primary"
            onClick={handleSubmit}
            disabled={registerMutation.isPending || !form.source_uri.trim() || !form.name.trim()}
          >
            {registerMutation.isPending ? 'REGISTERING...' : 'REGISTER MODEL'}
          </ActionButton>
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
  const { data: models, isLoading: modelsLoading } = useModels();
  const { data: vaultData } = useVaultModels({});
  const { data: offerings } = useOfferings();
  const { data: providers } = useProviders();
  const { data: instances } = useInstances();
  const { data: workers } = useWorkers(workspaceID);
  const deleteMutation = useDeleteVaultModel();
  const [searchQuery, setSearchQuery] = useState('');
  const deferredQuery = useDebouncedValue(searchQuery, 200);
  const isSearchStale = searchQuery !== deferredQuery;
  const [activeTagFilter, setActiveTagFilter] = useState<string | null>(null);
  const [showRegisterModal, setShowRegisterModal] = useState(false);
  const [verifyingModelID, setVerifyingModelID] = useState<string | null>(null);
  const [slideOverModelId, setSlideOverModelId] = useState<string | null>(null);

  const handleOpenSlideOver = useCallback((modelId: string) => {
    setSlideOverModelId(modelId);
  }, []);

  const handleCloseSlideOver = useCallback(() => {
    setSlideOverModelId(null);
  }, []);
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

  const allTags = useMemo(() => {
    const tagSet = new Set<string>();
    for (const model of displayModels) {
      for (const tag of model.tags || []) tagSet.add(tag);
    }
    return [...tagSet].sort();
  }, [displayModels]);

  const filtered = useMemo(() => displayModels.filter(m => {
    if (activeTagFilter && !(m.tags || []).includes(activeTagFilter)) return false;
    if (!deferredQuery) return true;
    const q = deferredQuery.toLowerCase();
    return m.id.toLowerCase().includes(q)
      || m.family?.toLowerCase().includes(q)
      || m.owned_by?.toLowerCase().includes(q)
      || (m.quantization || '').toLowerCase().includes(q)
      || (m.tags || []).some(t => t.toLowerCase().includes(q));
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

  if (modelsLoading) return <ModelsSkeleton />;

  return (
    <div className="models-page animate-fade-in">
      <div className="models-toolbar">
        <div className="models-toolbar-copy">
          <label className="models-search-shell">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>
            <input
              type="text"
              className="models-search-input"
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              placeholder="Filter by name, provider, quant, tag..."
            />
            {searchQuery && (
              <button
                type="button"
                onClick={() => setSearchQuery('')}
                style={{
                  background: 'none',
                  border: 'none',
                  cursor: 'pointer',
                  padding: '0.25rem',
                  color: 'var(--text-secondary)',
                  fontSize: '1rem',
                  lineHeight: 1,
                  display: 'flex',
                  alignItems: 'center',
                }}
                aria-label="Clear search"
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
                </svg>
              </button>
            )}
          </label>
          <div className="chip-row models-summary-strip" style={{ flexWrap: 'wrap' }}>
            <Badge>{displayModels.length} REGISTRY MODELS</Badge>
            <Badge>{activeCount} ACTIVE</Badge>
            <Badge>{readyCount} READY TO DEPLOY</Badge>
            <Badge>{servingVerifiedCount} SERVING VERIFIED</Badge>
            {(deferredQuery || activeTagFilter) && (
              <Badge style={{ opacity: isSearchStale ? 0.5 : 1, transition: 'opacity 0.15s' }}>
                SHOWING {filtered.length} OF {displayModels.length}
              </Badge>
            )}
          </div>
          {allTags.length > 0 && (
            <div className="chip-row" style={{ flexWrap: 'wrap', gap: '0.35rem' }}>
              {allTags.map(tag => (
                <button
                  key={tag}
                  type="button"
                  className="tag"
                  onClick={() => setActiveTagFilter(activeTagFilter === tag ? null : tag)}
                  style={{
                    cursor: 'pointer',
                    background: activeTagFilter === tag ? 'var(--text-primary)' : undefined,
                    color: activeTagFilter === tag ? 'var(--bg-paper)' : undefined,
                    borderColor: activeTagFilter === tag ? 'var(--text-primary)' : undefined,
                    transition: 'all 0.15s ease',
                  }}
                >
                  {tag}
                </button>
              ))}
              {activeTagFilter && (
                <button
                  type="button"
                  className="tag"
                  onClick={() => setActiveTagFilter(null)}
                  style={{ cursor: 'pointer', borderStyle: 'dashed', opacity: 0.6 }}
                >
                  CLEAR
                </button>
              )}
            </div>
          )}
        </div>
        <ActionGroup compact>
          <ActionButton variant="primary" onClick={() => setShowRegisterModal(true)}>
            REGISTER MODEL
          </ActionButton>
        </ActionGroup>
      </div>

      <GridRow>
        <Cell span={4}>
          <div className="help-callout" style={{ padding: '1rem 1.25rem' }}>
            <SectionHeader
              eyebrow="MODEL STATUS GUIDE"
              title="Registry first, runtime detail on demand"
              description={(
                <>
                  <strong>Serving verified</strong> means a live inference check passed for this model. <strong>Fresh verify</strong> means that proof is recent enough to trust immediately. <strong>Stale verify</strong> means the model was verified before, but the proof is old. Use <strong>View deployments</strong> or <strong>Open degraded nodes</strong> for node-level recovery and <strong>Verify now</strong> when you want a new explicit inference check from the registry.
                </>
              )}
              actions={(
                <ActionGroup compact>
                  <ActionButton onClick={() => navigate('/instances')}>OPEN NODES</ActionButton>
                  <ActionButton onClick={() => navigate('/docs')}>READ API DOCS</ActionButton>
                </ActionGroup>
              )}
            />
          </div>
        </Cell>
      </GridRow>

      {recommendedModels.length > 0 && (
        <GridRow>
          <Cell span={4}>
            <div className="help-callout" style={{ padding: '1rem 1.25rem' }}>
              <SectionHeader
                eyebrow="RECOMMENDED NOW"
                title="Fast-start picks"
                description="Curated picks from the expanded catalog. Qwen is the lighter reasoning option to trial quickly; Kimi is the high-capacity frontier candidate and will usually need larger infrastructure than the current default fleet."
              />
              <div className="panel-grid columns-2" style={{ marginTop: '1rem' }}>
                {recommendedModels.map((model) => {
                  const deployState = describeDeployReadiness(model, visibleOfferings, visibleProviders);
                  const overview = modelOverviewByID.get(model.id);
                  return (
                    <div key={model.id} className="overview-card accent">
                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '0.75rem', alignItems: 'flex-start' }}>
                        <div>
                          <div style={{ fontSize: '1rem', fontWeight: 600 }}>{model.id.split('/').pop() || model.id}</div>
                          <div className="mono" style={{ marginTop: '0.3rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
                            {model.parameters || 'N/A'} · {model.family || model.owned_by || 'model'}
                          </div>
                        </div>
                        <div style={{ display: 'flex', gap: '0.35rem', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                          {RECOMMENDED_MODEL_LABELS[model.id] ? <Badge>{RECOMMENDED_MODEL_LABELS[model.id]}</Badge> : null}
                          {overview ? <Badge tone={overview.badgeTone || undefined}>{overview.badgeLabel}</Badge> : null}
                        </div>
                      </div>
                      <div style={{ marginTop: '0.8rem', color: 'var(--text-secondary)', fontSize: '0.82rem', lineHeight: 1.6 }}>
                        {deployState.summary}
                      </div>
                      <div className="action-group compact" style={{ marginTop: '0.9rem' }}>
                        <ActionButton onClick={() => navigate(deployState.actionTarget)}>
                          {deployState.actionLabel}
                        </ActionButton>
                        <ActionButton onClick={() => setSearchQuery(model.id.split('/').pop() || model.id)}>
                          FILTER
                        </ActionButton>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          </Cell>
        </GridRow>
      )}

      {isMobile ? (
        <div className="mobile-data-list models-list-section">
          {filtered.length === 0 ? (
            <div style={{ padding: '3rem 1rem', textAlign: 'center', color: 'var(--text-secondary)' }}>
              {searchQuery ? (
                <>
                  <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" style={{ opacity: 0.4, marginBottom: '0.75rem' }}>
                    <circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" />
                  </svg>
                  <div style={{ fontSize: '0.95rem', fontWeight: 500, color: 'var(--text-primary)', marginBottom: '0.35rem' }}>
                    No models match &ldquo;{searchQuery}&rdquo;
                  </div>
                  <div style={{ fontSize: '0.85rem', lineHeight: 1.6, maxWidth: 360, margin: '0 auto 1rem' }}>
                    Try a different name, provider, quantization, or tag. Filters are combined with the active tag if one is selected.
                  </div>
                  <div className="help-actions" style={{ justifyContent: 'center' }}>
                    <ActionButton onClick={() => setSearchQuery('')}>CLEAR SEARCH</ActionButton>
                    {activeTagFilter && <ActionButton onClick={() => setActiveTagFilter(null)}>CLEAR TAG FILTER</ActionButton>}
                  </div>
                </>
              ) : (
                <>
                  No models in registry. Add one to get started.
                  <div className="help-actions" style={{ justifyContent: 'center' }}>
                    <ActionButton onClick={() => setShowRegisterModal(true)}>ADD MODEL</ActionButton>
                    <ActionButton onClick={() => navigate('/instances')}>OPEN NODES</ActionButton>
                    <ActionButton onClick={() => navigate('/getting-started')}>OPEN QUICKSTART</ActionButton>
                  </div>
                </>
              )}
            </div>
          ) : (
            filtered.map((model, rowIndex) => {
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
                  className="mobile-data-card"
                  style={{
                    animation: `fade-slide-in 0.3s ease both`,
                    animationDelay: `${Math.min(rowIndex * 40, 400)}ms`,
                  }}
                >
                  <div className="mobile-data-card-header">
                    <div>
                      <div className="mobile-data-title">
                        <HighlightMatch text={shortName} query={deferredQuery} />
                      </div>
                      <div className="mobile-data-subtitle mono">
                        {model.parameters && `${model.parameters} — `}<HighlightMatch text={provider} query={deferredQuery} />
                      </div>
                      <div className="mobile-card-chip-row">
                        <span className="mobile-status-inline">
                          <span className={`status-dot ${statusDotClass}`} />
                          {statusLabel}
                          {isDeploying && <span className="deploy-spinner" />}
                        </span>
                        <Badge tone={overview.badgeTone || undefined}>{overview.badgeLabel}</Badge>
                      </div>
                    </div>
                    <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                      <Badge mono>{runtime.activeNodes} node{runtime.activeNodes === 1 ? '' : 's'}</Badge>
                    </div>
                  </div>

                  <div className="mobile-data-meta">
                    <div><LabelText>DEPLOYMENTS</LabelText> <span className="mono">{runtime.activeNodes}</span></div>
                    <div><LabelText>VERIFY</LabelText> <Badge className={verificationToneClass(runtime.verificationFreshness)}>{runtime.verificationLabel}</Badge></div>
                    {runtime.degradedNodes > 0 && (
                      <div><LabelText>DEGRADED</LabelText> <span className="mono">{runtime.degradedNodes} node{runtime.degradedNodes === 1 ? '' : 's'}</span></div>
                    )}
                    <div><LabelText>DEPLOY</LabelText> <span>{deployState.summary}</span></div>
                    <div><LabelText>STATUS</LabelText> <span>{overview.summary}</span></div>
                  </div>

                  <div className="mobile-data-actions">
                    {runtime.activeNodes > 0 && (
                      <button
                        type="button"
                        className="mobile-data-action"
                        onClick={() => handleOpenSlideOver(model.id)}
                      >
                        MANAGE
                      </button>
                    )}
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
                          ? navigate(`/instances?provision=true&model=${encodeURIComponent(model.id)}&from=models`)
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

                  <div style={{ marginTop: '1rem' }}>
                    <CollapsibleSection
                      title="SHOW DETAILS"
                      description="Secondary runtime, verification, and registry metadata."
                    >
                      <MetadataList
                        items={[
                          { label: 'QUANT', value: model.quantization || 'FP16' },
                          { label: 'CONTEXT', value: model.max_context ? model.max_context.toLocaleString() : 'N/A', mono: true },
                          { label: 'LAST VERIFY', value: formatVerificationMeta(overview.verifiedAt, overview.latestVerificationLatencyMs) || 'Never' },
                          { label: 'SERVING', value: overview.summary },
                          { label: 'DEPLOY', value: deployState.summary },
                          { label: 'LATEST ISSUE', value: runtime.latestIssue || 'None' },
                        ]}
                        columns={1}
                      />
                    </CollapsibleSection>
                  </div>
                </div>
              );
            })
          )}
        </div>
      ) : (
        <div className="models-list-section">
          <div className="stack-list">
            {filtered.length === 0 ? (
              <div className="stack-item" style={{ textAlign: 'center', color: 'var(--text-secondary)', padding: '3rem 2rem' }}>
                {searchQuery ? (
                  <>
                    <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" style={{ opacity: 0.4, marginBottom: '0.75rem' }}>
                      <circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" />
                    </svg>
                    <div style={{ fontSize: '0.95rem', fontWeight: 500, color: 'var(--text-primary)', marginBottom: '0.35rem' }}>
                      No models match &ldquo;{searchQuery}&rdquo;
                    </div>
                    <div style={{ fontSize: '0.85rem', lineHeight: 1.6, maxWidth: 420, margin: '0 auto 1rem' }}>
                      Try a different name, provider, quantization, or tag. Filters are combined with the active tag if one is selected.
                    </div>
                    <div className="help-actions" style={{ justifyContent: 'center' }}>
                      <ActionButton onClick={() => setSearchQuery('')}>CLEAR SEARCH</ActionButton>
                      {activeTagFilter && <ActionButton onClick={() => setActiveTagFilter(null)}>CLEAR TAG FILTER</ActionButton>}
                    </div>
                  </>
                ) : (
                  <>
                    No models in registry. Add one to get started.
                    <div className="help-actions" style={{ justifyContent: 'center' }}>
                      <ActionButton onClick={() => setShowRegisterModal(true)}>ADD MODEL</ActionButton>
                      <ActionButton onClick={() => navigate('/instances')}>OPEN NODES</ActionButton>
                      <ActionButton onClick={() => navigate('/getting-started')}>OPEN QUICKSTART</ActionButton>
                    </div>
                  </>
                )}
              </div>
            ) : (
              filtered.map((model, rowIndex) => {
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
                    className="stack-item model-row-card"
                    data-testid="model-row-card"
                    style={{
                      animation: `fade-slide-in 0.3s ease both`,
                      animationDelay: `${Math.min(rowIndex * 40, 400)}ms`,
                    }}
                  >
                    <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', gap: '1rem', alignItems: 'start' }}>
                      <div>
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', flexWrap: 'wrap', alignItems: 'flex-start' }}>
                          <div>
                            <div style={{ fontSize: '1.15rem', fontWeight: 600 }}>
                              <HighlightMatch text={shortName} query={deferredQuery} />
                            </div>
                            <div className="mono" style={{ color: 'var(--text-secondary)', marginTop: '0.25rem' }}>
                              {model.parameters && `${model.parameters} — `}<HighlightMatch text={provider} query={deferredQuery} />
                            </div>
                          </div>
                          <div className="chip-row">
                            <Badge tone={overview.badgeTone || undefined}>{overview.badgeLabel}</Badge>
                            {runtime.activeNodes > 0 && <Badge>{runtime.activeNodes} DEPLOYMENT{runtime.activeNodes === 1 ? '' : 'S'}</Badge>}
                            <Badge className={verificationToneClass(runtime.verificationFreshness)}>{runtime.verificationLabel}</Badge>
                            {runtime.degradedNodes > 0 && <Badge tone="error">{runtime.degradedNodes} DEGRADED NODE{runtime.degradedNodes === 1 ? '' : 'S'}</Badge>}
                          </div>
                        </div>
                        <div style={{ marginTop: '0.6rem', display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem' }}>
                          <span className={`status-dot ${statusDotClass}`} />
                          {statusLabel}
                          {isDeploying && <span className="deploy-spinner" />}
                        </div>
                        <div style={{ marginTop: '0.6rem', fontSize: '0.88rem', color: 'var(--text-secondary)', lineHeight: 1.6 }}>
                          {overview.summary}
                        </div>
                        <div style={{ marginTop: '0.85rem' }}>
                          <MetadataList
                            items={[
                              { label: 'QUANTIZATION', value: model.quantization || 'FP16' },
                              { label: 'CONTEXT', value: model.max_context ? model.max_context.toLocaleString() : 'N/A', mono: true },
                              { label: 'DEPLOY', value: deployState.summary },
                              { label: 'SERVING', value: statusLabel },
                            ]}
                            columns={2}
                          />
                        </div>
                      </div>
                      <div style={{ display: 'grid', gap: '0.55rem', justifyItems: 'end' }}>
                        {runtime.activeNodes > 0 && (
                          <button
                            type="button"
                            className="action-link"
                            onClick={() => handleOpenSlideOver(model.id)}
                          >
                            MANAGE
                          </button>
                        )}
                        <button
                          type="button"
                          className="action-link"
                          onClick={() => navigate(runtime.activeNodes > 0 ? deploymentsTarget : deployState.actionTarget)}
                        >
                          {runtime.activeNodes > 0 ? 'VIEW DEPLOYMENTS' : deployState.actionLabel}
                        </button>
                        {runtime.activeNodes > 0 ? (
                          <button
                            type="button"
                            className={`action-link${overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh' ? ' muted' : ''}`}
                            disabled={verifyingModelID === model.id}
                            onClick={() => overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh'
                              ? navigate(`/instances?provision=true&model=${encodeURIComponent(model.id)}&from=models`)
                              : handleVerifyServing(model)}
                          >
                            {verifyingModelID === model.id ? 'VERIFYING...' : overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh' ? 'DEPLOY MORE' : 'VERIFY NOW'}
                          </button>
                        ) : (
                          <button
                            type="button"
                            className={`action-link${deployState.state === 'capacity' ? ' muted' : ''}`}
                            onClick={() => navigate('/instances')}
                          >
                            OPEN NODES
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

                    <div style={{ marginTop: '1rem' }}>
                      <CollapsibleSection
                        title="SHOW DETAILS"
                        description="Verification freshness, runtime issues, and extra registry metadata."
                      >
                        <MetadataList
                          items={[
                            { label: 'LAST VERIFY', value: formatVerificationMeta(overview.verifiedAt, overview.latestVerificationLatencyMs) || 'Never' },
                            { label: 'VRAM NEED', value: model.vram_required ? `${Math.ceil(model.vram_required / 1024)}GB` : 'Not specified' },
                            { label: 'LATEST ISSUE', value: runtime.latestIssue || 'None' },
                            { label: 'PROVIDER', value: provider || 'Unknown' },
                          ]}
                          columns={2}
                        />
                        {model.tags && model.tags.length > 0 ? (
                          <div className="chip-row" style={{ marginTop: '0.9rem' }}>
                            {model.tags.map((tag) => (
                              <span key={tag} className="tag">{tag}</span>
                            ))}
                          </div>
                        ) : null}
                      </CollapsibleSection>
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>
      )}

      <GridRow style={{ background: 'var(--bg-accent)' }}>
        <Cell>
          <LabelText as="div">REGISTRY MODELS</LabelText>
          <div className="mono" style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {displayModels.length}
          </div>
        </Cell>
        <Cell>
          <LabelText as="div">ACTIVE</LabelText>
          <div className="mono" style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {activeCount}
          </div>
        </Cell>
        <Cell>
          <LabelText as="div">READY TO DEPLOY</LabelText>
          <div className="mono" style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {readyCount}
          </div>
        </Cell>
        <Cell>
          <LabelText as="div">SERVING VERIFIED</LabelText>
          <div className="mono" style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {servingVerifiedCount}
          </div>
        </Cell>
        <Cell>
          <LabelText as="div">DEPLOYMENT SIGNAL</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.85rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span className={`status-dot ${visibleProviders.some((provider) => provider.connected) ? '' : 'inactive'}`} />
            {visibleProviders.some((provider) => provider.connected)
              ? `${visibleProviders.filter((provider) => provider.connected).length} provider${visibleProviders.filter((provider) => provider.connected).length === 1 ? '' : 's'} live.`
              : 'No live provider is currently connected for deployments.'}
          </div>
        </Cell>
      </GridRow>

      <RegisterModelModal isOpen={showRegisterModal} onClose={() => setShowRegisterModal(false)} />

      {slideOverModelId && (() => {
        const model = displayModels.find((m) => m.id === slideOverModelId);
        if (!model) return null;
        const overview = modelOverviewByID.get(model.id);
        if (!overview) return null;
        const runtime = modelRuntimeByID.get(model.id)!;
        const deployState = describeDeployReadiness(model, visibleOfferings, visibleProviders);
        return (
          <ModelSlideOver
            data={{ model, overview, runtime, deployState }}
            workers={workers}
            onClose={handleCloseSlideOver}
            onDelete={handleRemove}
            onVerify={handleVerifyServing}
            verifying={verifyingModelID === slideOverModelId}
          />
        );
      })()}
    </div>
  );
}
