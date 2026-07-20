import { useEffect, useCallback, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import type { Model, Worker } from '../types';
import { sendChatCompletion } from '../lib/chatClient';
import { publicAnalytics } from '../lib/publicAnalytics';
import { LabelText, Badge, ActionButton } from '../components/shared';
import { ModelsSkeleton } from '../components/skeletons';
import { ModelCatalogSection } from '../components/models/ModelCatalogSection';
import { ModelsOverviewSection } from '../components/models/ModelsOverviewSection';
import { deriveModelRuntimeDrilldown } from '../lib/modelRuntimeDrilldown';
import { useIsMobile } from '../hooks/useIsMobile';
import { useDebouncedValue } from '../hooks/useDebouncedValue';
import { type ModelDeployReadiness, type ModelServingOverview, useModelsViewState } from '../hooks/useModelsViewState';
import { useAuthSession } from '../lib/auth-context';
import { useDeploymentAttempts, useUpdateDeploymentVerification } from '../hooks/useDeploymentApi';
import { useInstances, useOfferings, useProviders } from '../hooks/useInfrastructureApi';
import { useModels, useWorkers } from '../hooks/useRuntimeApi';
import { useDeleteVaultModel, useRegisterVaultModel, useVaultModels } from '../hooks/useVaultApi';

const FAMILY_OPTIONS = ['mistral', 'llama', 'qwen', 'phi', 'gemma', 'deepseek', 'falcon', 'mixtral', 'yi', 'command-r'];
const QUANT_OPTIONS = ['none', 'GPTQ', 'AWQ', 'GGUF', 'FP8', 'INT8', 'INT4'];
const RECOMMENDED_MODEL_LABELS: Record<string, string> = {
  'Qwen/Qwen3-4B-Thinking-2507': 'Budget Reasoning',
  'moonshotai/Kimi-K2.5-Instruct': 'High-Capacity',
};

/* ------------------------------------------------------------------ */
/*  Model slide-over panel                                             */
/* ------------------------------------------------------------------ */

type SlideOverModel = {
  model: Model;
  overview: ModelServingOverview;
  runtime: ReturnType<typeof deriveModelRuntimeDrilldown>;
  deployState: ModelDeployReadiness;
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

  const {
    allTags,
    filtered,
    getDeployState,
    getOverview,
    getRuntime,
    readyCount,
    activeCount,
    servingVerifiedCount,
    recommendedModels,
    connectedProviderCount,
  } = useModelsViewState({
    displayModels,
    offerings,
    providers,
    liveInstances,
    workers,
    deploymentAttempts,
    deferredQuery,
    activeTagFilter,
  });

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
    const overview = getOverview(model.id);
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

      publicAnalytics.trackFirst('activation_first_unary_inference_succeeded', { surface: 'model_catalog' });

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
      <ModelsOverviewSection
        searchQuery={searchQuery}
        onSearchQueryChange={setSearchQuery}
        onClearSearch={() => setSearchQuery('')}
        displayModelCount={displayModels.length}
        activeCount={activeCount}
        readyCount={readyCount}
        servingVerifiedCount={servingVerifiedCount}
        showFilteredCount={Boolean(deferredQuery || activeTagFilter)}
        isSearchStale={isSearchStale}
        filteredCount={filtered.length}
        allTags={allTags}
        activeTagFilter={activeTagFilter}
        onToggleTagFilter={(tag) => setActiveTagFilter(activeTagFilter === tag ? null : tag)}
        onClearTagFilter={() => setActiveTagFilter(null)}
        onOpenRegister={() => setShowRegisterModal(true)}
        onOpenNodes={() => navigate('/instances')}
        onOpenDocs={() => navigate('/docs')}
        recommendedModels={recommendedModels}
        getDeployState={getDeployState}
        getOverview={getOverview}
        recommendedLabels={RECOMMENDED_MODEL_LABELS}
        onNavigate={navigate}
        onFilterToModel={setSearchQuery}
        connectedProviderCount={connectedProviderCount}
      />

      <ModelCatalogSection
        isMobile={isMobile}
        filtered={filtered}
        searchQuery={searchQuery}
        deferredQuery={deferredQuery}
        activeTagFilter={activeTagFilter}
        onClearSearch={() => setSearchQuery('')}
        onClearTagFilter={() => setActiveTagFilter(null)}
        onOpenRegister={() => setShowRegisterModal(true)}
        onOpenNodes={() => navigate('/instances')}
        onOpenQuickstart={() => navigate('/getting-started')}
        getOverview={getOverview}
        getRuntime={getRuntime}
        getDeployState={getDeployState}
        verifyingModelID={verifyingModelID}
        onOpenSlideOver={handleOpenSlideOver}
        onNavigate={navigate}
        onVerifyServing={handleVerifyServing}
        hasVaultEntry={(modelId) => vaultIdByUri.has(modelId)}
        onRemove={handleRemove}
      />

      <RegisterModelModal isOpen={showRegisterModal} onClose={() => setShowRegisterModal(false)} />

      {slideOverModelId && (() => {
        const model = displayModels.find((m) => m.id === slideOverModelId);
        if (!model) return null;
        const overview = getOverview(model.id);
        if (!overview) return null;
        const runtime = getRuntime(model.id);
        const deployState = getDeployState(model);
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
