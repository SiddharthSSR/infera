import { useMemo } from 'react';

import type { QuickConfigField } from '../components/dashboard/QuickConfigurationPanel';
import type { ApiKeyRecord, WorkspaceInvitationRecord, WorkspaceQuotaRecord } from '../types';
import type { AuditUsageRow } from '../lib/apiCore';
import {
  getDeploymentRemediation,
  selectPrimaryDeploymentSummary,
  summarizeDeploymentAttempt,
  type DeploymentAttemptRecord,
  type DeploymentAttemptSummary,
} from '../lib/deploymentHistory';
import { formatCount, usageRatio } from '../lib/formatting';
import { getInstanceReadiness } from '../lib/instanceReadiness';
import { buildLiveWorkspaceOperations } from '../lib/liveWorkspaceOperations';
import { buildFirstWorkspaceChecklist } from '../lib/onboardingChecklist';
import { isInventoryProviderType } from '../lib/providerInventory';
import { buildWorkspaceMaturity } from '../lib/workspaceMaturity';
import type { Instance, Model, ProviderStatus, Stats, Worker } from '../types';

type ModelServingState = 'not_deployed' | 'runtime_pending' | 'serving_unverified' | 'serving_verified' | 'serving_failed' | 'degraded';
type AttentionAction = 'open_clusters' | 'open_models' | 'open_workspace' | 'verify_now';

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
  if (latestVerification?.status === 'passed' && anyServing) return 'serving_verified';
  if (latestVerification?.status === 'failed' && anyServing) return 'serving_failed';
  if (allServingVerified) return 'serving_unverified';
  if (anyServing || readinessList.some((readiness) => readiness.label === 'MODEL LOADING' || readiness.label === 'PARTIAL READY')) {
    return 'runtime_pending';
  }
  return 'degraded';
}

function mapRemediationAction(action: 'open_workspace' | 'view_capacity' | 'retry_config' | 'focus_instance' | 'verify_inference' | undefined): AttentionAction {
  switch (action) {
    case 'open_workspace': return 'open_workspace';
    case 'verify_inference': return 'verify_now';
    default: return 'open_clusters';
  }
}

function buildOperationalAttentionQueue(
  deploymentSummaries: DeploymentAttemptSummary[],
  connectedProviders: number,
  visibleProviders: number,
  workers: Worker[] | undefined,
  activeInstances: Instance[],
  servingUnverifiedCount: number,
) {
  const items: Array<{
    id: string;
    severity: 'critical' | 'warning' | 'info';
    title: string;
    detail: string;
    actionLabel: string;
    action: AttentionAction;
    timestamp?: string;
  }> = [];
  const primaryDeployment = selectPrimaryDeploymentSummary(deploymentSummaries);
  const primaryDeploymentServing = Boolean(
    primaryDeployment && (
      primaryDeployment.inferenceVerified
      || primaryDeployment.readiness.serving
      || primaryDeployment.readiness.label === 'SERVING VERIFIED'
    ),
  );

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
      actionLabel: 'OPEN NODES',
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
    const failureIsSecondaryToHealthyServing = Boolean(
      primaryDeploymentServing
      && primaryDeployment
      && primaryDeployment.attempt.id !== latestFailure.attempt.id,
    );
    items.push({
      id: `failure-${latestFailure.attempt.id}`,
      severity: failureIsSecondaryToHealthyServing ? 'info' : 'critical',
      title: latestFailure.attempt.inference_verification?.status === 'failed'
        ? failureIsSecondaryToHealthyServing
          ? 'Recent deployment retry failed verification'
          : 'Inference verification failed'
        : latestFailure.attempt.outcome === 'request_failed'
          ? failureIsSecondaryToHealthyServing
            ? 'Recent deployment retry failed'
            : 'Latest deployment request failed'
          : failureIsSecondaryToHealthyServing
            ? 'Recent deployment retry needs review'
            : 'Deployment needs intervention',
      detail: failureIsSecondaryToHealthyServing
        ? `${latestFailure.attempt.inference_verification?.error || latestFailure.readiness.detail} Live serving is still available from the current deployment.`
        : latestFailure.attempt.inference_verification?.error || latestFailure.readiness.detail,
      actionLabel: remediation?.label || 'OPEN NODES',
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
      actionLabel: 'OPEN NODES',
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
) {
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
  const items: Array<{
    id: string;
    severity: 'critical' | 'warning' | 'info';
    title: string;
    detail: string;
    actionLabel: string;
    action: 'open_clusters' | 'open_models' | 'open_workspace' | 'verify_now';
  }> = [];

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
        actionLabel: 'OPEN NODES',
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
          detail: `${topProvider[0]} accounts for ${Math.round(share * 100)}% of today's spend. Check whether that concentration is intentional.`,
          actionLabel: 'OPEN NODES',
          action: 'open_clusters',
        });
      }
    }
  }

  return items;
}

export function useDashboardViewState({
  isLoading,
  errorWorkers,
  workers,
  errorStats,
  stats,
  instances,
  costs,
  models,
  providers,
  deploymentAttempts,
  quota,
  usageRows,
  workspaceInvites,
  workspaceServiceAccounts,
}: {
  isLoading: boolean;
  errorWorkers: boolean;
  workers: Worker[] | undefined;
  errorStats: boolean;
  stats: Stats | undefined;
  instances: Instance[] | undefined;
  costs: { current_hourly: number; today_total: number; by_provider: Record<string, number> } | undefined;
  models: Model[] | undefined;
  providers: ProviderStatus[] | undefined;
  deploymentAttempts: DeploymentAttemptRecord[];
  quota: WorkspaceQuotaRecord | null;
  usageRows: AuditUsageRow[];
  workspaceInvites: WorkspaceInvitationRecord[];
  workspaceServiceAccounts: ApiKeyRecord[];
}) {
  return useMemo(() => {
    const gatewayDown = !isLoading && errorWorkers && !workers && errorStats && !stats;
    const activeInstances = instances?.filter((instance) => instance.status === 'running') || [];
    const healthyWorkers = workers?.filter((worker) => worker.status === 'healthy') || [];
    const loadedModels = models?.filter((model) => model.loaded !== false) || [];
    const visibleProviders = (providers || []).filter((provider) => isInventoryProviderType(provider.provider));
    const connectedProviders = visibleProviders.filter((provider) => provider.connected);
    const deploymentSummaries = deploymentAttempts.map((attempt) => summarizeDeploymentAttempt(attempt, instances || [], workers)).slice(0, 5);
    const modelServingStates = (models || []).map((model) => deriveModelServingState(model, instances || [], workers, deploymentAttempts));
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

    const billingAttention = buildBillingAttentionQueue(quota, usageRows, costs);
    const operationalAttentionQueue = buildOperationalAttentionQueue(
      deploymentSummaries,
      connectedProviders.length,
      visibleProviders.length,
      workers,
      activeInstances,
      servingUnverifiedCount,
    );
    const attentionQueue = [...operationalAttentionQueue, ...billingAttention].slice(0, 6);

    const deploymentTrendRecent = deploymentSummaries.slice(0, 6);
    const deploymentTrend = {
      recent: deploymentTrendRecent,
      failed: deploymentTrendRecent.filter((summary) => (
        summary.attempt.outcome === 'request_failed'
        || summary.attempt.inference_verification?.status === 'failed'
        || summary.readiness.tone === 'error'
      )).length,
      pending: deploymentTrendRecent.filter((summary) => (
        ['PROVISIONING', 'WAITING FOR WORKER', 'WORKER CONNECTING', 'MODEL LOADING', 'MODEL LOAD DELAY', 'PARTIAL READY', 'SERVING UNVERIFIED'].includes(summary.readiness.label)
      )).length,
      stable: deploymentTrendRecent.filter((summary) => (
        summary.readiness.label === 'SERVING VERIFIED' || summary.attempt.inference_verification?.status === 'passed'
      )).length,
    };
    const verificationTrend = deploymentSummaries.filter((summary) => Boolean(summary.attempt.inference_verification)).slice(0, 4);

    const byDay = new Map<string, { requests: number; tokens: number }>();
    for (const row of usageRows) {
      const day = row.bucket_start.slice(0, 10);
      const totals = byDay.get(day) || { requests: 0, tokens: 0 };
      totals.requests += row.requests;
      totals.tokens += row.tokens;
      byDay.set(day, totals);
    }
    const usageTrend = Array.from(byDay.entries())
      .sort(([left], [right]) => left.localeCompare(right))
      .slice(-7)
      .map(([day, totals]) => ({ day, requests: totals.requests, tokens: totals.tokens }));
    const usageTrendMaxRequests = usageTrend.reduce((max, entry) => Math.max(max, entry.requests), 0);
    const usageSummary = usageRows.reduce((acc, row) => {
      acc.requests += row.requests;
      acc.tokens += row.tokens;
      return acc;
    }, { requests: 0, tokens: 0 });

    const requestMetricSamples = usageTrend.length > 0
      ? usageTrend.map((entry) => entry.requests)
      : (stats?.requests?.per_second != null ? [stats.requests.per_second] : []);
    const workerLatencySamples = (workers || []).map((worker) => worker.avg_latency_ms).filter((sample): sample is number => Number.isFinite(sample) && sample > 0);
    const verificationLatencySamples = deploymentSummaries
      .map((summary) => summary.attempt.inference_verification?.latency_ms)
      .filter((sample): sample is number => typeof sample === 'number' && Number.isFinite(sample) && sample > 0);
    const latencyMetricSamples = workerLatencySamples.length > 0
      ? workerLatencySamples
      : verificationLatencySamples.length > 0
        ? verificationLatencySamples
        : (stats?.latency?.avg_ms && stats.latency.avg_ms > 0 ? [stats.latency.avg_ms] : []);
    const workerThroughputSamples = (workers || []).map((worker) => worker.requests_per_sec).filter((sample): sample is number => Number.isFinite(sample) && sample >= 0);
    const tokenSamples = usageTrend.map((entry) => entry.tokens).filter((sample) => sample > 0);
    const throughputMetricSamples = workerThroughputSamples.length > 0
      ? workerThroughputSamples
      : tokenSamples.length > 0
        ? tokenSamples
        : (stats?.requests?.per_second != null ? [stats.requests.per_second] : []);

    const latencyAvg = latencyMetricSamples.length > 0
      ? Math.round(latencyMetricSamples.reduce((sum, sample) => sum + sample, 0) / latencyMetricSamples.length)
      : 0;
    const requestVolumeNum = usageSummary.requests > 0
      ? usageSummary.requests
      : stats?.requests?.per_second
        ? Math.round(stats.requests.per_second * 86400 / 1000) * 1000
        : 0;
    const throughputNum = stats?.requests?.per_second ?? 0;

    const hasInferenceVerifiedDeployment = deploymentSummaries.some((summary) => summary.attempt.inference_verification?.status === 'passed');
    const firstWorkspaceChecklist = buildFirstWorkspaceChecklist({
      providerReady: visibleProviders.length > 0,
      providerConnected: connectedProviders.length > 0,
      modelReady: (models?.length || 0) > 0,
      nodeReady: (instances?.length || 0) > 0,
      inferenceVerified: hasInferenceVerifiedDeployment,
      collaborationReady: workspaceInvites.length > 0 || workspaceServiceAccounts.length > 0,
    });
    const checklistCompletedCount = firstWorkspaceChecklist.filter((item) => item.done).length;
    const workspaceMaturity = buildWorkspaceMaturity({
      checklist: firstWorkspaceChecklist,
      attentionQueue: attentionQueue.map(({ severity, title, detail, actionLabel, action }) => ({ severity, title, detail, actionLabel, action })),
      servingVerifiedCount,
      servingUnverifiedCount,
      pendingDeploymentCount,
      activeInstanceCount: activeInstances.length,
    });
    const liveWorkspaceOperations = buildLiveWorkspaceOperations({
      maturityState: workspaceMaturity.state,
      modelServingStates,
      activeNodeCount: activeInstances.length,
      deploymentSummaries,
      operationalAttentionQueue: operationalAttentionQueue.map(({ severity, title, detail }) => ({ severity, title, detail })),
    });
    const isNewWorkspace = checklistCompletedCount < firstWorkspaceChecklist.length
      && servingVerifiedCount === 0
      && activeInstances.length === 0;

    const dashboardGuideCopy = liveWorkspaceOperations.show
      ? 'Use workspace state for the top-level health signal, live operations for day-two serving health, and the attention queue for what needs operator action right now.'
      : 'Use the attention queue for what needs action now, then use recent deployment activity to see what changed most recently.';

    const deploymentHistoryPreview = deploymentTrend.recent.slice(0, 3);
    const hiddenDeploymentHistoryCount = Math.max(deploymentTrend.recent.length - deploymentHistoryPreview.length, 0);
    const recentActivity = deploymentSummaries.slice(0, 4);
    const nodeOverviewRows = [
      { label: 'Active Instances', value: String(activeInstances.length), secondary: `${instances?.length || 0} total` },
      { label: 'Cost / Hour', value: `$${costs?.current_hourly?.toFixed(2) || '0.00'}`, secondary: `$${costs?.today_total?.toFixed(2) || '0.00'} today` },
      { label: 'Queue Depth', value: String(stats?.requests?.queue_depth || 0), secondary: 'pending' },
      { label: 'Avg GPU Util', value: stats?.gpu?.avg_utilization != null ? `${Math.round(stats.gpu.avg_utilization)}%` : '-', secondary: 'across workers' },
      { label: 'Memory Usage', value: stats?.memory?.total_bytes ? `${((stats.memory.used_bytes / stats.memory.total_bytes) * 100).toFixed(0)}%` : '-', secondary: stats?.memory?.total_bytes ? `${(stats.memory.used_bytes / (1024 ** 3)).toFixed(1)} / ${(stats.memory.total_bytes / (1024 ** 3)).toFixed(1)} GB` : '-' },
    ];
    const workspaceSnapshotItems = [
      { label: 'ACTIVE NODES', value: String(activeInstances.length), mono: true },
      { label: 'SERVING VERIFIED', value: String(servingVerifiedCount), mono: true },
      { label: 'VERIFY PENDING', value: String(servingUnverifiedCount), mono: true },
      { label: 'QUEUE DEPTH', value: String(stats?.requests?.queue_depth || 0), mono: true },
      { label: 'CURRENT HOURLY', value: `$${costs?.current_hourly?.toFixed(2) || '0.00'}`, mono: true },
      { label: 'TODAY TOTAL', value: `$${costs?.today_total?.toFixed(2) || '0.00'}`, mono: true },
    ] as const;
    const liveOperationsItems = [
      { label: 'ACTIVE SERVING MODELS', value: String(liveWorkspaceOperations.activeServingModels), mono: true },
      { label: 'ACTIVE NODES', value: String(liveWorkspaceOperations.activeNodes), mono: true },
      { label: 'VERIFICATION', value: liveWorkspaceOperations.verificationLabel },
      { label: 'DEGRADED RUNTIME', value: String(liveWorkspaceOperations.degradedRuntimeCount), mono: true },
    ] as const;

    const quickConfigFields: QuickConfigField[] = [
      {
        key: 'monthly_request_limit',
        label: 'Monthly Request Limit',
        value: quota?.monthly_request_limit != null ? String(quota.monthly_request_limit) : '',
        inputType: 'number',
      },
      {
        key: 'monthly_token_limit',
        label: 'Monthly Token Limit',
        value: quota?.monthly_token_limit != null ? String(quota.monthly_token_limit) : '',
        inputType: 'number',
      },
      {
        key: 'enforce_hard_limits',
        label: 'Enforce Hard Limits',
        value: String(quota?.enforce_hard_limits ?? true),
        inputType: 'select',
        selectOptions: [
          { value: 'true', label: 'Yes — block when exceeded' },
          { value: 'false', label: 'No — warn only' },
        ],
      },
    ];

    return {
      gatewayDown,
      activeInstances,
      healthyWorkers,
      loadedModels,
      billingAttention,
      attentionQueue,
      deploymentTrend,
      deploymentHistoryPreview,
      hiddenDeploymentHistoryCount,
      verificationTrend,
      usageTrend,
      usageTrendMaxRequests,
      requestMetricSamples,
      latencyMetricSamples,
      throughputMetricSamples,
      latencyAvg,
      requestVolumeNum,
      throughputNum,
      servingVerifiedCount,
      servingUnverifiedCount,
      degradedDeploymentCount,
      pendingDeploymentCount,
      latestFailure,
      latestVerification,
      firstWorkspaceChecklist,
      checklistCompletedCount,
      workspaceMaturity,
      liveWorkspaceOperations,
      isNewWorkspace,
      dashboardGuideCopy,
      recentActivity,
      nodeOverviewRows,
      workspaceSnapshotItems,
      liveOperationsItems,
      quickConfigFields,
    };
  }, [
    costs,
    deploymentAttempts,
    errorStats,
    errorWorkers,
    instances,
    isLoading,
    models,
    providers,
    quota,
    stats,
    usageRows,
    workers,
    workspaceInvites,
    workspaceServiceAccounts,
  ]);
}
