import type { ReactNode } from 'react';

import { ActionButton, Cell, GridRow, LabelText } from '../shared';
import { DeploymentTimeline } from '../DeploymentTimeline';
import {
  getDeploymentRemediation,
  getDeploymentTimeline,
  type DeploymentAttemptRecord,
  type DeploymentAttemptSummary,
  type DeploymentRemediation,
} from '../../lib/deploymentHistory';
import {
  formatInferenceVerificationCopy,
  getDeploymentAttemptTitle,
  getLatestDeploymentTitle,
} from '../../lib/deploymentPresentation';
import { formatGPUDisplayName, instanceStatusLabel } from '../../lib/labels';
import { getProviderDisplayName } from '../../lib/providerInventory';
import type { Instance } from '../../types';

type DeploymentPanelActions = {
  verifyingAttemptID: string | null;
  onRemediation: (
    summary: DeploymentAttemptSummary,
    remediation: DeploymentRemediation | null,
  ) => void;
  onRetry: (attempt: DeploymentAttemptRecord) => void;
  formatAttemptTime: (value: string) => string;
  formatVerificationLatency: (latencyMs?: number) => string | null;
};

type DeploymentHistorySectionProps = DeploymentPanelActions & {
  deploymentHistory: DeploymentAttemptSummary[];
  latestAttemptID: string | null;
  onNewAttempt: () => void;
  renderInstanceActions: (instance: Instance) => ReactNode;
};

type LatestDeploymentBannerProps = DeploymentPanelActions & {
  latestDeployment: DeploymentAttemptSummary;
};

function DeploymentVerificationNotice({
  summary,
  formatAttemptTime,
  formatVerificationLatency,
  maxWidth,
}: {
  summary: DeploymentAttemptSummary;
  formatAttemptTime: (value: string) => string;
  formatVerificationLatency: (latencyMs?: number) => string | null;
  maxWidth: string;
}) {
  const copy = formatInferenceVerificationCopy(
    summary.attempt.inference_verification,
    formatAttemptTime,
    formatVerificationLatency,
  );

  if (!copy) return null;

  return (
    <div style={{ marginTop: '0.75rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth }}>
      <LabelText as="div" style={{ marginBottom: '0.35rem' }}>FIRST INFERENCE</LabelText>
      {copy}
    </div>
  );
}

export function LatestDeploymentBanner({
  latestDeployment,
  verifyingAttemptID,
  onRemediation,
  onRetry,
  formatAttemptTime,
  formatVerificationLatency,
}: LatestDeploymentBannerProps) {
  const latestTimeline = getDeploymentTimeline(latestDeployment);
  const latestRemediation = getDeploymentRemediation(latestDeployment);

  return (
    <div style={{ padding: '1.25rem 2rem', borderBottom: 'var(--grid-line)', background: 'rgba(255, 255, 255, 0.82)' }}>
      <LabelText as="div" style={{ marginBottom: '0.5rem' }}>LATEST DEPLOYMENT</LabelText>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'flex-start', flexWrap: 'wrap' }}>
        <div>
          <div style={{ fontSize: '1rem', fontWeight: 600 }}>
            {getLatestDeploymentTitle(latestDeployment)}
          </div>
          <div style={{ marginTop: '0.4rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth: '44rem' }}>
            {latestDeployment.readiness.detail}
          </div>
          <div style={{ marginTop: '0.6rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
            {formatAttemptTime(latestDeployment.attempt.updated_at)}
          </div>
          <DeploymentVerificationNotice
            summary={latestDeployment}
            formatAttemptTime={formatAttemptTime}
            formatVerificationLatency={formatVerificationLatency}
            maxWidth="44rem"
          />
          <DeploymentTimeline steps={latestTimeline} />
          {latestRemediation && (
            <div style={{ marginTop: '0.85rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth: '44rem' }}>
              {latestRemediation.detail}
            </div>
          )}
        </div>
        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', alignItems: 'center' }}>
          <span className={`badge ${latestDeployment.readiness.tone ? `status-${latestDeployment.readiness.tone}` : ''}`}>{latestDeployment.readiness.label}</span>
          {latestDeployment.instance && instanceStatusLabel(latestDeployment.instance.status).toUpperCase() !== latestDeployment.readiness.label && (
            <span className="badge">{instanceStatusLabel(latestDeployment.instance.status).toUpperCase()}</span>
          )}
          {latestRemediation && (
            <ActionButton
              disabled={latestRemediation.action === 'verify_inference' && verifyingAttemptID === latestDeployment.attempt.id}
              onClick={() => onRemediation(latestDeployment, latestRemediation)}
            >
              {latestRemediation.action === 'verify_inference' && verifyingAttemptID === latestDeployment.attempt.id
                ? 'VERIFYING...'
                : latestRemediation.label}
            </ActionButton>
          )}
          {latestDeployment.inferenceVerified && <span className="badge">INFERENCE VERIFIED</span>}
          {latestDeployment.attempt.inference_verification?.status === 'failed' && (
            <span className="badge status-error">INFERENCE FAILED</span>
          )}
          {!latestDeployment.attempt.inference_verification && latestDeployment.autoVerificationRequested && (
            <span className="badge status-warning">
              {verifyingAttemptID === latestDeployment.attempt.id ? 'AUTO VERIFYING' : 'AUTO VERIFY QUEUED'}
            </span>
          )}
          {latestDeployment.retryable && latestRemediation?.action !== 'retry_config' && (
            <ActionButton onClick={() => onRetry(latestDeployment.attempt)}>
              RETRY CONFIG
            </ActionButton>
          )}
        </div>
      </div>
    </div>
  );
}

export function DeploymentHistorySection({
  deploymentHistory,
  latestAttemptID,
  verifyingAttemptID,
  onRemediation,
  onRetry,
  onNewAttempt,
  renderInstanceActions,
  formatAttemptTime,
  formatVerificationLatency,
}: DeploymentHistorySectionProps) {
  if (deploymentHistory.length === 0) return null;

  return (
    <GridRow>
      <Cell span={4}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'center', marginBottom: '1.5rem', flexWrap: 'wrap' }}>
          <div>
            <LabelText as="div" style={{ marginBottom: '0.35rem' }}>DEPLOYMENT HISTORY</LabelText>
            <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
              Recent provisioning attempts persist per workspace so you can recover the flow after refresh.
            </div>
          </div>
          <ActionButton onClick={onNewAttempt}>NEW ATTEMPT</ActionButton>
        </div>

        <div style={{ display: 'grid', gap: '0.85rem' }}>
          {deploymentHistory.slice(0, 5).map((summary) => {
            const { attempt, readiness, instance, retryable } = summary;
            const timeline = getDeploymentTimeline(summary);
            const remediation = getDeploymentRemediation(summary);

            return (
              <div
                key={attempt.id}
                style={{
                  border: 'var(--grid-line)',
                  padding: '1rem 1.1rem',
                  background: latestAttemptID === attempt.id ? 'rgba(244, 242, 238, 0.7)' : 'transparent',
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'flex-start', flexWrap: 'wrap' }}>
                  <div style={{ minWidth: 0, flex: '1 1 28rem' }}>
                    <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
                      <div style={{ fontSize: '0.95rem', fontWeight: 600 }}>
                        {getDeploymentAttemptTitle(summary)}
                      </div>
                      <span className={`badge ${readiness.tone ? `status-${readiness.tone}` : ''}`}>{readiness.label}</span>
                      {instance && instanceStatusLabel(instance.status).toUpperCase() !== readiness.label && (
                        <span className="badge">{instanceStatusLabel(instance.status).toUpperCase()}</span>
                      )}
                    </div>
                    <div style={{ marginTop: '0.45rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth: '54rem' }}>
                      {readiness.detail}
                    </div>
                    <div style={{ marginTop: '0.65rem', display: 'flex', gap: '0.5rem', flexWrap: 'wrap', color: 'var(--text-secondary)', fontSize: '0.75rem' }}>
                      <span className="badge">{formatAttemptTime(attempt.updated_at)}</span>
                      {attempt.request.provider && <span className="badge">{getProviderDisplayName(attempt.request.provider)}</span>}
                      <span className="badge">{attempt.request.gpu_count || 1}x {formatGPUDisplayName(attempt.request.gpu_type)}</span>
                      {attempt.request.spot_instance ? <span className="badge">SPOT</span> : null}
                      {attempt.request.models?.length ? <span className="badge">{attempt.request.models.length} MODEL{attempt.request.models.length === 1 ? '' : 'S'}</span> : null}
                      {summary.inferenceVerified ? <span className="badge">INFERENCE VERIFIED</span> : null}
                      {attempt.inference_verification?.status === 'failed' ? <span className="badge status-error">INFERENCE FAILED</span> : null}
                      {!attempt.inference_verification && summary.autoVerificationRequested ? (
                        <span className="badge status-warning">{verifyingAttemptID === attempt.id ? 'AUTO VERIFYING' : 'AUTO VERIFY QUEUED'}</span>
                      ) : null}
                    </div>
                    <DeploymentVerificationNotice
                      summary={summary}
                      formatAttemptTime={formatAttemptTime}
                      formatVerificationLatency={formatVerificationLatency}
                      maxWidth="54rem"
                    />
                    <DeploymentTimeline steps={timeline} />
                    {remediation && (
                      <div style={{ marginTop: '0.75rem', color: 'var(--text-secondary)', lineHeight: 1.6, maxWidth: '54rem' }}>
                        {remediation.detail}
                      </div>
                    )}
                  </div>

                  <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
                    {instance ? renderInstanceActions(instance) : null}
                    {remediation && (
                      <ActionButton
                        disabled={remediation.action === 'verify_inference' && verifyingAttemptID === attempt.id}
                        onClick={() => onRemediation(summary, remediation)}
                      >
                        {remediation.action === 'verify_inference' && verifyingAttemptID === attempt.id
                          ? 'VERIFYING...'
                          : remediation.label}
                      </ActionButton>
                    )}
                    {retryable && remediation?.action !== 'retry_config' && (
                      <ActionButton onClick={() => onRetry(attempt)}>
                        RETRY CONFIG
                      </ActionButton>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </Cell>
    </GridRow>
  );
}
