import { ActionButton, Badge, Cell, GridRow, LabelText } from '../shared';
import { CollapsibleSection } from '../CollapsibleSection';
import { formatCompactCount, formatShortTimestamp } from '../../lib/formatting';
import type { DeploymentAttemptSummary } from '../../lib/deploymentHistory';

function getAttemptTone(summary: DeploymentAttemptSummary): '' | 'warning' | 'error' | 'inactive' {
  if (summary.attempt.inference_verification?.status === 'failed') return 'error';
  if (summary.readiness.tone === 'error') return 'error';
  if (summary.readiness.tone === 'warning') return 'warning';
  if (summary.readiness.tone === 'inactive') return 'inactive';
  return '';
}

type UsageTrendEntry = {
  day: string;
  requests: number;
  tokens: number;
};

export function DashboardTrendsRow({
  deploymentTrend,
  deploymentHistoryPreview,
  hiddenDeploymentHistoryCount,
  verificationTrend,
  usageTrend,
  usageTrendMaxRequests,
  onOpenModels,
  onOpenDocs,
  onOpenWorkspace,
  onDeployModel,
}: {
  deploymentTrend: {
    recent: DeploymentAttemptSummary[];
    failed: number;
    pending: number;
    stable: number;
  };
  deploymentHistoryPreview: DeploymentAttemptSummary[];
  hiddenDeploymentHistoryCount: number;
  verificationTrend: DeploymentAttemptSummary[];
  usageTrend: UsageTrendEntry[];
  usageTrendMaxRequests: number;
  onOpenModels: () => void;
  onOpenDocs: () => void;
  onOpenWorkspace: () => void;
  onDeployModel: () => void;
}) {
  return (
    <GridRow className="dashboard-trends-row">
      <Cell span={2} className="dashboard-trend-cell">
        <LabelText as="div" style={{ marginBottom: '1rem' }}>RECENT CHANGES</LabelText>
        <CollapsibleSection
          title="DEPLOYMENT HISTORY"
          description="Latest provisioning attempts. Open the full history only when you need the details."
          summary={(
            <div className="dashboard-trend-summary">
              <span>{deploymentTrend.stable} stable</span>
              <span>{deploymentTrend.pending} pending</span>
              <span>{deploymentTrend.failed} failed</span>
              {hiddenDeploymentHistoryCount > 0 ? <span>+{hiddenDeploymentHistoryCount} hidden</span> : null}
            </div>
          )}
        >
          {deploymentTrend.recent.length > 0 ? (
            <div className="dashboard-trend-list">
              {deploymentHistoryPreview.map((summary) => (
                <div key={summary.attempt.id} className="dashboard-trend-item">
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '1rem' }}>
                    <div style={{ fontSize: '0.88rem', fontWeight: 500 }}>
                      {summary.attempt.selected_model_name || summary.attempt.request.models?.[0]?.split('/').pop() || summary.instance?.name || 'Deployment attempt'}
                    </div>
                    <Badge tone={getAttemptTone(summary) || undefined}>{summary.readiness.label}</Badge>
                  </div>
                  <div className="dashboard-summary-text" style={{ marginTop: '0.35rem' }}>{summary.readiness.detail}</div>
                </div>
              ))}
              {hiddenDeploymentHistoryCount > 0 && deploymentTrend.recent.slice(3).map((summary) => (
                <div key={summary.attempt.id} className="dashboard-trend-item">
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '1rem' }}>
                    <div style={{ fontSize: '0.88rem', fontWeight: 500 }}>
                      {summary.attempt.selected_model_name || summary.attempt.request.models?.[0]?.split('/').pop() || summary.instance?.name || 'Deployment attempt'}
                    </div>
                    <Badge tone={getAttemptTone(summary) || undefined}>{summary.readiness.label}</Badge>
                  </div>
                  <div className="dashboard-summary-text" style={{ marginTop: '0.35rem' }}>{summary.readiness.detail}</div>
                </div>
              ))}
            </div>
          ) : (
            <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>No deployment attempts recorded yet.</div>
          )}
        </CollapsibleSection>
      </Cell>

      <Cell className="dashboard-trend-cell">
        <LabelText as="div" style={{ marginBottom: '1rem' }}>VERIFICATION HISTORY</LabelText>
        {verificationTrend.length > 0 ? (
          <div className="dashboard-trend-list">
            {verificationTrend.map((summary) => (
              <div key={summary.attempt.id} className="dashboard-trend-item">
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.75rem' }}>
                  <Badge tone={summary.attempt.inference_verification?.status === 'failed' ? 'error' : undefined}>
                    {summary.attempt.inference_verification?.status === 'failed' ? 'FAILED' : 'PASSED'}
                  </Badge>
                  <span style={{ fontSize: '0.72rem', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-secondary)' }}>
                    {formatShortTimestamp(summary.attempt.inference_verification?.verified_at)}
                  </span>
                </div>
                <div style={{ marginTop: '0.5rem', fontSize: '0.84rem' }}>
                  {summary.attempt.selected_model_name || summary.attempt.inference_verification?.model?.split('/').pop() || 'Deployment'}
                </div>
                <div className="dashboard-summary-text" style={{ marginTop: '0.3rem' }}>
                  {summary.attempt.inference_verification?.status === 'failed'
                    ? (summary.attempt.inference_verification.error || 'Inference verification failed.')
                    : summary.attempt.inference_verification?.latency_ms != null
                      ? `Latency ${summary.attempt.inference_verification.latency_ms}ms`
                      : 'Live verification completed successfully.'}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
            No inference verification history yet.
            <div className="help-actions">
              <ActionButton onClick={onOpenModels}>OPEN MODELS</ActionButton>
              <ActionButton onClick={onOpenDocs}>READ VERIFY FLOW</ActionButton>
            </div>
          </div>
        )}
      </Cell>

      <Cell className="dashboard-trend-cell">
        <LabelText as="div" style={{ marginBottom: '1rem' }}>USAGE TRAJECTORY</LabelText>
        {usageTrend.length > 0 ? (
          <div className="dashboard-trend-list">
            {usageTrend.map((entry) => (
              <div key={entry.day} className="dashboard-usage-trend-row">
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '1rem' }}>
                  <span style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                    {new Date(`${entry.day}T00:00:00Z`).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}
                  </span>
                  <span className="mono" style={{ fontSize: '0.78rem' }}>{formatCompactCount(entry.requests)} req</span>
                </div>
                <div className="dashboard-usage-bar-track">
                  <div
                    className="dashboard-usage-bar-fill"
                    style={{ width: `${usageTrendMaxRequests > 0 ? Math.max((entry.requests / usageTrendMaxRequests) * 100, 6) : 0}%` }}
                  />
                </div>
                <div style={{ marginTop: '0.28rem', fontSize: '0.72rem', color: 'var(--text-secondary)' }}>
                  {formatCompactCount(entry.tokens)} tokens
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
            No workspace usage recorded yet this month.
            <div className="help-actions">
              <ActionButton onClick={onOpenWorkspace}>OPEN WORKSPACE</ActionButton>
              <ActionButton onClick={onDeployModel}>DEPLOY A MODEL</ActionButton>
            </div>
          </div>
        )}
      </Cell>
    </GridRow>
  );
}
