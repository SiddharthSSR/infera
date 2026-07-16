import { ActionButton, Cell, GridRow, LabelText } from '../shared';
import type { DeploymentAttemptRecord, DeploymentAttemptSummary } from '../../lib/deploymentHistory';
import type { NodeIncident } from '../../lib/instanceIncidents';
import { formatGPUDisplayName } from '../../lib/labels';
import type { Instance } from '../../types';

type InstanceIncidentActionsProps = {
  instance: Instance;
  summary: DeploymentAttemptSummary | null;
  verifyingAttemptID: string | null;
  compact?: boolean;
  onVerify: (summary: DeploymentAttemptSummary) => void;
  onRetry: (attempt: DeploymentAttemptRecord) => void;
};

type IncidentRow = {
  instance: Instance;
  summary: DeploymentAttemptSummary | null;
  incident: NodeIncident;
};

type ModelDrilldownPanelProps = {
  drilldownModelLabel: string;
  drilldownFocus: string | null;
  filteredInstanceCount: number;
  incidentRows: IncidentRow[];
  verifyingAttemptID: string | null;
  onClear: () => void;
  onOpenModels: () => void;
  onFocusInstance: (instanceID: string) => void;
  onVerify: (summary: DeploymentAttemptSummary) => void;
  onRetry: (attempt: DeploymentAttemptRecord) => void;
};

export function InstanceIncidentActions({
  instance,
  summary,
  verifyingAttemptID,
  compact = false,
  onVerify,
  onRetry,
}: InstanceIncidentActionsProps) {
  if (!summary) return null;

  const buttonStyle = compact ? { fontSize: '0.65rem' } : { fontSize: '0.65rem', marginRight: '1rem' };
  const hasModel = Boolean(instance.models?.length || summary.attempt.request.models?.length);

  return (
    <>
      {instance.status === 'running' && hasModel && (
        <ActionButton
          style={buttonStyle}
          disabled={verifyingAttemptID === summary.attempt.id}
          onClick={() => onVerify(summary)}
        >
          {verifyingAttemptID === summary.attempt.id ? 'VERIFYING...' : 'VERIFY NOW'}
        </ActionButton>
      )}
      {summary.retryable && (
        <ActionButton style={buttonStyle} onClick={() => onRetry(summary.attempt)}>
          RETRY CONFIG
        </ActionButton>
      )}
    </>
  );
}

export function ModelDrilldownPanel({
  drilldownModelLabel,
  drilldownFocus,
  filteredInstanceCount,
  incidentRows,
  verifyingAttemptID,
  onClear,
  onOpenModels,
  onFocusInstance,
  onVerify,
  onRetry,
}: ModelDrilldownPanelProps) {
  return (
    <GridRow>
      <Cell span={4}>
        <div className="help-callout">
          <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '1rem', flexWrap: 'wrap' }}>
            <div>
              <LabelText as="div">MODEL DRILLDOWN</LabelText>
              <div className="help-callout-copy">
                Showing {drilldownFocus === 'degraded' ? 'degraded runtime nodes' : 'nodes'} for <strong>{drilldownModelLabel}</strong>. Use this view to inspect the deployments behind the model health signal from the registry.
              </div>
            </div>
            <span className={`badge ${drilldownFocus === 'degraded' ? 'status-error' : ''}`}>
              {filteredInstanceCount} NODE{filteredInstanceCount === 1 ? '' : 'S'}
            </span>
          </div>
          {incidentRows.length > 0 && (
            <div style={{ display: 'grid', gap: '0.75rem', marginTop: '1rem' }}>
              {incidentRows.slice(0, 3).map(({ instance, summary, incident }) => (
                <div
                  key={`incident-${instance.id}`}
                  style={{
                    display: 'grid',
                    gap: '0.5rem',
                    padding: '0.85rem 1rem',
                    border: '1px solid var(--border-color)',
                    background: 'rgba(255, 255, 255, 0.88)',
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', flexWrap: 'wrap', alignItems: 'center' }}>
                    <div>
                      <div className="mono" style={{ fontSize: '0.85rem' }}>{instance.name || instance.id.slice(0, 16)}</div>
                      <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginTop: '0.2rem' }}>
                        {instance.gpu_count}x {formatGPUDisplayName(instance.gpu_type)}
                      </div>
                    </div>
                    <span className={`badge ${incident.tone ? `status-${incident.tone}` : ''}`}>{incident.title}</span>
                  </div>
                  <div style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.6 }}>
                    {incident.detail}
                  </div>
                  <div className="help-actions">
                    <ActionButton onClick={() => onFocusInstance(instance.id)}>FOCUS NODE</ActionButton>
                    <InstanceIncidentActions
                      instance={instance}
                      summary={summary}
                      verifyingAttemptID={verifyingAttemptID}
                      onVerify={onVerify}
                      onRetry={onRetry}
                    />
                  </div>
                </div>
              ))}
            </div>
          )}
          <div className="help-actions">
            <ActionButton onClick={onClear}>CLEAR DRILLDOWN</ActionButton>
            <ActionButton onClick={onOpenModels}>OPEN MODELS</ActionButton>
          </div>
        </div>
      </Cell>
    </GridRow>
  );
}
