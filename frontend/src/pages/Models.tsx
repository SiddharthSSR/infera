import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import { useModels, useVaultModels, useRegisterVaultModel, useDeleteVaultModel } from '../hooks/useApi';
import { useIsMobile } from '../hooks/useIsMobile';

const FAMILY_OPTIONS = ['mistral', 'llama', 'qwen', 'phi', 'gemma', 'deepseek', 'falcon', 'mixtral', 'yi', 'command-r'];
const QUANT_OPTIONS = ['none', 'GPTQ', 'AWQ', 'GGUF', 'FP8', 'INT8', 'INT4'];

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
  const { data: models } = useModels();
  const { data: vaultData } = useVaultModels({});
  const deleteMutation = useDeleteVaultModel();
  const [searchQuery, setSearchQuery] = useState('');
  const [showRegisterModal, setShowRegisterModal] = useState(false);

  const allModels = models || [];
  const vaultModels = vaultData?.models || [];

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
    max_context: vm.max_context,
    tags: vm.tags,
    vault_status: vm.status,
  }));

  const filtered = displayModels.filter(m => {
    if (!searchQuery) return true;
    const q = searchQuery.toLowerCase();
    return m.id.toLowerCase().includes(q) || m.family?.toLowerCase().includes(q) || m.owned_by?.toLowerCase().includes(q);
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

      {isMobile ? (
        <div className="mobile-data-list" style={{ padding: '1rem' }}>
          {filtered.length === 0 ? (
            <div style={{ padding: '2rem 0', textAlign: 'center', color: 'var(--text-secondary)' }}>
              {searchQuery ? 'No models match your search.' : 'No models in registry. Add one to get started.'}
            </div>
          ) : (
            filtered.map(model => {
              const isLoaded = model.loaded !== false;
              const isDeploying = model.vault_status === 'testing';
              const statusDotClass = isLoaded ? '' : isDeploying ? 'warning' : 'inactive';
              const statusLabel = isLoaded ? 'Active' : isDeploying ? 'Deploying...' : 'Available';
              const shortName = model.id.split('/').pop() || model.id;
              const provider = model.owned_by || model.family || '';
              const hasVaultEntry = vaultIdByUri.has(model.id);

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
                  </div>

                  <div className="mobile-data-actions">
                    {isLoaded ? (
                      <span
                        className="mobile-data-action"
                        onClick={() => navigate('/instances')}
                      >
                        MANAGE
                      </span>
                    ) : isDeploying ? (
                      <span
                        className="mobile-data-action muted"
                        onClick={() => toast.info('Cancellation coming soon')}
                      >
                        CANCEL
                      </span>
                    ) : (
                      <>
                        <span
                          className="mobile-data-action"
                          onClick={() => navigate(`/instances?provision=true&model=${encodeURIComponent(model.id)}`)}
                        >
                          DEPLOY
                        </span>
                        {hasVaultEntry && (
                          <span
                            className="mobile-data-action danger"
                            onClick={() => handleRemove(model.id)}
                          >
                            REMOVE
                          </span>
                        )}
                      </>
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
              </div>
            ) : (
              filtered.map(model => {
            const isLoaded = model.loaded !== false;
            const isDeploying = model.vault_status === 'testing';
            const statusDotClass = isLoaded ? '' : isDeploying ? 'warning' : 'inactive';
            const statusLabel = isLoaded ? 'Active' : isDeploying ? 'Deploying...' : 'Available';
            const shortName = model.id.split('/').pop() || model.id;
            const provider = model.owned_by || model.family || '';
            const hasVaultEntry = vaultIdByUri.has(model.id);

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
                </div>
                <div>
                  <span className="badge">{model.quantization || 'FP16'}</span>
                </div>
                <div className="mono" style={{ color: 'var(--text-secondary)' }}>
                  {model.max_context ? model.max_context.toLocaleString() : 'N/A'}
                </div>
                <div style={{ display: 'flex', gap: '0.75rem' }}>
                  {isLoaded ? (
                    <button
                      type="button"
                      className="action-link"
                      onClick={() => navigate('/instances')}
                    >
                      MANAGE
                    </button>
                  ) : isDeploying ? (
                    <button
                      type="button"
                      className="action-link muted"
                      onClick={() => toast.info('Cancellation coming soon')}
                    >
                      CANCEL
                    </button>
                  ) : (
                    <>
                      <button
                        type="button"
                        className="action-link"
                        onClick={() => navigate(`/instances?provision=true&model=${encodeURIComponent(model.id)}`)}
                      >
                        DEPLOY
                      </button>
                      {hasVaultEntry && (
                        <button
                          type="button"
                          className="action-link danger"
                          onClick={() => handleRemove(model.id)}
                        >
                          REMOVE
                        </button>
                      )}
                    </>
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
          <div className="label-text">LOADED</div>
          <div className="mono" style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {displayModels.filter(m => m.loaded !== false).length}
          </div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text">SYSTEM STATUS</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.85rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span className="status-dot" />
            All model endpoints are performing within latency targets.
          </div>
        </div>
      </div>

      <RegisterModelModal isOpen={showRegisterModal} onClose={() => setShowRegisterModal(false)} />
    </div>
  );
}
