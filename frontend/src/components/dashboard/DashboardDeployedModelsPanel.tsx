import { ActionButton } from '../shared';
import { ActionGroup } from '../ActionGroup';
import { SectionHeader } from '../SectionHeader';
import type { Model } from '../../types';

export function DashboardDeployedModelsPanel({
  loadedModels,
  onDeployModel,
  onOpenNodes,
  onOpenOnboarding,
}: {
  loadedModels: Model[];
  onDeployModel: () => void;
  onOpenNodes: () => void;
  onOpenOnboarding: () => void;
}) {
  return (
    <>
      <SectionHeader
        eyebrow="SECONDARY DETAIL"
        title="DEPLOYED MODELS"
        description="Keep the top of the dashboard focused on what changed. Use this section when you need the serving inventory."
        actions={(
          <ActionGroup compact>
            <ActionButton onClick={onDeployModel}>DEPLOY NEW MODEL</ActionButton>
            <ActionButton onClick={onOpenNodes}>OPEN NODES</ActionButton>
            <ActionButton onClick={onOpenOnboarding}>SEE ONBOARDING PATH</ActionButton>
          </ActionGroup>
        )}
      />
      {loadedModels.length > 0 ? (
        <div className="stack-list" style={{ marginTop: '1.75rem' }}>
          {loadedModels.slice(0, 3).map((model) => (
            <div key={model.id} className="stack-item">
              <div className="label-text">
                <span className="nav-diamond">&#9671;</span>
                {model.family?.toUpperCase() || 'MODEL'}
              </div>
              <h2 style={{ fontSize: '1.75rem', marginTop: '0.5rem', lineHeight: 1.1, fontWeight: 500, letterSpacing: '-0.02em' }}>
                {model.id.split('/').pop()}
              </h2>
              <div style={{ marginTop: '0.5rem', fontSize: '0.9rem', color: 'var(--text-secondary)' }}>
                {model.quantization && `Quantization: ${model.quantization}`}
                {model.max_context && <>&nbsp;|&nbsp;Context: {(model.max_context / 1000).toFixed(0)}k</>}
              </div>
              {model.tags && model.tags.length > 0 && (
                <div className="model-tags-row" style={{ display: 'flex', gap: '1rem', marginTop: '1rem' }}>
                  {model.tags.map((tag) => (<span key={tag} className="tag">{tag}</span>))}
                </div>
              )}
            </div>
          ))}
        </div>
      ) : (
        <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem', marginTop: '1.75rem' }}>
          No models deployed yet. Provision an instance to get started.
        </div>
      )}
    </>
  );
}
