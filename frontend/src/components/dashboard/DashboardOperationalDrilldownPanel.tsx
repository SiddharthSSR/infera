import { ActionButton, Badge, StatusDot } from '../shared';
import { ActionGroup } from '../ActionGroup';
import { CollapsibleSection } from '../CollapsibleSection';
import { SectionHeader } from '../SectionHeader';
import { getDeploymentRemediation, type DeploymentAttemptSummary } from '../../lib/deploymentHistory';
import { formatShortTimestamp } from '../../lib/formatting';
import type { Worker } from '../../types';

function getAttemptTone(summary: DeploymentAttemptSummary): '' | 'warning' | 'error' | 'inactive' {
  if (summary.attempt.inference_verification?.status === 'failed') return 'error';
  if (summary.readiness.tone === 'error') return 'error';
  if (summary.readiness.tone === 'warning') return 'warning';
  if (summary.readiness.tone === 'inactive') return 'inactive';
  return '';
}

type NodeOverviewRow = {
  label: string;
  value: string;
  secondary: string;
};

export function DashboardOperationalDrilldownPanel({
  latestFailure,
  latestVerification,
  hasBillingAttention,
  nodeOverviewRows,
  recentActivity,
  healthyWorkers,
  onOpenNodes,
  onOpenModels,
  onViewUsage,
  onOpenQuickstart,
  onRemediationAction,
}: {
  latestFailure: DeploymentAttemptSummary | undefined;
  latestVerification: DeploymentAttemptSummary | undefined;
  hasBillingAttention: boolean;
  nodeOverviewRows: NodeOverviewRow[];
  recentActivity: DeploymentAttemptSummary[];
  healthyWorkers: Worker[];
  onOpenNodes: () => void;
  onOpenModels: () => void;
  onViewUsage: () => void;
  onOpenQuickstart: () => void;
  onRemediationAction: (action: 'open_workspace' | 'view_capacity' | 'retry_config' | 'focus_instance' | 'verify_inference') => void;
}) {
  return (
    <>
      <SectionHeader
        eyebrow="SECONDARY DETAIL"
        title="Operational drilldown"
        description="Dense operational detail lives here so the top of the page stays readable."
        actions={(
          <ActionGroup compact>
            <ActionButton onClick={onOpenNodes}>OPEN NODES</ActionButton>
            <ActionButton onClick={onOpenModels}>OPEN MODELS</ActionButton>
            {latestFailure ? <ActionButton onClick={onOpenNodes}>VIEW FAILED NODES</ActionButton> : null}
            {latestVerification ? <ActionButton onClick={onOpenModels}>VERIFY SERVING</ActionButton> : null}
            {hasBillingAttention ? <ActionButton onClick={onViewUsage}>VIEW USAGE</ActionButton> : null}
          </ActionGroup>
        )}
      />

      <div className="stack-list" style={{ marginTop: '1.5rem' }}>
        <CollapsibleSection title="NODE OVERVIEW" description="Resource utilization and cost posture for the active workspace.">
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            {nodeOverviewRows.map((row, i, arr) => (
              <div key={row.label} className="cluster-metric-row" style={{ display: 'grid', gridTemplateColumns: '2fr 1fr 1fr', padding: '1rem 0', borderBottom: i < arr.length - 1 ? '1px solid #EEEEEC' : 'none', alignItems: 'center' }}>
                <div style={{ fontSize: '0.9rem' }}>{row.label}</div>
                <div className="mono">{row.value}</div>
                <div style={{ textAlign: 'right', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>{row.secondary}</div>
              </div>
            ))}
          </div>
        </CollapsibleSection>

        <CollapsibleSection title="RECENT DEPLOYMENT ACTIVITY" description="Primary sentence first. Expand only when you need remediation context." defaultExpanded>
          {recentActivity.length > 0 ? (
            <div className="dashboard-activity-list">
              {recentActivity.map((summary) => {
                const remediation = getDeploymentRemediation(summary);
                const modelName = summary.attempt.selected_model_name || summary.attempt.request.models?.[0]?.split('/').pop() || summary.instance?.name || 'Deployment attempt';
                const primarySentence = summary.attempt.inference_verification?.status === 'failed'
                  ? `${modelName} failed live inference verification.`
                  : summary.readiness.label === 'SERVING VERIFIED'
                    ? `${modelName} is serving and recently verified.`
                    : `${modelName} is ${summary.readiness.label.toLowerCase()}.`;
                return (
                  <div key={summary.attempt.id} className="dashboard-activity-item">
                    <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '1rem' }}>
                      <div>
                        <div style={{ fontSize: '0.9rem', fontWeight: 600 }}>{primarySentence}</div>
                        <div className="chip-row" style={{ marginTop: '0.55rem' }}>
                          <Badge tone={getAttemptTone(summary) || undefined}>{summary.readiness.label}</Badge>
                          <Badge>{summary.instance?.provider?.toUpperCase() || 'REQUEST'}</Badge>
                          <Badge>{formatShortTimestamp(summary.attempt.updated_at || summary.attempt.created_at)}</Badge>
                          {summary.attempt.inference_verification?.status === 'passed' && (
                            <Badge>{summary.attempt.inference_verification.latency_ms != null ? `${summary.attempt.inference_verification.latency_ms}ms` : 'verified'}</Badge>
                          )}
                          {summary.attempt.inference_verification?.status === 'failed' && (
                            <Badge tone="error">verification failed</Badge>
                          )}
                        </div>
                        <div className="dashboard-summary-text" style={{ marginTop: '0.55rem' }}>{summary.readiness.detail}</div>
                      </div>
                      {remediation ? (
                        <ActionButton onClick={() => onRemediationAction(remediation.action)}>
                          {remediation.label}
                        </ActionButton>
                      ) : null}
                    </div>
                  </div>
                );
              })}
            </div>
          ) : (
            <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
              No recent deployment activity yet. Provision capacity from Nodes to start tracking deployment health here.
              <div className="help-actions">
                <ActionButton onClick={onOpenNodes}>OPEN NODES</ActionButton>
                <ActionButton onClick={onOpenQuickstart}>OPEN QUICKSTART</ActionButton>
              </div>
            </div>
          )}
        </CollapsibleSection>

        <CollapsibleSection title="WORKER STATUS" description="Worker heartbeats and active model assignment.">
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.6 }}>
            {healthyWorkers.length > 0 ? (
              healthyWorkers.slice(0, 4).map((worker) => (
                <div className="worker-status-row" key={worker.worker_id} style={{ borderBottom: '1px solid #F0F0F0', padding: '0.5rem 0', display: 'flex', gap: '1rem' }}>
                  <span style={{ color: 'var(--text-primary)', minWidth: 80 }}>{worker.worker_id.slice(0, 8)}</span>
                  <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <StatusDot tone="success" size={6} />
                    GPU {worker.gpu_utilization}%
                  </span>
                  <span>{worker.models?.[0]?.split('/').pop() || '-'}</span>
                </div>
              ))
            ) : (
              <div style={{ padding: '0.5rem 0' }}>No workers connected.</div>
            )}
          </div>
        </CollapsibleSection>
      </div>
    </>
  );
}
