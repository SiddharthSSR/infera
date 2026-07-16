import type { Model } from '../../types';
import type { ModelDeployReadiness, ModelServingOverview } from '../../hooks/useModelsViewState';
import { ActionGroup } from '../ActionGroup';
import { SectionHeader } from '../SectionHeader';
import { ActionButton, Badge, Cell, GridRow, LabelText } from '../shared';

export function ModelsOverviewSection({
  searchQuery,
  onSearchQueryChange,
  onClearSearch,
  displayModelCount,
  activeCount,
  readyCount,
  servingVerifiedCount,
  showFilteredCount,
  isSearchStale,
  filteredCount,
  allTags,
  activeTagFilter,
  onToggleTagFilter,
  onClearTagFilter,
  onOpenRegister,
  onOpenNodes,
  onOpenDocs,
  recommendedModels,
  getDeployState,
  getOverview,
  recommendedLabels,
  onNavigate,
  onFilterToModel,
  connectedProviderCount,
}: {
  searchQuery: string;
  onSearchQueryChange: (value: string) => void;
  onClearSearch: () => void;
  displayModelCount: number;
  activeCount: number;
  readyCount: number;
  servingVerifiedCount: number;
  showFilteredCount: boolean;
  isSearchStale: boolean;
  filteredCount: number;
  allTags: string[];
  activeTagFilter: string | null;
  onToggleTagFilter: (tag: string) => void;
  onClearTagFilter: () => void;
  onOpenRegister: () => void;
  onOpenNodes: () => void;
  onOpenDocs: () => void;
  recommendedModels: Model[];
  getDeployState: (model: Model) => ModelDeployReadiness;
  getOverview: (modelId: string) => ModelServingOverview;
  recommendedLabels: Record<string, string>;
  onNavigate: (path: string) => void;
  onFilterToModel: (modelId: string) => void;
  connectedProviderCount: number;
}) {
  return (
    <>
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
              onChange={(e) => onSearchQueryChange(e.target.value)}
              placeholder="Filter by name, provider, quant, tag..."
            />
            {searchQuery && (
              <button
                type="button"
                onClick={onClearSearch}
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
            <Badge>{displayModelCount} REGISTRY MODELS</Badge>
            <Badge>{activeCount} ACTIVE</Badge>
            <Badge>{readyCount} READY TO DEPLOY</Badge>
            <Badge>{servingVerifiedCount} SERVING VERIFIED</Badge>
            {showFilteredCount && (
              <Badge style={{ opacity: isSearchStale ? 0.5 : 1, transition: 'opacity 0.15s' }}>
                SHOWING {filteredCount} OF {displayModelCount}
              </Badge>
            )}
          </div>
          {allTags.length > 0 && (
            <div className="chip-row" style={{ flexWrap: 'wrap', gap: '0.35rem' }}>
              {allTags.map((tag) => (
                <button
                  key={tag}
                  type="button"
                  className="tag"
                  onClick={() => onToggleTagFilter(tag)}
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
                  onClick={onClearTagFilter}
                  style={{ cursor: 'pointer', borderStyle: 'dashed', opacity: 0.6 }}
                >
                  CLEAR
                </button>
              )}
            </div>
          )}
        </div>
        <ActionGroup compact>
          <ActionButton variant="primary" onClick={onOpenRegister}>
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
                  <ActionButton onClick={onOpenNodes}>OPEN NODES</ActionButton>
                  <ActionButton onClick={onOpenDocs}>READ API DOCS</ActionButton>
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
                  const deployState = getDeployState(model);
                  const overview = getOverview(model.id);
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
                          {recommendedLabels[model.id] ? <Badge>{recommendedLabels[model.id]}</Badge> : null}
                          <Badge tone={overview.badgeTone || undefined}>{overview.badgeLabel}</Badge>
                        </div>
                      </div>
                      <div style={{ marginTop: '0.8rem', color: 'var(--text-secondary)', fontSize: '0.82rem', lineHeight: 1.6 }}>
                        {deployState.summary}
                      </div>
                      <div className="action-group compact" style={{ marginTop: '0.9rem' }}>
                        <ActionButton onClick={() => onNavigate(deployState.actionTarget)}>
                          {deployState.actionLabel}
                        </ActionButton>
                        <ActionButton onClick={() => onFilterToModel(model.id.split('/').pop() || model.id)}>
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

      <GridRow style={{ background: 'var(--bg-accent)' }}>
        <Cell>
          <LabelText as="div">REGISTRY MODELS</LabelText>
          <div className="mono" style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {displayModelCount}
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
            <span className={`status-dot ${connectedProviderCount > 0 ? '' : 'inactive'}`} />
            {connectedProviderCount > 0
              ? `${connectedProviderCount} provider${connectedProviderCount === 1 ? '' : 's'} live.`
              : 'No live provider is currently connected for deployments.'}
          </div>
        </Cell>
      </GridRow>
    </>
  );
}
