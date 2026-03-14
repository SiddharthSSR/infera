import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useWorkers, useStats, useInstances, useCosts, useModels } from '../hooks/useApi';
import { SkeletonCell } from '../components/Skeleton';
import { useAuthSession } from '../lib/auth-context';
import {
  getDeploymentRemediation,
  readDeploymentAttempts,
  summarizeDeploymentAttempt,
  type DeploymentAttemptRecord,
  type DeploymentAttemptSummary,
} from '../lib/deploymentHistory';
import { getInstanceReadiness } from '../lib/instanceReadiness';
import type { Instance, Model, Worker } from '../types';

function ChartBars({ heights, activeIndex }: { heights: number[]; activeIndex?: number }) {
  return (
    <div className="metric-chart">
      {heights.map((h, i) => (
        <div
          key={i}
          className={`chart-bar ${i === (activeIndex ?? heights.length - 1) ? 'active' : ''}`}
          style={{ height: `${h}%` }}
        />
      ))}
    </div>
  );
}

type ModelServingState = 'not_deployed' | 'runtime_pending' | 'serving_unverified' | 'serving_verified' | 'serving_failed' | 'degraded';

function deriveModelServingState(
  model: Model,
  instances: Instance[],
  workers: Worker[] | undefined,
  deploymentAttempts: DeploymentAttemptRecord[],
): ModelServingState {
  const relatedInstances = instances.filter((instance) => (instance.models || []).includes(model.id));
  const relatedAttempts = deploymentAttempts
    .filter((attempt) =>
      (attempt.request.models || []).includes(model.id)
      || attempt.inference_verification?.model === model.id,
    )
    .map((attempt) => summarizeDeploymentAttempt(attempt, instances, workers));
  const latestAttempt = relatedAttempts[0] || null;
  const readinessList = relatedInstances.map((instance) => getInstanceReadiness(instance, workers));
  const anyServing = readinessList.some((readiness) => readiness.serving);
  const allServingVerified = readinessList.length > 0 && readinessList.every((readiness) => readiness.serving && readiness.verified);
  const latestVerification = latestAttempt?.attempt.inference_verification;

  if (relatedInstances.length === 0) {
    return latestAttempt?.readiness.label === 'REQUEST FAILED' ? 'serving_failed' : 'not_deployed';
  }

  if (latestVerification?.status === 'passed' && anyServing) {
    return 'serving_verified';
  }

  if (latestVerification?.status === 'failed' && anyServing) {
    return 'serving_failed';
  }

  if (allServingVerified) {
    return 'serving_unverified';
  }

  if (anyServing || readinessList.some((readiness) => readiness.label === 'MODEL LOADING' || readiness.label === 'PARTIAL READY')) {
    return 'runtime_pending';
  }

  return 'degraded';
}

function getSummaryToneClass(tone: '' | 'warning' | 'error' | 'inactive') {
  return tone ? `status-${tone}` : '';
}

function getAttemptTone(summary: DeploymentAttemptSummary): '' | 'warning' | 'error' | 'inactive' {
  if (summary.attempt.inference_verification?.status === 'failed') return 'error';
  if (summary.readiness.tone === 'error') return 'error';
  if (summary.readiness.tone === 'warning') return 'warning';
  if (summary.readiness.tone === 'inactive') return 'inactive';
  return '';
}

function formatAttemptTime(timestamp?: string) {
  if (!timestamp) return null;
  return new Date(timestamp).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });
}

export function Dashboard() {
  const navigate = useNavigate();
  const { session } = useAuthSession();
  const workspaceID = session?.workspace?.id;
  const { data: workers, isLoading: loadingWorkers, isError: errorWorkers } = useWorkers();
  const { data: stats, isLoading: loadingStats, isError: errorStats } = useStats();
  const { data: instances, isLoading: loadingInstances } = useInstances();
  const { data: costs, isLoading: loadingCosts } = useCosts();
  const { data: models, isLoading: loadingModels } = useModels();
  const [deploymentAttempts, setDeploymentAttempts] = useState<DeploymentAttemptRecord[]>([]);
  const isLoading = loadingWorkers || loadingStats || loadingInstances || loadingCosts || loadingModels;

  useEffect(() => {
    setDeploymentAttempts(readDeploymentAttempts(workspaceID));
  }, [workspaceID]);

  const gatewayDown = !isLoading && (errorWorkers && !workers) && (errorStats && !stats);

  const activeInstances = instances?.filter(i => i.status === 'running') || [];
  const healthyWorkers = workers?.filter(w => w.status === 'healthy') || [];
  const loadedModels = models?.filter(m => m.loaded !== false) || [];
  const deploymentSummaries = useMemo(
    () => deploymentAttempts.map((attempt) => summarizeDeploymentAttempt(attempt, instances || [], workers)).slice(0, 5),
    [deploymentAttempts, instances, workers],
  );
  const modelServingStates = useMemo(
    () => (models || []).map((model) => deriveModelServingState(model, instances || [], workers, deploymentAttempts)),
    [deploymentAttempts, instances, models, workers],
  );
  const servingVerifiedCount = modelServingStates.filter((state) => state === 'serving_verified').length;
  const servingUnverifiedCount = modelServingStates.filter((state) => state === 'serving_unverified').length;
  const degradedDeploymentCount = deploymentSummaries.filter((summary) => (
    summary.attempt.inference_verification?.status === 'failed'
    || ['REQUEST FAILED', 'INSTANCE NOT FOUND', 'FAILED', 'WORKER NOT CONNECTED', 'WORKER MISSING', 'WORKER UNHEALTHY', 'WORKER DEGRADED', 'HEARTBEAT STALE'].includes(summary.readiness.label)
  )).length;
  const pendingDeploymentCount = deploymentSummaries.filter((summary) => (
    ['PROVISIONING', 'WAITING FOR WORKER', 'WORKER CONNECTING', 'MODEL LOADING', 'MODEL LOAD DELAY', 'PARTIAL READY', 'SERVING UNVERIFIED'].includes(summary.readiness.label)
  )).length;
  const latestFailure = deploymentSummaries.find((summary) => (
    summary.attempt.outcome === 'request_failed'
    || summary.attempt.inference_verification?.status === 'failed'
    || summary.readiness.tone === 'error'
  ));
  const latestVerification = deploymentSummaries.find((summary) => Boolean(summary.attempt.inference_verification));

  if (isLoading) {
    return (
      <div className="dashboard-page animate-fade-in">
        <div className="grid-row">
          <SkeletonCell />
          <SkeletonCell />
          <SkeletonCell />
          <SkeletonCell />
        </div>
      </div>
    );
  }

  if (gatewayDown) {
    return (
      <div className="dashboard-page animate-fade-in">
        <div className="grid-row">
          <div className="cell" style={{ gridColumn: 'span 4', textAlign: 'center', padding: '4rem 2rem' }}>
            <div style={{ fontSize: '2rem', fontWeight: 700, marginBottom: '1rem', letterSpacing: '-0.02em' }}>
              Gateway Unreachable
            </div>
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.95rem', maxWidth: 480, margin: '0 auto 2rem', lineHeight: 1.6 }}>
              Unable to connect to the Infera gateway. The service may be restarting or experiencing an outage.
              The dashboard will automatically reconnect when the gateway is available.
            </div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '0.5rem', fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
              <span className="status-dot inactive" />
              Retrying periodically...
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="dashboard-page animate-fade-in">
      {/* Metrics Row */}
      <div className="grid-row dashboard-metrics-row">
        <div className="cell" style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between', minHeight: 140 }}>
          <div className="label-text">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M12 2v20M2 12h20" />
            </svg>
            TOTAL REQUESTS
          </div>
          <div className="value-text">{stats?.requests?.per_second ? `${(stats.requests.per_second * 86400 / 1000).toFixed(1)}K` : '0'}</div>
          <ChartBars heights={[30, 50, 40, 80, 60, 90]} />
        </div>

        <div className="cell" style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between', minHeight: 140 }}>
          <div className="label-text">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
            </svg>
            AVG LATENCY
          </div>
          <div className="value-text">{stats?.latency?.avg_ms != null ? `${Math.round(stats.latency.avg_ms)}ms` : '-'}</div>
          <ChartBars heights={[20, 25, 22, 20, 30, 25]} activeIndex={3} />
        </div>

        <div className="cell" style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between', minHeight: 140 }}>
          <div className="label-text">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" />
            </svg>
            THROUGHPUT
          </div>
          <div className="value-text">{stats?.requests?.per_second ? `${stats.requests.per_second.toFixed(1)} r/s` : '-'}</div>
          <ChartBars heights={[40, 60, 85, 70, 60, 55]} activeIndex={2} />
        </div>

        <div className="cell" style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between', minHeight: 140 }}>
          <div className="label-text">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
              <line x1="8" y1="21" x2="16" y2="21" /><line x1="12" y1="17" x2="12" y2="21" />
            </svg>
            ACTIVE NODES
          </div>
          <div className="value-text">{healthyWorkers.length} / {workers?.length || 0}</div>
          <div style={{ marginTop: 'auto', paddingTop: '1rem' }}>
            <span className="status-dot" />{' '}
            <span style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', marginLeft: '0.5rem' }}>
              {healthyWorkers.length > 0 ? 'All systems operational' : 'No workers online'}
            </span>
          </div>
        </div>
      </div>

      <div className="grid-row dashboard-serving-row">
        <div className="cell dashboard-serving-cell">
          <div className="label-text">SERVING VERIFIED</div>
          <div className="value-text" style={{ marginTop: '0.85rem' }}>{servingVerifiedCount}</div>
          <div className="dashboard-summary-text">Models that answered a real verification request successfully.</div>
          <button className="action-btn" style={{ marginTop: '1rem' }} onClick={() => navigate('/models')}>OPEN MODELS</button>
        </div>
        <div className="cell dashboard-serving-cell">
          <div className="label-text">VERIFY PENDING</div>
          <div className="value-text" style={{ marginTop: '0.85rem' }}>{servingUnverifiedCount}</div>
          <div className="dashboard-summary-text">Models that look runtime-ready but still need or are awaiting inference verification.</div>
          <button className="action-btn" style={{ marginTop: '1rem' }} onClick={() => navigate('/models')}>VERIFY SERVING</button>
        </div>
        <div className="cell dashboard-serving-cell">
          <div className="label-text">DEGRADED DEPLOYMENTS</div>
          <div className="value-text" style={{ marginTop: '0.85rem' }}>{degradedDeploymentCount}</div>
          <div className="dashboard-summary-text">Recent attempts that failed, lost their node, or are serving with an explicit error signal.</div>
          <button className="action-btn" style={{ marginTop: '1rem' }} onClick={() => navigate('/instances')}>VIEW FAILED DEPLOYMENTS</button>
        </div>
        <div className="cell dashboard-serving-cell">
          <div className="label-text">PENDING DEPLOYMENTS</div>
          <div className="value-text" style={{ marginTop: '0.85rem' }}>{pendingDeploymentCount}</div>
          <div className="dashboard-summary-text">Nodes still provisioning, connecting a worker, or loading assigned models.</div>
          <button className="action-btn" style={{ marginTop: '1rem' }} onClick={() => navigate('/instances')}>OPEN CLUSTERS</button>
        </div>
      </div>

      {/* Main Content Row */}
      <div className="grid-row dashboard-main-row" style={{ flexGrow: 1 }}>
        {/* Deployed Models */}
        <div className="cell dashboard-models-cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text" style={{ marginBottom: '2rem' }}>DEPLOYED MODELS</div>

          {loadedModels.length > 0 ? (
            loadedModels.slice(0, 3).map((model) => (
              <div key={model.id} style={{ marginBottom: '3rem' }}>
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
                    {model.tags.map(tag => (
                      <span key={tag} className="tag">{tag}</span>
                    ))}
                  </div>
                )}
              </div>
            ))
          ) : (
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
              No models deployed yet. Provision an instance to get started.
            </div>
          )}

          <button className="action-btn" style={{ marginTop: '1.5rem' }} onClick={() => navigate('/models')}>DEPLOY NEW MODEL</button>
        </div>

        {/* Right Panel */}
        <div className="cell dashboard-overview-cell" style={{ gridColumn: 'span 2', backgroundColor: 'var(--bg-accent)' }}>
          <div style={{ marginBottom: '3rem' }}>
            <div className="label-text">CLUSTER OVERVIEW</div>
            <h3 style={{ fontSize: '1.25rem', marginTop: '1rem', marginBottom: '1.5rem', fontWeight: 500 }}>
              Resource utilization
            </h3>

            <div style={{ display: 'flex', flexDirection: 'column' }}>
              <div className="cluster-metric-row" style={{ display: 'grid', gridTemplateColumns: '2fr 1fr 1fr', padding: '1rem 0', borderBottom: '1px solid #EEEEEC', alignItems: 'center' }}>
                <div style={{ fontSize: '0.9rem' }}>Active Instances</div>
                <div className="mono">{activeInstances.length}</div>
                <div style={{ textAlign: 'right', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                  {instances?.length || 0} total
                </div>
              </div>
              <div className="cluster-metric-row" style={{ display: 'grid', gridTemplateColumns: '2fr 1fr 1fr', padding: '1rem 0', borderBottom: '1px solid #EEEEEC', alignItems: 'center' }}>
                <div style={{ fontSize: '0.9rem' }}>Cost / Hour</div>
                <div className="mono">${costs?.current_hourly?.toFixed(2) || '0.00'}</div>
                <div style={{ textAlign: 'right', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                  ${costs?.today_total?.toFixed(2) || '0.00'} today
                </div>
              </div>
              <div className="cluster-metric-row" style={{ display: 'grid', gridTemplateColumns: '2fr 1fr 1fr', padding: '1rem 0', borderBottom: '1px solid #EEEEEC', alignItems: 'center' }}>
                <div style={{ fontSize: '0.9rem' }}>Queue Depth</div>
                <div className="mono">{stats?.requests?.queue_depth || 0}</div>
                <div style={{ textAlign: 'right', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>pending</div>
              </div>
              <div className="cluster-metric-row" style={{ display: 'grid', gridTemplateColumns: '2fr 1fr 1fr', padding: '1rem 0', borderBottom: '1px solid #EEEEEC', alignItems: 'center' }}>
                <div style={{ fontSize: '0.9rem' }}>Avg GPU Util</div>
                <div className="mono">{stats?.gpu?.avg_utilization != null ? `${Math.round(stats.gpu.avg_utilization)}%` : '-'}</div>
                <div style={{ textAlign: 'right', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>across workers</div>
              </div>
              <div className="cluster-metric-row" style={{ display: 'grid', gridTemplateColumns: '2fr 1fr 1fr', padding: '1rem 0', borderBottom: '1px solid #EEEEEC', alignItems: 'center' }}>
                <div style={{ fontSize: '0.9rem' }}>Memory Usage</div>
                <div className="mono">{stats?.memory?.total_bytes ? `${((stats.memory.used_bytes / stats.memory.total_bytes) * 100).toFixed(0)}%` : '-'}</div>
                <div style={{ textAlign: 'right', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                  {stats?.memory?.total_bytes ? `${(stats.memory.used_bytes / (1024 ** 3)).toFixed(1)} / ${(stats.memory.total_bytes / (1024 ** 3)).toFixed(1)} GB` : '-'}
                </div>
              </div>
            </div>
          </div>

          <div style={{ marginBottom: '2.25rem' }}>
            <div className="label-text" style={{ marginBottom: '1rem' }}>RECENT DEPLOYMENT ACTIVITY</div>
            {deploymentSummaries.length > 0 ? (
              <div className="dashboard-activity-list">
                {deploymentSummaries.slice(0, 4).map((summary) => {
                  const remediation = getDeploymentRemediation(summary);
                  const toneClass = getSummaryToneClass(getAttemptTone(summary));
                  return (
                    <div key={summary.attempt.id} className="dashboard-activity-item">
                      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '1rem' }}>
                        <div>
                          <div style={{ fontSize: '0.88rem', fontWeight: 500 }}>
                            {summary.attempt.selected_model_name || summary.attempt.request.models?.[0]?.split('/').pop() || summary.instance?.name || 'Deployment attempt'}
                          </div>
                          <div style={{ marginTop: '0.35rem', fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
                            {summary.readiness.detail}
                          </div>
                        </div>
                        <span className={`badge ${toneClass}`}>{summary.readiness.label}</span>
                      </div>
                      <div className="dashboard-activity-meta">
                        <span>{summary.instance?.provider?.toUpperCase() || 'REQUEST'}</span>
                        <span>{formatAttemptTime(summary.attempt.updated_at || summary.attempt.created_at)}</span>
                        {summary.attempt.inference_verification?.status === 'passed' && (
                          <span>{summary.attempt.inference_verification.latency_ms != null ? `${summary.attempt.inference_verification.latency_ms}ms` : 'verified'}</span>
                        )}
                        {summary.attempt.inference_verification?.status === 'failed' && (
                          <span>verification failed</span>
                        )}
                      </div>
                      {remediation && (
                        <button
                          className="action-btn"
                          style={{ marginTop: '0.85rem' }}
                          onClick={() => {
                            if (remediation.action === 'open_workspace') navigate('/workspace');
                            else if (remediation.action === 'verify_inference') navigate('/models');
                            else navigate('/instances');
                          }}
                        >
                          {remediation.label}
                        </button>
                      )}
                    </div>
                  );
                })}
              </div>
            ) : (
              <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
                No recent deployment activity yet. Provision capacity from Clusters to start tracking deployment health here.
              </div>
            )}
          </div>

          <div className="dashboard-quick-actions">
            <button className="action-btn" onClick={() => navigate('/instances')}>OPEN CLUSTERS</button>
            <button className="action-btn" onClick={() => navigate('/models')}>OPEN MODELS</button>
            {latestFailure && (
              <button className="action-btn" onClick={() => navigate('/instances')}>VIEW FAILED DEPLOYMENTS</button>
            )}
            {latestVerification && (
              <button className="action-btn" onClick={() => navigate('/models')}>VERIFY SERVING</button>
            )}
          </div>

          {/* Recent Workers */}
          <div style={{ marginTop: '2.25rem' }}>
            <div className="label-text" style={{ marginBottom: '1.5rem' }}>WORKER STATUS</div>
            <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.6 }}>
              {healthyWorkers.length > 0 ? (
                healthyWorkers.slice(0, 4).map(worker => (
                  <div className="worker-status-row" key={worker.worker_id} style={{ borderBottom: '1px solid #F0F0F0', padding: '0.5rem 0', display: 'flex', gap: '1rem' }}>
                    <span style={{ color: 'var(--text-primary)', minWidth: 80 }}>
                      {worker.worker_id.slice(0, 8)}
                    </span>
                    <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      <span className="status-dot" style={{ width: 6, height: 6 }} />
                      GPU {worker.gpu_utilization}%
                    </span>
                    <span>{worker.models?.[0]?.split('/').pop() || '-'}</span>
                  </div>
                ))
              ) : (
                <div style={{ padding: '0.5rem 0' }}>No workers connected.</div>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Footer Row */}
      <div className="grid-row dashboard-footer-row">
        <div className="cell">
          <div className="label-text">VERSION</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>v1.0.0</div>
        </div>
        <div className="cell">
          <div className="label-text">UPTIME</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {stats?.uptime_seconds ? `${Math.floor(stats.uptime_seconds / 3600)}h ${Math.floor((stats.uptime_seconds % 3600) / 60)}m` : '-'}
          </div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text">SYSTEM STATUS</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.85rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span className={`status-dot ${healthyWorkers.length > 0 ? '' : 'inactive'}`} />
            {healthyWorkers.length > 0
              ? 'All endpoints are performing within latency targets.'
              : 'No active workers. Provision an instance to start serving.'}
          </div>
        </div>
      </div>
    </div>
  );
}
