import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useWorkers, useStats, useInstances, useCosts, useModels, useProviders } from '../hooks/useApi';
import { SkeletonCell } from '../components/Skeleton';
import { useAuthSession } from '../lib/auth-context';
import {
  fetchAuditUsage,
  fetchWorkspaceQuota,
  type AuditUsageRow,
  type WorkspaceQuotaRecord,
} from '../lib/api';
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
type AttentionSeverity = 'critical' | 'warning' | 'info';
type AttentionAction = 'open_clusters' | 'open_models' | 'open_workspace' | 'verify_now';

type AttentionItem = {
  id: string;
  severity: AttentionSeverity;
  title: string;
  detail: string;
  actionLabel: string;
  action: AttentionAction;
  timestamp?: string;
};

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

function getAttentionSeverityClass(severity: AttentionSeverity) {
  switch (severity) {
    case 'critical':
      return 'dashboard-alert-critical';
    case 'warning':
      return 'dashboard-alert-warning';
    default:
      return 'dashboard-alert-info';
  }
}

function mapRemediationAction(action: 'open_workspace' | 'view_capacity' | 'retry_config' | 'focus_instance' | 'verify_inference' | undefined): AttentionAction {
  switch (action) {
    case 'open_workspace':
      return 'open_workspace';
    case 'verify_inference':
      return 'verify_now';
    default:
      return 'open_clusters';
  }
}

function monthRange() {
  const now = new Date();
  const start = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1, 0, 0, 0, 0));
  return { start: start.toISOString(), end: now.toISOString() };
}

function usageRatio(used: number, limit?: number | null): number {
  if (limit == null) return 0;
  if (limit <= 0) return used > 0 ? Number.POSITIVE_INFINITY : 1;
  return used / limit;
}

function formatCount(value: number): string {
  return new Intl.NumberFormat('en-US').format(value);
}

function formatCompactCount(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return String(value);
}

function buildOperationalAttentionQueue(
  deploymentSummaries: DeploymentAttemptSummary[],
  connectedProviders: number,
  visibleProviders: number,
  workers: Worker[] | undefined,
  activeInstances: Instance[],
  servingUnverifiedCount: number,
): AttentionItem[] {
  const items: AttentionItem[] = [];

  if (visibleProviders > 0 && connectedProviders === 0) {
    items.push({
      id: 'provider-disconnected',
      severity: 'critical',
      title: 'No live provider connection',
      detail: 'Workspace providers are configured but none are currently connected. New deployments will fail until provider access is restored.',
      actionLabel: 'OPEN WORKSPACE',
      action: 'open_workspace',
    });
  }

  if ((workers?.length || 0) === 0 && activeInstances.length > 0) {
    items.push({
      id: 'workers-offline',
      severity: 'critical',
      title: 'Workers are not connected',
      detail: 'Compute nodes exist, but no worker is currently reporting healthy runtime state back to the gateway.',
      actionLabel: 'OPEN CLUSTERS',
      action: 'open_clusters',
    });
  }

  const latestFailure = deploymentSummaries.find((summary) => (
    summary.attempt.outcome === 'request_failed'
    || summary.attempt.inference_verification?.status === 'failed'
    || summary.readiness.tone === 'error'
  ));
  if (latestFailure) {
    const remediation = getDeploymentRemediation(latestFailure);
    items.push({
      id: `failure-${latestFailure.attempt.id}`,
      severity: 'critical',
      title: latestFailure.attempt.inference_verification?.status === 'failed'
        ? 'Inference verification failed'
        : latestFailure.attempt.outcome === 'request_failed'
          ? 'Latest deployment request failed'
          : 'Deployment needs intervention',
      detail: latestFailure.attempt.inference_verification?.error || latestFailure.readiness.detail,
      actionLabel: remediation?.label || 'OPEN CLUSTERS',
      action: mapRemediationAction(remediation?.action),
      timestamp: latestFailure.attempt.updated_at || latestFailure.attempt.created_at,
    });
  }

  const now = Date.now();
  const stuckPending = deploymentSummaries.find((summary) => {
    if (!['PROVISIONING', 'WAITING FOR WORKER', 'WORKER CONNECTING', 'MODEL LOADING', 'MODEL LOAD DELAY', 'PARTIAL READY', 'SERVING UNVERIFIED'].includes(summary.readiness.label)) {
      return false;
    }
    const age = now - Date.parse(summary.attempt.updated_at || summary.attempt.created_at);
    return age > 15 * 60 * 1000;
  });
  if (stuckPending) {
    items.push({
      id: `stuck-${stuckPending.attempt.id}`,
      severity: 'warning',
      title: 'Deployment appears stuck',
      detail: `${stuckPending.readiness.detail} This attempt has been pending longer than expected.`,
      actionLabel: 'OPEN CLUSTERS',
      action: 'open_clusters',
      timestamp: stuckPending.attempt.updated_at || stuckPending.attempt.created_at,
    });
  }

  if (servingUnverifiedCount > 0) {
    items.push({
      id: 'verify-pending',
      severity: 'info',
      title: 'Serving verification still pending',
      detail: `${servingUnverifiedCount} model${servingUnverifiedCount === 1 ? '' : 's'} look runtime-ready but still need a clean inference verification result.`,
      actionLabel: 'VERIFY NOW',
      action: 'verify_now',
    });
  }

  return items;
}

function buildBillingAttentionQueue(
  quota: WorkspaceQuotaRecord | null,
  usageRows: AuditUsageRow[],
  costs: { current_hourly: number; today_total: number; by_provider: Record<string, number> } | undefined,
): AttentionItem[] {
  if (!quota && usageRows.length === 0 && !costs) return [];

  const usageSummary = usageRows.reduce((acc, row) => {
    acc.requests += row.requests;
    acc.tokens += row.tokens;
    return acc;
  }, { requests: 0, tokens: 0 });

  const requestUsageRatio = usageRatio(usageSummary.requests, quota?.monthly_request_limit);
  const tokenUsageRatio = usageRatio(usageSummary.tokens, quota?.monthly_token_limit);
  const quotaPressure = Math.max(requestUsageRatio, tokenUsageRatio);
  const dominantMetric = tokenUsageRatio >= requestUsageRatio ? 'token' : 'request';
  const items: AttentionItem[] = [];

  if (quota && quotaPressure >= 1) {
    const limit = dominantMetric === 'token' ? quota.monthly_token_limit : quota.monthly_request_limit;
    const used = dominantMetric === 'token' ? usageSummary.tokens : usageSummary.requests;
    items.push({
      id: 'quota-exceeded',
      severity: 'critical',
      title: 'Workspace quota exceeded',
      detail: `${dominantMetric === 'token' ? 'Token' : 'Request'} usage is at ${Number.isFinite(quotaPressure) ? `${Math.round(quotaPressure * 100)}%` : '100%+'} of the configured limit (${formatCount(used)} / ${limit != null ? formatCount(limit) : '0'}).`,
      actionLabel: 'ADJUST QUOTA',
      action: 'open_workspace',
    });
  } else if (quota && quotaPressure >= 0.8) {
    items.push({
      id: 'quota-near-limit',
      severity: 'warning',
      title: 'Workspace quota nearing limit',
      detail: `${dominantMetric === 'token' ? 'Token' : 'Request'} usage is at ${Math.round(quotaPressure * 100)}% of the monthly quota. Review limits before traffic is blocked.`,
      actionLabel: 'OPEN WORKSPACE',
      action: 'open_workspace',
    });
  }

  if (costs && costs.current_hourly > 0 && costs.today_total > 5) {
    const projectedDayFromCurrentHour = costs.current_hourly * 24;
    if (projectedDayFromCurrentHour >= costs.today_total * 2.25) {
      items.push({
        id: 'cost-burn-spike',
        severity: projectedDayFromCurrentHour >= costs.today_total * 3 ? 'critical' : 'warning',
        title: 'Current cost burn is elevated',
        detail: `At the current pace, hourly spend projects to about $${projectedDayFromCurrentHour.toFixed(2)} for the day versus $${costs.today_total.toFixed(2)} spent so far.`,
        actionLabel: 'OPEN CLUSTERS',
        action: 'open_clusters',
      });
    }
  }

  if (costs && costs.today_total > 10) {
    const providerEntries = Object.entries(costs.by_provider || {}).sort(([, left], [, right]) => right - left);
    const topProvider = providerEntries[0];
    if (topProvider) {
      const share = topProvider[1] / costs.today_total;
      if (share >= 0.85) {
        items.push({
          id: 'provider-cost-concentration',
          severity: 'info',
          title: 'Spend is concentrated on one provider',
          detail: `${topProvider[0]} accounts for ${Math.round(share * 100)}% of today’s spend. Check whether that concentration is intentional.`,
          actionLabel: 'OPEN CLUSTERS',
          action: 'open_clusters',
        });
      }
    }
  }

  return items;
}

export function Dashboard() {
  const navigate = useNavigate();
  const { session } = useAuthSession();
  const workspaceID = session?.workspace?.id;
  const role = session?.key?.role ?? 'user';
  const canViewQuota = role === 'owner' || role === 'admin' || role === 'billing' || role === 'read_only';
  const canViewUsage = canViewQuota;
  const { data: workers, isLoading: loadingWorkers, isError: errorWorkers } = useWorkers();
  const { data: stats, isLoading: loadingStats, isError: errorStats } = useStats();
  const { data: instances, isLoading: loadingInstances } = useInstances();
  const { data: costs, isLoading: loadingCosts } = useCosts();
  const { data: models, isLoading: loadingModels } = useModels();
  const { data: providers, isLoading: loadingProviders } = useProviders();
  const [deploymentAttempts, setDeploymentAttempts] = useState<DeploymentAttemptRecord[]>([]);
  const [quota, setQuota] = useState<WorkspaceQuotaRecord | null>(null);
  const [usageRows, setUsageRows] = useState<AuditUsageRow[]>([]);
  const isLoading = loadingWorkers || loadingStats || loadingInstances || loadingCosts || loadingModels || loadingProviders;

  useEffect(() => {
    setDeploymentAttempts(readDeploymentAttempts(workspaceID));
  }, [workspaceID]);

  useEffect(() => {
    let cancelled = false;
    if (!workspaceID) {
      setQuota(null);
      setUsageRows([]);
      return () => { cancelled = true; };
    }

    if (canViewQuota) {
      fetchWorkspaceQuota(workspaceID)
        .then((nextQuota) => {
          if (!cancelled) setQuota(nextQuota);
        })
        .catch(() => {
          if (!cancelled) setQuota(null);
        });
    } else {
      setQuota(null);
    }

    if (canViewUsage) {
      const { start, end } = monthRange();
      fetchAuditUsage({ start, end, bucket: 'day', workspace_id: workspaceID })
        .then((usage) => {
          if (!cancelled) setUsageRows(usage.rows);
        })
        .catch(() => {
          if (!cancelled) setUsageRows([]);
        });
    } else {
      setUsageRows([]);
    }

    return () => {
      cancelled = true;
    };
  }, [canViewQuota, canViewUsage, workspaceID]);

  const gatewayDown = !isLoading && (errorWorkers && !workers) && (errorStats && !stats);

  const activeInstances = instances?.filter(i => i.status === 'running') || [];
  const healthyWorkers = workers?.filter(w => w.status === 'healthy') || [];
  const loadedModels = models?.filter(m => m.loaded !== false) || [];
  const visibleProviders = useMemo(
    () => (providers || []).filter((provider) => provider.provider !== 'mock' && provider.provider !== 'lambda'),
    [providers],
  );
  const connectedProviders = visibleProviders.filter((provider) => provider.connected);
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
  const billingAttention = useMemo(
    () => buildBillingAttentionQueue(quota, usageRows, costs),
    [costs, quota, usageRows],
  );
  const attentionQueue = useMemo(
    () => [...buildOperationalAttentionQueue(deploymentSummaries, connectedProviders.length, visibleProviders.length, workers, activeInstances, servingUnverifiedCount), ...billingAttention].slice(0, 6),
    [activeInstances, billingAttention, connectedProviders.length, deploymentSummaries, servingUnverifiedCount, visibleProviders.length, workers],
  );
  const deploymentTrend = useMemo(() => {
    const recent = deploymentSummaries.slice(0, 6);
    return {
      recent,
      failed: recent.filter((summary) => (
        summary.attempt.outcome === 'request_failed'
        || summary.attempt.inference_verification?.status === 'failed'
        || summary.readiness.tone === 'error'
      )).length,
      pending: recent.filter((summary) => (
        ['PROVISIONING', 'WAITING FOR WORKER', 'WORKER CONNECTING', 'MODEL LOADING', 'MODEL LOAD DELAY', 'PARTIAL READY', 'SERVING UNVERIFIED'].includes(summary.readiness.label)
      )).length,
      stable: recent.filter((summary) => (
        summary.readiness.label === 'SERVING VERIFIED' || summary.attempt.inference_verification?.status === 'passed'
      )).length,
    };
  }, [deploymentSummaries]);
  const verificationTrend = useMemo(
    () => deploymentSummaries
      .filter((summary) => Boolean(summary.attempt.inference_verification))
      .slice(0, 4),
    [deploymentSummaries],
  );
  const usageTrend = useMemo(() => {
    const byDay = new Map<string, { requests: number; tokens: number }>();
    for (const row of usageRows) {
      const day = row.bucket_start.slice(0, 10);
      const totals = byDay.get(day) || { requests: 0, tokens: 0 };
      totals.requests += row.requests;
      totals.tokens += row.tokens;
      byDay.set(day, totals);
    }

    return Array.from(byDay.entries())
      .sort(([left], [right]) => left.localeCompare(right))
      .slice(-7)
      .map(([day, totals]) => ({
        day,
        requests: totals.requests,
        tokens: totals.tokens,
      }));
  }, [usageRows]);
  const usageTrendMaxRequests = useMemo(
    () => usageTrend.reduce((max, entry) => Math.max(max, entry.requests), 0),
    [usageTrend],
  );

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
      <div className="grid-row">
        <div className="cell" style={{ gridColumn: 'span 4' }}>
          <div className="help-callout">
            <div className="label-text">HOW TO READ THIS DASHBOARD</div>
            <div className="help-callout-copy">
              <strong>Serving verified</strong> means runtime state looks healthy and the latest worker heartbeat is fresh. <strong>Inference verified</strong> means a real chat-completions request succeeded. Use the attention queue for what needs action now, then use recent deployment activity to see what changed most recently.
            </div>
          </div>
        </div>
      </div>

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

      <div className="grid-row dashboard-alerts-row">
        <div className="cell dashboard-alerts-cell" style={{ gridColumn: 'span 4' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '1rem', marginBottom: '1.4rem' }}>
            <div>
              <div className="label-text">ATTENTION QUEUE</div>
              <div className="dashboard-summary-text" style={{ marginTop: '0.45rem' }}>
                The next operational issues that need action now.
              </div>
            </div>
            <div className="badge status-inactive">{attentionQueue.length} OPEN</div>
          </div>

          {attentionQueue.length > 0 ? (
            <div className="dashboard-alert-list">
              {attentionQueue.map((item) => (
                <div key={item.id} className={`dashboard-alert-item ${getAttentionSeverityClass(item.severity)}`}>
                  <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '1rem' }}>
                    <div>
                      <div className="dashboard-alert-title-row">
                        <span className={`badge ${item.severity === 'critical' ? 'status-error' : item.severity === 'warning' ? 'status-warning' : 'status-inactive'}`}>
                          {item.severity.toUpperCase()}
                        </span>
                        <span style={{ fontSize: '0.95rem', fontWeight: 500 }}>{item.title}</span>
                      </div>
                      <div className="dashboard-summary-text">{item.detail}</div>
                    </div>
                    {item.timestamp && (
                      <div style={{ fontSize: '0.74rem', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-secondary)' }}>
                        {formatAttemptTime(item.timestamp)}
                      </div>
                    )}
                  </div>
                  <button
                    className="action-btn"
                    style={{ marginTop: '0.95rem' }}
                    onClick={() => {
                      if (item.action === 'open_workspace') navigate('/workspace');
                      else if (item.action === 'open_models' || item.action === 'verify_now') navigate('/models');
                      else navigate('/instances');
                    }}
                  >
                    {item.actionLabel}
                  </button>
                </div>
              ))}
            </div>
          ) : (
            <div style={{ fontSize: '0.88rem', color: 'var(--text-secondary)' }}>
              No urgent operational issues are currently queued. The serving, quota, and spend loop look stable right now.
            </div>
          )}
        </div>
      </div>

      <div className="grid-row dashboard-trends-row">
        <div className="cell dashboard-trend-cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text" style={{ marginBottom: '1rem' }}>DEPLOYMENT HISTORY</div>
          <div className="dashboard-trend-summary">
            <span>{deploymentTrend.stable} stable</span>
            <span>{deploymentTrend.pending} pending</span>
            <span>{deploymentTrend.failed} failed</span>
          </div>
          {deploymentTrend.recent.length > 0 ? (
            <div className="dashboard-trend-list">
              {deploymentTrend.recent.map((summary) => (
                <div key={summary.attempt.id} className="dashboard-trend-item">
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '1rem' }}>
                    <div style={{ fontSize: '0.88rem', fontWeight: 500 }}>
                      {summary.attempt.selected_model_name || summary.attempt.request.models?.[0]?.split('/').pop() || summary.instance?.name || 'Deployment attempt'}
                    </div>
                    <span className={`badge ${getSummaryToneClass(getAttemptTone(summary))}`}>{summary.readiness.label}</span>
                  </div>
                  <div className="dashboard-summary-text" style={{ marginTop: '0.35rem' }}>{summary.readiness.detail}</div>
                </div>
              ))}
            </div>
          ) : (
            <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
              No deployment attempts recorded yet.
            </div>
          )}
        </div>

        <div className="cell dashboard-trend-cell">
          <div className="label-text" style={{ marginBottom: '1rem' }}>VERIFICATION HISTORY</div>
          {verificationTrend.length > 0 ? (
            <div className="dashboard-trend-list">
              {verificationTrend.map((summary) => (
                <div key={summary.attempt.id} className="dashboard-trend-item">
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.75rem' }}>
                    <span className={`badge ${summary.attempt.inference_verification?.status === 'failed' ? 'status-error' : ''}`}>
                      {summary.attempt.inference_verification?.status === 'failed' ? 'FAILED' : 'PASSED'}
                    </span>
                    <span style={{ fontSize: '0.72rem', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-secondary)' }}>
                      {formatAttemptTime(summary.attempt.inference_verification?.verified_at)}
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
            </div>
          )}
        </div>

        <div className="cell dashboard-trend-cell">
          <div className="label-text" style={{ marginBottom: '1rem' }}>USAGE TRAJECTORY</div>
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
            </div>
          )}
        </div>
      </div>

      <div className="grid-row dashboard-main-row" style={{ flexGrow: 1 }}>
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
            {billingAttention.length > 0 && (
              <button className="action-btn" onClick={() => navigate('/workspace')}>VIEW USAGE</button>
            )}
          </div>

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
