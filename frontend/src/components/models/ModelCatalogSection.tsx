import type { Model } from '../../types';
import { formatVerificationMeta } from '../../lib/formatting';
import { verificationToneClass } from '../../lib/labels';
import type { ModelVerificationFreshness } from '../../lib/modelRuntimeDrilldown';
import { ActionButton, Badge } from '../shared';
import { CollapsibleSection } from '../CollapsibleSection';
import { MetadataList } from '../MetadataList';

type ModelServingOverviewLike = {
  state: 'not_deployed' | 'runtime_pending' | 'serving_unverified' | 'serving_verified' | 'serving_failed' | 'degraded';
  summary: string;
  badgeLabel: string;
  badgeTone: '' | 'warning' | 'error' | 'inactive';
  activeInstances: number;
  verifiedAt?: string;
  latestVerificationLatencyMs?: number;
};

type ModelRuntimeLike = {
  activeNodes: number;
  degradedNodes: number;
  verificationFreshness: ModelVerificationFreshness;
  verificationLabel: string;
  latestIssue?: string | null;
};

type DeployStateLike = {
  state: string;
  summary: string;
  actionLabel: string;
  actionTarget: string;
};

function HighlightMatch({ text, query }: { text: string; query: string }) {
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

export function ModelCatalogSection({
  isMobile,
  filtered,
  searchQuery,
  deferredQuery,
  activeTagFilter,
  onClearSearch,
  onClearTagFilter,
  onOpenRegister,
  onOpenNodes,
  onOpenQuickstart,
  getOverview,
  getRuntime,
  getDeployState,
  verifyingModelID,
  onOpenSlideOver,
  onNavigate,
  onVerifyServing,
  hasVaultEntry,
  onRemove,
}: {
  isMobile: boolean;
  filtered: Model[];
  searchQuery: string;
  deferredQuery: string;
  activeTagFilter: string | null;
  onClearSearch: () => void;
  onClearTagFilter: () => void;
  onOpenRegister: () => void;
  onOpenNodes: () => void;
  onOpenQuickstart: () => void;
  getOverview: (modelId: string) => ModelServingOverviewLike;
  getRuntime: (modelId: string) => ModelRuntimeLike;
  getDeployState: (model: Model) => DeployStateLike;
  verifyingModelID: string | null;
  onOpenSlideOver: (modelId: string) => void;
  onNavigate: (path: string) => void;
  onVerifyServing: (model: Model) => void;
  hasVaultEntry: (modelId: string) => boolean;
  onRemove: (modelId: string) => void;
}) {
  const renderEmptyState = () => (
    <div style={{ padding: isMobile ? '3rem 1rem' : '3rem 2rem', textAlign: 'center', color: 'var(--text-secondary)' }}>
      {searchQuery ? (
        <>
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" style={{ opacity: 0.4, marginBottom: '0.75rem' }}>
            <circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" />
          </svg>
          <div style={{ fontSize: '0.95rem', fontWeight: 500, color: 'var(--text-primary)', marginBottom: '0.35rem' }}>
            No models match &ldquo;{searchQuery}&rdquo;
          </div>
          <div style={{ fontSize: '0.85rem', lineHeight: 1.6, maxWidth: isMobile ? 360 : 420, margin: '0 auto 1rem' }}>
            Try a different name, provider, quantization, or tag. Filters are combined with the active tag if one is selected.
          </div>
          <div className="help-actions" style={{ justifyContent: 'center' }}>
            <ActionButton onClick={onClearSearch}>CLEAR SEARCH</ActionButton>
            {activeTagFilter && <ActionButton onClick={onClearTagFilter}>CLEAR TAG FILTER</ActionButton>}
          </div>
        </>
      ) : (
        <>
          No models in registry. Add one to get started.
          <div className="help-actions" style={{ justifyContent: 'center' }}>
            <ActionButton onClick={onOpenRegister}>ADD MODEL</ActionButton>
            <ActionButton onClick={onOpenNodes}>OPEN NODES</ActionButton>
            <ActionButton onClick={onOpenQuickstart}>OPEN QUICKSTART</ActionButton>
          </div>
        </>
      )}
    </div>
  );

  if (isMobile) {
    return (
      <div className="mobile-data-list models-list-section">
        {filtered.length === 0 ? renderEmptyState() : filtered.map((model, rowIndex) => {
          const isLoaded = model.loaded !== false;
          const isDeploying = model.vault_status === 'testing';
          const statusDotClass = isLoaded ? '' : isDeploying ? 'warning' : 'inactive';
          const statusLabel = isLoaded ? 'Active' : isDeploying ? 'Deploying...' : 'Available';
          const deployState = getDeployState(model);
          const overview = getOverview(model.id);
          const runtime = getRuntime(model.id);
          const shortName = model.id.split('/').pop() || model.id;
          const provider = model.owned_by || model.family || '';
          const deploymentsTarget = `/instances?model=${encodeURIComponent(model.id)}`;
          const degradedTarget = `/instances?model=${encodeURIComponent(model.id)}&focus=degraded`;

          return (
            <div
              key={model.id}
              className="mobile-data-card"
              style={{
                animation: 'fade-slide-in 0.3s ease both',
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
                <div><span className="label-text">DEPLOYMENTS</span> <span className="mono">{runtime.activeNodes}</span></div>
                <div><span className="label-text">VERIFY</span> <Badge className={verificationToneClass(runtime.verificationFreshness)}>{runtime.verificationLabel}</Badge></div>
                {runtime.degradedNodes > 0 && (
                  <div><span className="label-text">DEGRADED</span> <span className="mono">{runtime.degradedNodes} node{runtime.degradedNodes === 1 ? '' : 's'}</span></div>
                )}
                <div><span className="label-text">DEPLOY</span> <span>{deployState.summary}</span></div>
                <div><span className="label-text">STATUS</span> <span>{overview.summary}</span></div>
              </div>

              <div className="mobile-data-actions">
                {runtime.activeNodes > 0 && (
                  <button type="button" className="mobile-data-action" onClick={() => onOpenSlideOver(model.id)}>
                    MANAGE
                  </button>
                )}
                <button
                  type="button"
                  className="mobile-data-action"
                  onClick={() => onNavigate(runtime.activeNodes > 0 ? deploymentsTarget : deployState.actionTarget)}
                >
                  {runtime.activeNodes > 0 ? 'VIEW DEPLOYMENTS' : deployState.actionLabel}
                </button>
                {runtime.activeNodes > 0 ? (
                  <button
                    type="button"
                    className={`mobile-data-action${overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh' ? ' muted' : ''}`}
                    disabled={verifyingModelID === model.id}
                    onClick={() => overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh'
                      ? onNavigate(`/instances?provision=true&model=${encodeURIComponent(model.id)}&from=models`)
                      : onVerifyServing(model)}
                  >
                    {verifyingModelID === model.id ? 'VERIFYING...' : overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh' ? 'DEPLOY MORE' : 'VERIFY NOW'}
                  </button>
                ) : null}
                {runtime.degradedNodes > 0 && (
                  <button type="button" className="mobile-data-action muted" onClick={() => onNavigate(degradedTarget)}>
                    OPEN DEGRADED NODES
                  </button>
                )}
                {hasVaultEntry(model.id) && !isLoaded && !isDeploying && (
                  <button type="button" className="mobile-data-action danger" onClick={() => onRemove(model.id)}>
                    REMOVE
                  </button>
                )}
              </div>

              <div style={{ marginTop: '1rem' }}>
                <CollapsibleSection title="SHOW DETAILS" description="Secondary runtime, verification, and registry metadata.">
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
        })}
      </div>
    );
  }

  return (
    <div className="models-list-section">
      <div className="stack-list">
        {filtered.length === 0 ? renderEmptyState() : filtered.map((model, rowIndex) => {
          const isLoaded = model.loaded !== false;
          const isDeploying = model.vault_status === 'testing';
          const statusDotClass = isLoaded ? '' : isDeploying ? 'warning' : 'inactive';
          const statusLabel = isLoaded ? 'Active' : isDeploying ? 'Deploying...' : 'Available';
          const deployState = getDeployState(model);
          const overview = getOverview(model.id);
          const runtime = getRuntime(model.id);
          const shortName = model.id.split('/').pop() || model.id;
          const provider = model.owned_by || model.family || '';
          const deploymentsTarget = `/instances?model=${encodeURIComponent(model.id)}`;
          const degradedTarget = `/instances?model=${encodeURIComponent(model.id)}&focus=degraded`;

          return (
            <div
              key={model.id}
              className="stack-item model-row-card"
              data-testid="model-row-card"
              style={{
                animation: 'fade-slide-in 0.3s ease both',
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
                    <button type="button" className="action-link" onClick={() => onOpenSlideOver(model.id)}>
                      MANAGE
                    </button>
                  )}
                  <button
                    type="button"
                    className="action-link"
                    onClick={() => onNavigate(runtime.activeNodes > 0 ? deploymentsTarget : deployState.actionTarget)}
                  >
                    {runtime.activeNodes > 0 ? 'VIEW DEPLOYMENTS' : deployState.actionLabel}
                  </button>
                  {runtime.activeNodes > 0 ? (
                    <button
                      type="button"
                      className={`action-link${overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh' ? ' muted' : ''}`}
                      disabled={verifyingModelID === model.id}
                      onClick={() => overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh'
                        ? onNavigate(`/instances?provision=true&model=${encodeURIComponent(model.id)}&from=models`)
                        : onVerifyServing(model)}
                    >
                      {verifyingModelID === model.id ? 'VERIFYING...' : overview.state === 'serving_verified' && runtime.degradedNodes === 0 && runtime.verificationFreshness === 'fresh' ? 'DEPLOY MORE' : 'VERIFY NOW'}
                    </button>
                  ) : (
                    <button type="button" className={`action-link${deployState.state === 'capacity' ? ' muted' : ''}`} onClick={onOpenNodes}>
                      OPEN NODES
                    </button>
                  )}
                  {runtime.degradedNodes > 0 && (
                    <button type="button" className="action-link danger" onClick={() => onNavigate(degradedTarget)}>
                      OPEN DEGRADED NODES
                    </button>
                  )}
                  {hasVaultEntry(model.id) && !isLoaded && !isDeploying && (
                    <button type="button" className="action-link danger" onClick={() => onRemove(model.id)}>
                      REMOVE
                    </button>
                  )}
                </div>
              </div>

              <div style={{ marginTop: '1rem' }}>
                <CollapsibleSection title="SHOW DETAILS" description="Verification freshness, runtime issues, and extra registry metadata.">
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
        })}
      </div>
    </div>
  );
}
