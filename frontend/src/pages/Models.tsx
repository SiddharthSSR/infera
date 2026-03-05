import { useState } from 'react';
import {
  Database, Plus, Search, X, Trash2, Loader2,
  Cpu, MemoryStick, MessageSquare, Tag,
  Box, Beaker, Archive
} from 'lucide-react';
import { toast } from 'sonner';
import { cn } from '../lib/utils';
import type { VaultModel, VaultModelFilter, CreateVaultModelInput } from '../types';
import {
  useVaultModels, useVaultStats, useVaultFamilies,
  useRegisterVaultModel, useDeleteVaultModel,
} from '../hooks/useApi';

const familyColors: Record<string, string> = {
  mistral: 'bg-orange-500/10 text-orange-500 border-orange-500/20',
  llama: 'bg-blue-500/10 text-blue-500 border-blue-500/20',
  phi: 'bg-violet-500/10 text-violet-500 border-violet-500/20',
  qwen: 'bg-cyan-500/10 text-cyan-500 border-cyan-500/20',
  gemma: 'bg-pink-500/10 text-pink-500 border-pink-500/20',
};

const statusConfig = {
  available: { color: 'bg-emerald-500/10 text-emerald-500 border-emerald-500/20', icon: Box },
  testing: { color: 'bg-amber-500/10 text-amber-500 border-amber-500/20', icon: Beaker },
  deprecated: { color: 'bg-red-500/10 text-red-500 border-red-500/20', icon: Archive },
};

function getFamilyColor(family: string) {
  return familyColors[family] || 'bg-muted text-muted-foreground border-border';
}

function formatVRAM(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(mb % 1024 === 0 ? 0 : 1)} GB`;
  return `${mb} MB`;
}

function formatContext(ctx: number): string {
  if (ctx >= 1024) return `${(ctx / 1024).toFixed(0)}K`;
  return `${ctx}`;
}

function StatCard({ label, value, icon: Icon }: { label: string; value: number; icon: typeof Database }) {
  return (
    <div className="bg-card border border-border rounded-xl p-4">
      <div className="flex items-center gap-3">
        <div className="w-10 h-10 rounded-lg bg-muted flex items-center justify-center">
          <Icon className="w-5 h-5 text-muted-foreground" />
        </div>
        <div>
          <div className="text-2xl font-light tabular-nums font-mono text-foreground">{value}</div>
          <div className="text-xs uppercase tracking-wider text-muted-foreground">{label}</div>
        </div>
      </div>
    </div>
  );
}

function ModelCard({
  model,
  onClick,
  onDelete,
}: {
  model: VaultModel;
  onClick: () => void;
  onDelete: () => void;
}) {
  const cfg = statusConfig[model.status] || statusConfig.available;
  const StatusIcon = cfg.icon;

  return (
    <div
      onClick={onClick}
      className="bg-card border border-border rounded-xl p-5 hover:border-primary/30 transition-all cursor-pointer group relative"
    >
      {/* Delete button on hover */}
      <button
        onClick={(e) => { e.stopPropagation(); onDelete(); }}
        className="absolute top-3 right-3 p-1.5 rounded-lg text-muted-foreground opacity-0 group-hover:opacity-100 hover:text-destructive hover:bg-destructive/10 transition-all"
        title="Delete model"
      >
        <Trash2 className="w-4 h-4" />
      </button>

      {/* Header */}
      <div className="flex items-start gap-3 mb-3">
        <div className={cn("w-10 h-10 rounded-lg flex items-center justify-center border flex-shrink-0", getFamilyColor(model.family))}>
          <Database className="w-5 h-5" />
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="font-semibold text-foreground text-sm leading-tight truncate pr-6">{model.name}</h3>
          <code className="text-xs text-muted-foreground font-mono truncate block mt-0.5">{model.source_uri}</code>
        </div>
      </div>

      {/* Badges */}
      <div className="flex flex-wrap gap-1.5 mb-3">
        <span className={cn("inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium border", getFamilyColor(model.family))}>
          {model.family}
        </span>
        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-muted text-muted-foreground border border-border">
          {model.parameters}
        </span>
        {model.quantization !== 'none' && (
          <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-warning/10 text-warning border border-warning/20">
            {model.quantization.toUpperCase()}
          </span>
        )}
        <span className={cn("inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium border", cfg.color)}>
          <StatusIcon className="w-3 h-3" />
          {model.status}
        </span>
      </div>

      {/* Specs */}
      <div className="grid grid-cols-2 gap-2 text-xs mb-3">
        <div className="flex items-center gap-1.5 text-muted-foreground">
          <MemoryStick className="w-3 h-3" />
          <span>{formatVRAM(model.vram_required)} VRAM</span>
        </div>
        <div className="flex items-center gap-1.5 text-muted-foreground">
          <MessageSquare className="w-3 h-3" />
          <span>{formatContext(model.max_context)} ctx</span>
        </div>
      </div>

      {/* Tags */}
      {model.tags && model.tags.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {model.tags.map(tag => (
            <span key={tag} className="px-1.5 py-0.5 rounded text-[10px] bg-muted text-muted-foreground">
              {tag}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

function ModelDetailPanel({
  model,
  onClose,
}: {
  model: VaultModel;
  onClose: () => void;
}) {
  const cfg = statusConfig[model.status] || statusConfig.available;
  const StatusIcon = cfg.icon;

  return (
    <>
      <div className="fixed inset-0 bg-background/60 backdrop-blur-sm z-40" onClick={onClose} />
      <div className="fixed right-0 top-0 h-full w-full max-w-lg bg-card border-l border-border z-50 overflow-y-auto animate-slide-in-right">
        <div className="p-6">
          {/* Header */}
          <div className="flex items-center justify-between mb-6">
            <h2 className="text-lg font-semibold text-foreground">Model Details</h2>
            <button onClick={onClose} className="p-2 rounded-lg text-muted-foreground hover:text-foreground hover:bg-accent transition-colors">
              <X className="w-5 h-5" />
            </button>
          </div>

          {/* Name & URI */}
          <div className="mb-6">
            <h3 className="text-xl font-semibold text-foreground mb-1">{model.name}</h3>
            <code className="text-sm text-primary font-mono break-all">{model.source_uri}</code>
          </div>

          {/* Status + Family */}
          <div className="flex flex-wrap gap-2 mb-6">
            <span className={cn("inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-sm font-medium border", getFamilyColor(model.family))}>
              {model.family}
            </span>
            <span className={cn("inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-sm font-medium border", cfg.color)}>
              <StatusIcon className="w-3.5 h-3.5" />
              {model.status}
            </span>
            {model.quantization !== 'none' && (
              <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-sm font-medium bg-warning/10 text-warning border border-warning/20">
                {model.quantization.toUpperCase()}
              </span>
            )}
          </div>

          {/* Specs Grid */}
          <div className="grid grid-cols-2 gap-4 mb-6">
            <div className="bg-muted/50 rounded-lg p-4 border border-border">
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Parameters</div>
              <div className="text-lg font-semibold text-foreground font-mono">{model.parameters}</div>
            </div>
            <div className="bg-muted/50 rounded-lg p-4 border border-border">
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">VRAM Required</div>
              <div className="text-lg font-semibold text-foreground font-mono">{formatVRAM(model.vram_required)}</div>
            </div>
            <div className="bg-muted/50 rounded-lg p-4 border border-border">
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Max Context</div>
              <div className="text-lg font-semibold text-foreground font-mono">{formatContext(model.max_context)}</div>
            </div>
            <div className="bg-muted/50 rounded-lg p-4 border border-border">
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-1">Source</div>
              <div className="text-lg font-semibold text-foreground">{model.source}</div>
            </div>
          </div>

          {/* Tags */}
          {model.tags && model.tags.length > 0 && (
            <div className="mb-6">
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Tags</div>
              <div className="flex flex-wrap gap-2">
                {model.tags.map(tag => (
                  <span key={tag} className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium bg-muted text-muted-foreground border border-border">
                    <Tag className="w-3 h-3" />
                    {tag}
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Metadata */}
          {model.metadata && Object.keys(model.metadata).length > 0 && (
            <div className="mb-6">
              <div className="text-xs text-muted-foreground uppercase tracking-wider mb-2">Metadata</div>
              <div className="bg-muted/50 rounded-lg border border-border p-3 space-y-2">
                {Object.entries(model.metadata).map(([key, value]) => (
                  <div key={key} className="flex items-center justify-between text-sm">
                    <span className="text-muted-foreground font-mono">{key}</span>
                    <span className="text-foreground font-mono">{value}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Timestamps */}
          <div className="border-t border-border pt-4 space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">ID</span>
              <code className="text-xs text-foreground font-mono">{model.id}</code>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Created</span>
              <span className="text-foreground">{new Date(model.created_at).toLocaleString()}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Updated</span>
              <span className="text-foreground">{new Date(model.updated_at).toLocaleString()}</span>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}

function RegisterModelModal({
  isOpen,
  onClose,
}: {
  isOpen: boolean;
  onClose: () => void;
}) {
  const registerMutation = useRegisterVaultModel();
  const [form, setForm] = useState<CreateVaultModelInput>({
    name: '',
    source_uri: '',
    parameters: '',
    family: '',
    vram_required: 0,
    max_context: 4096,
    quantization: 'none',
    tags: [],
  });
  const [tagsInput, setTagsInput] = useState('');

  const handleSubmit = async () => {
    if (!form.name || !form.source_uri) return;

    const tags = tagsInput
      .split(',')
      .map(t => t.trim())
      .filter(Boolean);

    try {
      await registerMutation.mutateAsync({ ...form, tags });
      toast.success('Model registered');
      onClose();
      setForm({ name: '', source_uri: '', parameters: '', family: '', vram_required: 0, max_context: 4096, quantization: 'none', tags: [] });
      setTagsInput('');
    } catch {
      toast.error('Failed to register model');
    }
  };

  if (!isOpen) return null;

  return (
    <>
      <div className="fixed inset-0 bg-background/80 backdrop-blur-sm z-50" onClick={onClose} />
      <div className="fixed inset-4 md:inset-y-8 md:inset-x-auto md:left-1/2 md:-translate-x-1/2 md:max-w-2xl md:w-full bg-card border border-border rounded-2xl shadow-2xl z-50 overflow-hidden flex flex-col animate-scale-in">
        <div className="p-6 border-b border-border">
          <h2 className="text-xl font-semibold text-foreground">Register Model</h2>
          <p className="text-sm text-muted-foreground mt-1">Add a new model to the registry</p>
        </div>

        <div className="flex-1 overflow-y-auto p-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div className="col-span-2">
              <label className="block text-sm font-medium text-foreground mb-1.5">Name *</label>
              <input
                type="text"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Llama 3.1 8B Instruct"
                className="w-full bg-input border border-border rounded-lg px-4 py-2.5 text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </div>

            <div className="col-span-2">
              <label className="block text-sm font-medium text-foreground mb-1.5">Source URI *</label>
              <input
                type="text"
                value={form.source_uri}
                onChange={(e) => setForm({ ...form, source_uri: e.target.value })}
                placeholder="meta-llama/Meta-Llama-3.1-8B-Instruct"
                className="w-full bg-input border border-border rounded-lg px-4 py-2.5 text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring font-mono text-sm"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-1.5">Parameters</label>
              <input
                type="text"
                value={form.parameters}
                onChange={(e) => setForm({ ...form, parameters: e.target.value })}
                placeholder="8B"
                className="w-full bg-input border border-border rounded-lg px-4 py-2.5 text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-1.5">Family</label>
              <input
                type="text"
                value={form.family}
                onChange={(e) => setForm({ ...form, family: e.target.value })}
                placeholder="llama"
                className="w-full bg-input border border-border rounded-lg px-4 py-2.5 text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-1.5">VRAM Required (MB)</label>
              <input
                type="number"
                value={form.vram_required || ''}
                onChange={(e) => setForm({ ...form, vram_required: parseInt(e.target.value) || 0 })}
                placeholder="16384"
                className="w-full bg-input border border-border rounded-lg px-4 py-2.5 text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-1.5">Max Context</label>
              <input
                type="number"
                value={form.max_context || ''}
                onChange={(e) => setForm({ ...form, max_context: parseInt(e.target.value) || 0 })}
                placeholder="131072"
                className="w-full bg-input border border-border rounded-lg px-4 py-2.5 text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-1.5">Quantization</label>
              <select
                value={form.quantization}
                onChange={(e) => setForm({ ...form, quantization: e.target.value })}
                className="w-full bg-input border border-border rounded-lg px-4 py-2.5 text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              >
                <option value="none">None</option>
                <option value="awq">AWQ</option>
                <option value="gptq">GPTQ</option>
                <option value="int8">INT8</option>
              </select>
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-1.5">Tags</label>
              <input
                type="text"
                value={tagsInput}
                onChange={(e) => setTagsInput(e.target.value)}
                placeholder="chat, instruct, coding"
                className="w-full bg-input border border-border rounded-lg px-4 py-2.5 text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </div>
          </div>
        </div>

        <div className="p-6 border-t border-border flex items-center justify-end gap-3 bg-muted/20">
          <button
            onClick={onClose}
            className="px-5 py-2.5 rounded-lg bg-secondary text-secondary-foreground border border-border hover:bg-accent transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleSubmit}
            disabled={!form.name || !form.source_uri || registerMutation.isPending}
            className="inline-flex items-center gap-2 px-5 py-2.5 rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            {registerMutation.isPending ? (
              <><Loader2 className="w-4 h-4 animate-spin" />Registering...</>
            ) : (
              <><Plus className="w-4 h-4" />Register Model</>
            )}
          </button>
        </div>
      </div>
    </>
  );
}

export function Models() {
  const [filters, setFilters] = useState<VaultModelFilter>({});
  const [searchInput, setSearchInput] = useState('');
  const [selectedModel, setSelectedModel] = useState<VaultModel | null>(null);
  const [showRegisterModal, setShowRegisterModal] = useState(false);

  const { data: modelsData, isLoading } = useVaultModels(filters);
  const { data: stats } = useVaultStats();
  const { data: families } = useVaultFamilies();
  const deleteMutation = useDeleteVaultModel();

  const models = modelsData?.models || [];

  const handleSearch = () => {
    setFilters(f => ({ ...f, search: searchInput || undefined }));
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') handleSearch();
  };

  const handleDelete = async (model: VaultModel) => {
    if (!confirm(`Delete "${model.name}"?`)) return;
    try {
      await deleteMutation.mutateAsync(model.id);
      toast.success('Model deleted');
      if (selectedModel?.id === model.id) setSelectedModel(null);
    } catch {
      toast.error('Failed to delete model');
    }
  };

  const statusFilters = ['all', 'available', 'testing', 'deprecated'] as const;

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Stats Bar */}
      {stats && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          <StatCard label="Total Models" value={stats.total_models} icon={Database} />
          <StatCard label="Available" value={stats.available_models} icon={Box} />
          <StatCard label="Deprecated" value={stats.deprecated_models} icon={Archive} />
          <StatCard label="Families" value={stats.model_families} icon={Cpu} />
        </div>
      )}

      {/* Filter Bar */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center gap-4">
        {/* Search */}
        <div className="relative flex-1 max-w-md w-full">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
          <input
            type="text"
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Search models..."
            className="w-full bg-input border border-border rounded-lg pl-10 pr-4 py-2.5 text-sm text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
          />
          {searchInput && (
            <button
              onClick={() => { setSearchInput(''); setFilters(f => ({ ...f, search: undefined })); }}
              className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
            >
              <X className="w-4 h-4" />
            </button>
          )}
        </div>

        {/* Status pills */}
        <div className="flex items-center gap-1.5">
          {statusFilters.map(s => (
            <button
              key={s}
              onClick={() => setFilters(f => ({ ...f, status: s === 'all' ? undefined : s }))}
              className={cn(
                "px-3 py-1.5 rounded-full text-xs font-medium transition-colors border",
                (s === 'all' && !filters.status) || filters.status === s
                  ? "bg-primary text-primary-foreground border-primary"
                  : "bg-card text-muted-foreground border-border hover:border-primary/50"
              )}
            >
              {s === 'all' ? 'All' : s.charAt(0).toUpperCase() + s.slice(1)}
            </button>
          ))}
        </div>

        {/* Family pills */}
        {families && families.length > 0 && (
          <div className="flex items-center gap-1.5 flex-wrap">
            <span className="text-xs text-muted-foreground mr-1">Family:</span>
            {families.map(f => (
              <button
                key={f}
                onClick={() => setFilters(prev => ({ ...prev, family: prev.family === f ? undefined : f }))}
                className={cn(
                  "px-2.5 py-1 rounded-full text-xs font-medium transition-colors border",
                  filters.family === f
                    ? getFamilyColor(f)
                    : "bg-card text-muted-foreground border-border hover:border-primary/50"
                )}
              >
                {f}
              </button>
            ))}
          </div>
        )}

        {/* Register button */}
        <button
          onClick={() => setShowRegisterModal(true)}
          className="inline-flex items-center gap-2 px-4 py-2.5 rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 transition-colors ml-auto flex-shrink-0"
        >
          <Plus className="w-4 h-4" />
          Register Model
        </button>
      </div>

      {/* Model Grid */}
      {isLoading ? (
        <div className="grid md:grid-cols-2 xl:grid-cols-3 gap-4">
          {[1, 2, 3, 4, 5, 6].map(i => (
            <div key={i} className="h-48 bg-muted rounded-xl animate-pulse" />
          ))}
        </div>
      ) : models.length === 0 ? (
        <div className="bg-card border border-border rounded-xl p-6 text-center py-16">
          <div className="w-16 h-16 rounded-xl bg-muted flex items-center justify-center mx-auto mb-4">
            <Database className="w-8 h-8 text-muted-foreground" />
          </div>
          <h3 className="text-lg font-semibold text-foreground mb-2">No models found</h3>
          <p className="text-muted-foreground text-sm mb-6">
            {filters.search || filters.family || filters.status
              ? 'Try adjusting your filters'
              : 'Register a model to get started'}
          </p>
          {!filters.search && !filters.family && !filters.status && (
            <button
              onClick={() => setShowRegisterModal(true)}
              className="inline-flex items-center gap-2 px-4 py-2.5 rounded-lg bg-primary text-primary-foreground"
            >
              <Plus className="w-4 h-4" />
              Register Model
            </button>
          )}
        </div>
      ) : (
        <>
          <div className="text-sm text-muted-foreground">{models.length} model{models.length !== 1 ? 's' : ''}</div>
          <div className="grid md:grid-cols-2 xl:grid-cols-3 gap-4">
            {models.map(model => (
              <ModelCard
                key={model.id}
                model={model}
                onClick={() => setSelectedModel(model)}
                onDelete={() => handleDelete(model)}
              />
            ))}
          </div>
        </>
      )}

      {/* Detail Panel */}
      {selectedModel && (
        <ModelDetailPanel
          model={selectedModel}
          onClose={() => setSelectedModel(null)}
        />
      )}

      {/* Register Modal */}
      <RegisterModelModal
        isOpen={showRegisterModal}
        onClose={() => setShowRegisterModal(false)}
      />
    </div>
  );
}
