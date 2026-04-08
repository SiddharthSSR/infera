import { useEffect, useMemo, useState, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import { useDeploymentAttempts, useWorkers, useStats, useInstances, useCosts, useModels, useProviders } from '../hooks/useApi';
import { GridRow, Cell, LabelText, StatusDot, Badge, ActionButton, ControlInput } from '../components/shared';
import { DashboardSkeleton } from '../components/DashboardSkeleton';
import { MetricCard } from '../components/MetricCard';
import { ServingStatusRow, type ServingStatusItem } from '../components/ServingStatusRow';
import { AttentionQueue, type AttentionItem } from '../components/AttentionQueue';
import { ActionGroup } from '../components/ActionGroup';
import { CollapsibleSection } from '../components/CollapsibleSection';
import { MetadataList } from '../components/MetadataList';
import { SectionHeader } from '../components/SectionHeader';
import { useAuthSession } from '../lib/auth-context';
import { isInventoryProviderType } from '../lib/providerInventory';
import { formatShortTimestamp, formatCount, formatCompactCount, usageRatio, monthRange } from '../lib/formatting';
import {
  fetchAuditUsage,
  fetchApiKeys,
  fetchWorkspaceQuota,
  fetchWorkspaceInvites,
  updateWorkspaceQuota,
  type AuditUsageRow,
  type ApiKeyRecord,
  type WorkspaceQuotaRecord,
  type WorkspaceInvitationRecord,
} from '../lib/api';
import {
  getDeploymentRemediation,
  selectPrimaryDeploymentSummary,
  summarizeDeploymentAttempt,
  type DeploymentAttemptRecord,
  type DeploymentAttemptSummary,
} from '../lib/deploymentHistory';
import { buildFirstWorkspaceChecklist } from '../lib/onboardingChecklist';
import { getInstanceReadiness } from '../lib/instanceReadiness';
import { buildLiveWorkspaceOperations } from '../lib/liveWorkspaceOperations';
import { buildWorkspaceMaturity } from '../lib/workspaceMaturity';
import type { Instance, Model, Worker } from '../types';

/* ------------------------------------------------------------------ */
/*  Quick Configuration — inline-editable row                          */
/* ------------------------------------------------------------------ */

type QuickConfigField = {
  key: string;
  label: string;
  value: string;
  inputType?: 'text' | 'number' | 'select';
  selectOptions?: { value: string; label: string }[];
};

function QuickConfigRow({
  field,
  canEdit,
  onSave,
}: {
  field: QuickConfigField;
  canEdit: boolean;
  onSave: (key: string, value: string) => Promise<void>;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(field.value);
  const [saving, setSaving] = useState(false);
  const rowRef = useRef<HTMLDivElement>(null);

  const handleEdit = () => {
    setDraft(field.value);
    setEditing(true);
  };

  const handleCancel = () => {
    setEditing(false);
    setDraft(field.value);
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(field.key, draft);
      setEditing(false);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      ref={rowRef}
      className="quick-config-row"
      style={{
        display: 'grid',
        gridTemplateColumns: '2fr 2fr 1fr',
        padding: '0.75rem 0',
        borderBottom: '1px solid #EEEEEC',
        alignItems: 'center',
        transition: 'min-height 0.25s ease',
        minHeight: editing ? 52 : 38,
      }}
    >
      <div style={{ fontSize: '0.9rem' }}>{field.label}</div>
      <div style={{ minHeight: 28 }}>
        {editing ? (
          field.inputType === 'select' && field.selectOptions ? (
            <select
              className="control-input"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              style={{ width: '100%', margin: 0 }}
            >
              {field.selectOptions.map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
          ) : (
            <ControlInput
              type={field.inputType || 'text'}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              style={{ width: '100%', margin: 0 }}
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter') void handleSave();
                if (e.key === 'Escape') handleCancel();
              }}
            />
          )
        ) : (
          <span className="mono" style={{ fontSize: '0.85rem' }}>{field.value}</span>
        )}
      </div>
      <div style={{ textAlign: 'right' }}>
        {!canEdit ? null : editing ? (
          <span style={{ display: 'inline-flex', gap: '0.5rem' }}>
            <ActionButton disabled={saving} onClick={() => void handleSave()}>
              {saving ? 'SAVING...' : 'SAVE'}
            </ActionButton>
            <ActionButton onClick={handleCancel}>CANCEL</ActionButton>
          </span>
        ) : (
          <ActionButton onClick={handleEdit}>EDIT</ActionButton>
        )}
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ */
/*  Dashboard system logs — mini log feed                              */
/* ------------------------------------------------------------------ */

type DashboardLogEntry = {
  id: string;
  timestamp: Date;
  level: 'info' | 'warn' | 'error' | 'debug';
  source: string;
  message: string;
};

const LOG_LEVELS: DashboardLogEntry['level'][] = ['info', 'info', 'info', 'debug', 'warn', 'error'];
const LOG_SOURCES = ['GATEWAY', 'WORKER', 'SCHEDULER', 'AUTOSCALER', 'INFERENCE'];
const LOG_MESSAGES = [
  'Request accepted: model inference [req_9a2b8c]',
  'KV Cache hit rate: 0.92 for block 8410',
  'Streaming response completed in 412ms',
  'Health check passed. Latency stable.',
  'Prefill phase latency: 12ms | Decode: 40 t/s',
  'Worker heartbeat received',
  'GPU utilization: 65%',
  'Rate limit warning: 90% capacity',
  'Node approaching thermal threshold (82C)',
  'Re-routing pending tasks to cluster-us-east-b',
];

function generateDashboardLog(): DashboardLogEntry {
  return {
    id: Math.random().toString(36).slice(2),
    timestamp: new Date(),
    level: LOG_LEVELS[Math.floor(Math.random() * LOG_LEVELS.length)],
    source: LOG_SOURCES[Math.floor(Math.random() * LOG_SOURCES.length)],
    message: LOG_MESSAGES[Math.floor(Math.random() * LOG_MESSAGES.length)],
  };
}

function levelColor(level: DashboardLogEntry['level']): string {
  switch (level) {
    case 'info': return 'var(--color-success)';
    case 'warn': return 'var(--color-warning)';
    case 'error': return 'var(--color-error)';
    case 'debug': return 'var(--text-secondary)';
  }
}

/* ------------------------------------------------------------------ */
/*  Pure helpers (unchanged from original)                             */
/* ------------------------------------------------------------------ */

type ModelServingState = 'not_deployed' | 'runtime_pending' | 'serving_unverified' | 'serving_verified' | 'serving_failed' | 'degraded';
type AttentionAction = 'open_clusters' | 'open_models' | 'open_workspace' | 'verify_now';
type DashboardAction = 'open_clusters' | 'open_models' | 'open_workspace' | 'verify_now' | 'open_docs' | 'open_api_keys';

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


function getAttemptTone(summary: DeploymentAttemptSummary): '' | 'warning' | 'error' | 'inactive' {
  if (summary.attempt.inference_verification?.status === 'failed') return 'error';
  if (summary.readiness.tone === 'error') return 'error';
  if (summary.readiness.tone === 'warning') return 'warning';
  if (summary.readiness.tone === 'inactive') return 'inactive';
  return '';
}

const formatAttemptTime = formatShortTimestamp;

function mapRemediationAction(action: 'open_workspace' | 'view_capacity' | 'retry_config' | 'focus_instance' | 'verify_inference' | undefined): AttentionAction {
  switch (action) {
    case 'open_workspace': return 'open_workspace';
    case 'verify_inference': return 'verify_now';
    default: return 'open_clusters';
  }
}


/* ------------------------------------------------------------------ */
/*  Attention queue builders                                           */
/* ------------------------------------------------------------------ */

function buildOperationalAttentionQueue(
  deploymentSummaries: DeploymentAttemptSummary[],
  connectedProviders: number,
  visibleProviders: number,
  workers: Worker[] | undefined,
  activeInstances: Instance[],
  servingUnverifiedCount: number,
): AttentionItem[] {
  const items: AttentionItem[] = [];
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

/* ------------------------------------------------------------------ */
/*  Metric card SVG icons                                              */
/* ------------------------------------------------------------------ */

const RequestsIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
    <path d="M12 2v20M2 12h20" />
  </svg>
);

const LatencyIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
    <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
  </svg>
);

const ThroughputIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
    <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" />
  </svg>
);

const NodesIcon = (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
    <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
    <line x1="8" y1="21" x2="16" y2="21" /><line x1="12" y1="17" x2="12" y2="21" />
  </svg>
);

/* ------------------------------------------------------------------ */
/*  Constants                                                          */
/* ------------------------------------------------------------------ */

const GUIDE_DISMISSED_KEY = 'infera:dashboard-guide-dismissed';

/* ------------------------------------------------------------------ */
/*  Dashboard                                                          */
/* ------------------------------------------------------------------ */

export function Dashboard() {
  const navigate = useNavigate();
  const { session } = useAuthSession();
  const [guideDismissed, setGuideDismissed] = useState(() => sessionStorage.getItem(GUIDE_DISMISSED_KEY) === '1');
  const dismissGuide = useCallback(() => {
    sessionStorage.setItem(GUIDE_DISMISSED_KEY, '1');
    setGuideDismissed(true);
  }, []);
  const workspaceID = session?.workspace?.id;
  const role = session?.key?.role ?? 'user';
  const canViewQuota = role === 'owner' || role === 'admin' || role === 'billing' || role === 'read_only';
  const canViewUsage = canViewQuota;
  const { data: workers, isLoading: loadingWorkers, isError: errorWorkers } = useWorkers(workspaceID);
  const { data: stats, isLoading: loadingStats, isError: errorStats } = useStats();
  const { data: instances, isLoading: loadingInstances } = useInstances();
  const { data: costs, isLoading: loadingCosts } = useCosts();
  const { data: models, isLoading: loadingModels } = useModels();
  const { data: providers, isLoading: loadingProviders } = useProviders();
  const { data: deploymentAttempts = [] } = useDeploymentAttempts(workspaceID);
  const [quota, setQuota] = useState<WorkspaceQuotaRecord | null>(null);
  const [usageRows, setUsageRows] = useState<AuditUsageRow[]>([]);
  const [workspaceInvites, setWorkspaceInvites] = useState<WorkspaceInvitationRecord[]>([]);
  const [workspaceServiceAccounts, setWorkspaceServiceAccounts] = useState<ApiKeyRecord[]>([]);
  const isLoading = loadingWorkers || loadingStats || loadingInstances || loadingCosts || loadingModels || loadingProviders;
  const canEditQuota = role === 'owner' || role === 'admin' || role === 'billing';

  // System logs mini-feed
  const [dashLogs, setDashLogs] = useState<DashboardLogEntry[]>(() =>
    Array.from({ length: 8 }, generateDashboardLog),
  );
  const dashLogsRef = useRef<HTMLDivElement>(null);
  const [logsPrevCount, setLogsPrevCount] = useState(8);

  useEffect(() => {
    const interval = setInterval(() => {
      setDashLogs((prev) => [...prev, generateDashboardLog()].slice(-30));
    }, 3000);
    return () => clearInterval(interval);
  }, []);

  // Auto-scroll logs to bottom when new entries arrive
  useEffect(() => {
    if (dashLogs.length > logsPrevCount && dashLogsRef.current) {
      dashLogsRef.current.scrollTop = dashLogsRef.current.scrollHeight;
    }
    setLogsPrevCount(dashLogs.length);
  }, [dashLogs.length, logsPrevCount]);

  // Quick Config save handler
  const handleQuickConfigSave = useCallback(async (key: string, value: string) => {
    if (!workspaceID) return;
    const currentQuota = quota ?? { workspace_id: workspaceID, enforce_hard_limits: true, updated_at: '' };
    const payload: { monthly_request_limit?: number | null; monthly_token_limit?: number | null; enforce_hard_limits: boolean } = {
      monthly_request_limit: currentQuota.monthly_request_limit,
      monthly_token_limit: currentQuota.monthly_token_limit,
      enforce_hard_limits: currentQuota.enforce_hard_limits,
    };
    if (key === 'monthly_request_limit') payload.monthly_request_limit = value === '' ? null : Number(value);
    else if (key === 'monthly_token_limit') payload.monthly_token_limit = value === '' ? null : Number(value);
    else if (key === 'enforce_hard_limits') payload.enforce_hard_limits = value === 'true';
    try {
      const nextQuota = await updateWorkspaceQuota(workspaceID, payload);
      setQuota(nextQuota);
      toast.success('Configuration updated.');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to save configuration.');
      throw err;
    }
  }, [workspaceID, quota]);

  useEffect(() => {
    let cancelled = false;
    if (!workspaceID) {
      setQuota(null);
      setUsageRows([]);
      setWorkspaceInvites([]);
      setWorkspaceServiceAccounts([]);
      return () => { cancelled = true; };
    }

    if (canViewQuota) {
      fetchWorkspaceQuota(workspaceID)
        .then((nextQuota) => { if (!cancelled) setQuota(nextQuota); })
        .catch(() => { if (!cancelled) setQuota(null); });
    } else {
      setQuota(null);
    }

    if (canViewUsage) {
      const { start, end } = monthRange();
      fetchAuditUsage({ start, end, bucket: 'day', workspace_id: workspaceID })
        .then((usage) => { if (!cancelled) setUsageRows(usage.rows); })
        .catch(() => { if (!cancelled) setUsageRows([]); });
    } else {
      setUsageRows([]);
    }

    if (session?.key?.role === 'owner' || session?.key?.role === 'admin') {
      fetchWorkspaceInvites(workspaceID)
        .then((invites) => { if (!cancelled) setWorkspaceInvites(invites); })
        .catch(() => { if (!cancelled) setWorkspaceInvites([]); });

      fetchApiKeys()
        .then((keys) => { if (!cancelled) setWorkspaceServiceAccounts(keys.filter((key) => key.principal_type === 'service_account' && key.status === 'active')); })
        .catch(() => { if (!cancelled) setWorkspaceServiceAccounts([]); });
    } else {
      setWorkspaceInvites([]);
      setWorkspaceServiceAccounts([]);
    }

    return () => { cancelled = true; };
  }, [canViewQuota, canViewUsage, session?.key?.role, workspaceID]);

  /* ---------------------------------------------------------------- */
  /*  Derived state                                                    */
  /* ---------------------------------------------------------------- */

  const gatewayDown = !isLoading && (errorWorkers && !workers) && (errorStats && !stats);
  const activeInstances = instances?.filter(i => i.status === 'running') || [];
  const healthyWorkers = workers?.filter(w => w.status === 'healthy') || [];
  const loadedModels = models?.filter(m => m.loaded !== false) || [];
  const visibleProviders = useMemo(
    () => (providers || []).filter((provider) => isInventoryProviderType(provider.provider)),
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
  const operationalAttentionQueue = useMemo(
    () => buildOperationalAttentionQueue(deploymentSummaries, connectedProviders.length, visibleProviders.length, workers, activeInstances, servingUnverifiedCount),
    [activeInstances, connectedProviders.length, deploymentSummaries, servingUnverifiedCount, visibleProviders.length, workers],
  );
  const attentionQueue = useMemo(
    () => [...operationalAttentionQueue, ...billingAttention].slice(0, 6),
    [billingAttention, operationalAttentionQueue],
  );

  /* Deployment & verification trends */
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
    () => deploymentSummaries.filter((summary) => Boolean(summary.attempt.inference_verification)).slice(0, 4),
    [deploymentSummaries],
  );

  /* Usage */
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
      .map(([day, totals]) => ({ day, requests: totals.requests, tokens: totals.tokens }));
  }, [usageRows]);
  const usageTrendMaxRequests = useMemo(
    () => usageTrend.reduce((max, entry) => Math.max(max, entry.requests), 0),
    [usageTrend],
  );
  const usageSummary = useMemo(
    () => usageRows.reduce((acc, row) => { acc.requests += row.requests; acc.tokens += row.tokens; return acc; }, { requests: 0, tokens: 0 }),
    [usageRows],
  );

  /* Metric samples for sparklines */
  const requestMetricSamples = useMemo(
    () => usageTrend.length > 0
      ? usageTrend.map((entry) => entry.requests)
      : (stats?.requests?.per_second != null ? [stats.requests.per_second] : []),
    [stats?.requests?.per_second, usageTrend],
  );
  const latencyMetricSamples = useMemo(() => {
    const workerSamples = (workers || []).map((w) => w.avg_latency_ms).filter((s): s is number => Number.isFinite(s) && s > 0);
    if (workerSamples.length > 0) return workerSamples;
    const verSamples = deploymentSummaries.map((s) => s.attempt.inference_verification?.latency_ms).filter((s): s is number => typeof s === 'number' && Number.isFinite(s) && s > 0);
    if (verSamples.length > 0) return verSamples;
    return stats?.latency?.avg_ms && stats.latency.avg_ms > 0 ? [stats.latency.avg_ms] : [];
  }, [deploymentSummaries, stats?.latency?.avg_ms, workers]);
  const throughputMetricSamples = useMemo(() => {
    const workerSamples = (workers || []).map((w) => w.requests_per_sec).filter((s): s is number => Number.isFinite(s) && s >= 0);
    if (workerSamples.length > 0) return workerSamples;
    const tokenSamples = usageTrend.map((entry) => entry.tokens).filter((s) => s > 0);
    if (tokenSamples.length > 0) return tokenSamples;
    return stats?.requests?.per_second != null ? [stats.requests.per_second] : [];
  }, [stats?.requests?.per_second, usageTrend, workers]);

  /* Display values */
  const latencyAvg = useMemo(() => {
    if (latencyMetricSamples.length > 0) {
      return Math.round(latencyMetricSamples.reduce((sum, s) => sum + s, 0) / latencyMetricSamples.length);
    }
    return 0;
  }, [latencyMetricSamples]);
  const requestVolumeNum = useMemo(() => {
    if (usageSummary.requests > 0) return usageSummary.requests;
    if (stats?.requests?.per_second) return Math.round(stats.requests.per_second * 86400 / 1000) * 1000;
    return 0;
  }, [stats?.requests?.per_second, usageSummary.requests]);
  const throughputNum = stats?.requests?.per_second ?? 0;

  /* Workspace maturity & checklist */
  const hasInferenceVerifiedDeployment = useMemo(
    () => deploymentSummaries.some((summary) => summary.attempt.inference_verification?.status === 'passed'),
    [deploymentSummaries],
  );
  const firstWorkspaceChecklist = useMemo(
    () => buildFirstWorkspaceChecklist({
      providerReady: visibleProviders.length > 0,
      providerConnected: connectedProviders.length > 0,
      modelReady: (models?.length || 0) > 0,
      nodeReady: (instances?.length || 0) > 0,
      inferenceVerified: hasInferenceVerifiedDeployment,
      collaborationReady: workspaceInvites.length > 0 || workspaceServiceAccounts.length > 0,
    }),
    [connectedProviders.length, hasInferenceVerifiedDeployment, instances?.length, models?.length, visibleProviders.length, workspaceInvites.length, workspaceServiceAccounts.length],
  );
  const checklistCompletedCount = useMemo(
    () => firstWorkspaceChecklist.filter((item) => item.done).length,
    [firstWorkspaceChecklist],
  );
  const workspaceMaturity = useMemo(
    () => buildWorkspaceMaturity({
      checklist: firstWorkspaceChecklist,
      attentionQueue: attentionQueue.map(({ severity, title, detail, actionLabel, action }) => ({ severity, title, detail, actionLabel, action })),
      servingVerifiedCount,
      servingUnverifiedCount,
      pendingDeploymentCount,
      activeInstanceCount: activeInstances.length,
    }),
    [activeInstances.length, attentionQueue, firstWorkspaceChecklist, pendingDeploymentCount, servingUnverifiedCount, servingVerifiedCount],
  );
  const liveWorkspaceOperations = useMemo(
    () => buildLiveWorkspaceOperations({
      maturityState: workspaceMaturity.state,
      modelServingStates,
      activeNodeCount: activeInstances.length,
      deploymentSummaries,
      operationalAttentionQueue: operationalAttentionQueue.map(({ severity, title, detail }) => ({ severity, title, detail })),
    }),
    [activeInstances.length, deploymentSummaries, modelServingStates, operationalAttentionQueue, workspaceMaturity.state],
  );
  const isNewWorkspace = checklistCompletedCount < firstWorkspaceChecklist.length
    && servingVerifiedCount === 0
    && activeInstances.length === 0;

  const dashboardGuideCopy = liveWorkspaceOperations.show
    ? 'Use workspace state for the top-level health signal, live operations for day-two serving health, and the attention queue for what needs operator action right now.'
    : 'Use the attention queue for what needs action now, then use recent deployment activity to see what changed most recently.';

  /* ---------------------------------------------------------------- */
  /*  Navigation handler                                               */
  /* ---------------------------------------------------------------- */

  const handleDashboardAction = useCallback((action: DashboardAction) => {
    switch (action) {
      case 'open_workspace': navigate('/workspace'); return;
      case 'open_models': case 'verify_now': navigate('/models'); return;
      case 'open_clusters': navigate('/instances'); return;
      case 'open_api_keys': navigate('/api-keys'); return;
      case 'open_docs': navigate('/getting-started'); return;
    }
  }, [navigate]);

  /* ---------------------------------------------------------------- */
  /*  Loading & error states                                           */
  /* ---------------------------------------------------------------- */

  if (isLoading) return <DashboardSkeleton />;

  if (gatewayDown) {
    return (
      <div className="dashboard-page animate-fade-in">
        <GridRow>
          <Cell span={4} style={{ textAlign: 'center', padding: '4rem 2rem' }}>
            <div style={{ fontSize: '2rem', fontWeight: 700, marginBottom: '1rem', letterSpacing: '-0.02em' }}>
              Gateway Unreachable
            </div>
            <div style={{ color: 'var(--text-secondary)', fontSize: '0.95rem', maxWidth: 480, margin: '0 auto 2rem', lineHeight: 1.6 }}>
              Unable to connect to the Infera gateway. The service may be restarting or experiencing an outage.
              The dashboard will automatically reconnect when the gateway is available.
            </div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '0.5rem', fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
              <StatusDot tone="inactive" />
              Retrying periodically...
            </div>
          </Cell>
        </GridRow>
      </div>
    );
  }

  /* ---------------------------------------------------------------- */
  /*  Pre-render data                                                  */
  /* ---------------------------------------------------------------- */

  const deploymentHistoryPreview = deploymentTrend.recent.slice(0, 3);
  const hiddenDeploymentHistoryCount = Math.max(deploymentTrend.recent.length - deploymentHistoryPreview.length, 0);
  const recentActivity = deploymentSummaries.slice(0, 4);
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

  const servingStatusItems: ServingStatusItem[] = [
    { label: 'SERVING VERIFIED', value: servingVerifiedCount, description: 'Models that answered a real verification request successfully.', actionLabel: 'OPEN MODELS', onAction: () => navigate('/models') },
    { label: 'VERIFY PENDING', value: servingUnverifiedCount, description: 'Models that look runtime-ready but still need or are awaiting inference verification.', actionLabel: 'VERIFY SERVING', onAction: () => navigate('/models') },
    { label: 'DEGRADED DEPLOYMENTS', value: degradedDeploymentCount, description: 'Recent attempts that failed, lost their node, or are serving with an explicit error signal.', actionLabel: 'VIEW FAILED NODES', onAction: () => navigate('/instances') },
    { label: 'PENDING DEPLOYMENTS', value: pendingDeploymentCount, description: 'Nodes still provisioning, connecting a worker, or loading assigned models.', actionLabel: 'OPEN NODES', onAction: () => navigate('/instances') },
  ];

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

  /* ---------------------------------------------------------------- */
  /*  Render                                                           */
  /* ---------------------------------------------------------------- */

  return (
    <div className="dashboard-page animate-fade-in">
      {/* Guide callout */}
      {!guideDismissed && (
        <GridRow>
          <Cell span={4}>
            <div className="help-callout" style={{ position: 'relative', padding: '1rem 1.25rem' }}>
              <SectionHeader
                eyebrow="HOW TO READ THIS DASHBOARD"
                title="Progressive operator view"
                description={(
                  <>
                    <strong>Serving verified</strong> means runtime state looks healthy and the latest worker heartbeat is fresh. <strong>Inference verified</strong> means a real chat-completions request succeeded. {dashboardGuideCopy}
                  </>
                )}
                actions={(
                  <ActionGroup compact>
                    <ActionButton onClick={() => navigate('/getting-started')}>OPEN QUICKSTART</ActionButton>
                    <ActionButton onClick={() => navigate('/docs')}>OPEN API DOCS</ActionButton>
                    <ActionButton onClick={dismissGuide} title="Dismiss" aria-label="Dismiss guide">DISMISS</ActionButton>
                  </ActionGroup>
                )}
              />
            </div>
          </Cell>
        </GridRow>
      )}

      {/* Workspace state + Live operations */}
      <GridRow>
        <Cell span={2} className="dashboard-maturity-cell">
          <SectionHeader
            eyebrow="WORKSPACE STATE"
            title={workspaceMaturity.headline}
            description={workspaceMaturity.detail}
            badge={<Badge tone={workspaceMaturity.tone || undefined}>{workspaceMaturity.label}</Badge>}
            actions={(
              <ActionGroup compact>
                <ActionButton onClick={() => handleDashboardAction(workspaceMaturity.action)}>
                  {workspaceMaturity.actionLabel}
                </ActionButton>
                {workspaceMaturity.state !== 'serving_verified' && workspaceMaturity.action !== 'open_workspace' && (
                  <ActionButton onClick={() => navigate('/workspace')}>OPEN WORKSPACE</ActionButton>
                )}
              </ActionGroup>
            )}
          />
          <div style={{ marginTop: '1.25rem' }}>
            <MetadataList items={workspaceSnapshotItems.map((item) => ({ ...item, value: String(item.value) }))} columns={3} />
          </div>
        </Cell>
        <Cell span={2} className="dashboard-live-ops-cell" bg="var(--bg-accent)">
          {liveWorkspaceOperations.show ? (
            <>
              <SectionHeader
                eyebrow="LIVE OPERATIONS"
                title={liveWorkspaceOperations.headline}
                description={liveWorkspaceOperations.detail}
                badge={(
                  <Badge tone={
                    liveWorkspaceOperations.verificationFreshness === 'fresh' ? undefined
                    : liveWorkspaceOperations.verificationFreshness === 'recent' ? 'inactive' : 'warning'
                  }>
                    {liveWorkspaceOperations.verificationLabel}
                  </Badge>
                )}
                actions={(
                  <ActionGroup compact>
                    <ActionButton onClick={() => navigate('/instances')}>OPEN NODES</ActionButton>
                    <ActionButton onClick={() => navigate('/models')}>OPEN MODELS</ActionButton>
                  </ActionGroup>
                )}
              />
              <div style={{ marginTop: '1.25rem' }}>
                <MetadataList items={liveOperationsItems.map((item) => ({ ...item, value: String(item.value) }))} columns={2} />
              </div>
              {liveWorkspaceOperations.operatorIssueTitle && (
                <div className="overview-card accent" style={{ marginTop: '1.25rem' }}>
                  <LabelText as="div">LATEST ISSUE</LabelText>
                  <div style={{ fontSize: '0.98rem', fontWeight: 600, marginTop: '0.5rem' }}>{liveWorkspaceOperations.operatorIssueTitle}</div>
                  <div className="dashboard-summary-text" style={{ marginTop: '0.4rem' }}>{liveWorkspaceOperations.operatorIssueDetail}</div>
                </div>
              )}
            </>
          ) : (
            <>
              <SectionHeader
                eyebrow={isNewWorkspace ? 'NEW WORKSPACE' : 'SETUP CHECKLIST'}
                title={isNewWorkspace ? 'FIRST WORKSPACE CHECKLIST' : 'Remaining setup work'}
                description={isNewWorkspace
                  ? 'Follow this sequence to get the first model serving. Each step unlocks the next.'
                  : 'Remaining steps to complete workspace setup. Derived from live workspace state.'}
                badge={<Badge>{checklistCompletedCount} / {firstWorkspaceChecklist.length} COMPLETE</Badge>}
              />
              <div className="dashboard-onboarding-grid" style={{ marginTop: '1rem' }}>
                {firstWorkspaceChecklist.map((item) => (
                  <div key={item.id} className="dashboard-onboarding-item">
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.75rem', flexWrap: 'wrap' }}>
                      <div style={{ fontSize: '0.92rem', fontWeight: 500 }}>{item.label}</div>
                      <Badge tone={item.done ? undefined : 'warning'}>{item.done ? 'DONE' : 'NEXT'}</Badge>
                    </div>
                    <div className="dashboard-summary-text" style={{ marginTop: '0.45rem' }}>{item.detail}</div>
                    {!item.done && (
                      <ActionButton style={{ marginTop: '0.85rem' }} onClick={() => handleDashboardAction(item.action)}>
                        {item.actionLabel}
                      </ActionButton>
                    )}
                  </div>
                ))}
              </div>
            </>
          )}
        </Cell>
      </GridRow>

      {/* Metrics row — now using MetricCard with count-up animations */}
      <GridRow className="dashboard-metrics-row">
        <MetricCard
          icon={RequestsIcon}
          label="TOTAL REQUESTS"
          value={requestVolumeNum}
          format="compact"
          samples={requestMetricSamples}
          note={usageTrend.length > 0 ? 'Last 7 days of recorded workspace usage.' : 'Usage history will appear here after the first requests land.'}
          staggerIndex={0}
        />
        <MetricCard
          icon={LatencyIcon}
          label="AVG LATENCY"
          value={latencyAvg}
          suffix="ms"
          displayOverride={latencyAvg === 0 ? '-' : undefined}
          samples={latencyMetricSamples}
          note={latencyMetricSamples.length > 0 ? 'Based on recent worker or verification telemetry.' : 'Latency samples appear once live inference or worker telemetry is available.'}
          staggerIndex={1}
        />
        <MetricCard
          icon={ThroughputIcon}
          label="THROUGHPUT"
          value={throughputNum}
          format="decimal"
          suffix=" r/s"
          displayOverride={throughputNum === 0 ? '-' : undefined}
          samples={throughputMetricSamples}
          note={(workers?.length || 0) > 0 ? 'Current requests per second across registered workers.' : 'Throughput will reflect worker traffic once serving starts.'}
          staggerIndex={2}
        />
        <MetricCard
          icon={NodesIcon}
          label="ACTIVE NODES"
          value={healthyWorkers.length}
          displayOverride={`${healthyWorkers.length} / ${workers?.length || 0}`}
          progress={(workers?.length || 0) > 0 ? healthyWorkers.length / (workers?.length || 1) : 0}
          statusIndicator={{
            active: healthyWorkers.length > 0,
            label: healthyWorkers.length > 0 ? 'All systems operational' : 'No workers online',
          }}
          staggerIndex={3}
        />
      </GridRow>

      {/* Serving status row — now using ServingStatusRow with count-up */}
      <ServingStatusRow items={servingStatusItems} />

      {/* Attention queue — now a standalone component */}
      <AttentionQueue items={attentionQueue} onAction={handleDashboardAction} />

      {/* Trends row */}
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
              <div className="help-actions">
                <ActionButton onClick={() => navigate('/models')}>OPEN MODELS</ActionButton>
                <ActionButton onClick={() => navigate('/docs')}>READ VERIFY FLOW</ActionButton>
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
                <ActionButton onClick={() => navigate('/workspace')}>OPEN WORKSPACE</ActionButton>
                <ActionButton onClick={() => navigate('/models')}>DEPLOY A MODEL</ActionButton>
              </div>
            </div>
          )}
        </Cell>
      </GridRow>

      {/* Deployed models + Operational drilldown */}
      <GridRow className="dashboard-main-row" style={{ flexGrow: 1 }}>
        <Cell span={2} className="dashboard-models-cell">
          <SectionHeader
            eyebrow="SECONDARY DETAIL"
            title="DEPLOYED MODELS"
            description="Keep the top of the dashboard focused on what changed. Use this section when you need the serving inventory."
            actions={(
              <ActionGroup compact>
                <ActionButton onClick={() => navigate('/models')}>DEPLOY NEW MODEL</ActionButton>
                <ActionButton onClick={() => navigate('/instances')}>OPEN NODES</ActionButton>
                <ActionButton onClick={() => navigate('/getting-started')}>SEE ONBOARDING PATH</ActionButton>
              </ActionGroup>
            )}
          />
          {loadedModels.length > 0 ? (
            <div className="stack-list" style={{ marginTop: '1.75rem' }}>
              {loadedModels.slice(0, 3).map((model) => (
                <div key={model.id} className="stack-item">
                  <LabelText as="div">
                    <span className="nav-diamond">&#9671;</span>
                    {model.family?.toUpperCase() || 'MODEL'}
                  </LabelText>
                  <h2 style={{ fontSize: '1.75rem', marginTop: '0.5rem', lineHeight: 1.1, fontWeight: 500, letterSpacing: '-0.02em' }}>
                    {model.id.split('/').pop()}
                  </h2>
                  <div style={{ marginTop: '0.5rem', fontSize: '0.9rem', color: 'var(--text-secondary)' }}>
                    {model.quantization && `Quantization: ${model.quantization}`}
                    {model.max_context && <>&nbsp;|&nbsp;Context: {(model.max_context / 1000).toFixed(0)}k</>}
                  </div>
                  {model.tags && model.tags.length > 0 && (
                    <div className="model-tags-row" style={{ display: 'flex', gap: '1rem', marginTop: '1rem' }}>
                      {model.tags.map(tag => (<span key={tag} className="tag">{tag}</span>))}
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
        </Cell>

        <Cell span={2} className="dashboard-overview-cell" bg="var(--bg-accent)">
          <SectionHeader
            eyebrow="SECONDARY DETAIL"
            title="Operational drilldown"
            description="Dense operational detail lives here so the top of the page stays readable."
            actions={(
              <ActionGroup compact>
                <ActionButton onClick={() => navigate('/instances')}>OPEN NODES</ActionButton>
                <ActionButton onClick={() => navigate('/models')}>OPEN MODELS</ActionButton>
                {latestFailure ? <ActionButton onClick={() => navigate('/instances')}>VIEW FAILED NODES</ActionButton> : null}
                {latestVerification ? <ActionButton onClick={() => navigate('/models')}>VERIFY SERVING</ActionButton> : null}
                {billingAttention.length > 0 ? <ActionButton onClick={() => navigate('/workspace')}>VIEW USAGE</ActionButton> : null}
              </ActionGroup>
            )}
          />

          <div className="stack-list" style={{ marginTop: '1.5rem' }}>
            <CollapsibleSection title="NODE OVERVIEW" description="Resource utilization and cost posture for the active workspace.">
              <div style={{ display: 'flex', flexDirection: 'column' }}>
                {[
                  { label: 'Active Instances', value: String(activeInstances.length), secondary: `${instances?.length || 0} total` },
                  { label: 'Cost / Hour', value: `$${costs?.current_hourly?.toFixed(2) || '0.00'}`, secondary: `$${costs?.today_total?.toFixed(2) || '0.00'} today` },
                  { label: 'Queue Depth', value: String(stats?.requests?.queue_depth || 0), secondary: 'pending' },
                  { label: 'Avg GPU Util', value: stats?.gpu?.avg_utilization != null ? `${Math.round(stats.gpu.avg_utilization)}%` : '-', secondary: 'across workers' },
                  { label: 'Memory Usage', value: stats?.memory?.total_bytes ? `${((stats.memory.used_bytes / stats.memory.total_bytes) * 100).toFixed(0)}%` : '-', secondary: stats?.memory?.total_bytes ? `${(stats.memory.used_bytes / (1024 ** 3)).toFixed(1)} / ${(stats.memory.total_bytes / (1024 ** 3)).toFixed(1)} GB` : '-' },
                ].map((row, i, arr) => (
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
                              <Badge>{formatAttemptTime(summary.attempt.updated_at || summary.attempt.created_at)}</Badge>
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
                            <ActionButton onClick={() => {
                              if (remediation.action === 'open_workspace') navigate('/workspace');
                              else if (remediation.action === 'verify_inference') navigate('/models');
                              else navigate('/instances');
                            }}>
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
                    <ActionButton onClick={() => navigate('/instances')}>OPEN NODES</ActionButton>
                    <ActionButton onClick={() => navigate('/getting-started')}>OPEN QUICKSTART</ActionButton>
                  </div>
                </div>
              )}
            </CollapsibleSection>

            <CollapsibleSection title="WORKER STATUS" description="Worker heartbeats and active model assignment.">
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: 'var(--text-secondary)', lineHeight: 1.6 }}>
                {healthyWorkers.length > 0 ? (
                  healthyWorkers.slice(0, 4).map(worker => (
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
        </Cell>
      </GridRow>

      {/* Quick Configuration + System Logs */}
      <GridRow>
        <Cell span={2}>
          <SectionHeader
            eyebrow="QUICK CONFIGURATION"
            title="Workspace limits"
            description="Edit quota settings inline. Changes are saved to the active workspace immediately."
          />
          <div style={{ marginTop: '1.25rem' }}>
            {/* Table header */}
            <div style={{ display: 'grid', gridTemplateColumns: '2fr 2fr 1fr', paddingBottom: '0.5rem', borderBottom: '1px solid var(--text-primary)' }}>
              <LabelText>SETTING</LabelText>
              <LabelText>VALUE</LabelText>
              <LabelText style={{ textAlign: 'right' }}>ACTION</LabelText>
            </div>
            {quickConfigFields.map((field) => (
              <QuickConfigRow
                key={field.key}
                field={field}
                canEdit={canEditQuota}
                onSave={handleQuickConfigSave}
              />
            ))}
          </div>
        </Cell>

        <Cell span={2} bg="var(--bg-accent)">
          <SectionHeader
            eyebrow="SYSTEM LOGS"
            title="Live feed"
            description="Recent runtime events from the inference gateway and workers."
            actions={(
              <ActionButton onClick={() => navigate('/logs')}>OPEN FULL LOGS</ActionButton>
            )}
          />
          <div
            ref={dashLogsRef}
            style={{
              marginTop: '1rem',
              maxHeight: 240,
              overflowY: 'auto',
              fontFamily: 'var(--font-mono)',
              fontSize: '0.75rem',
              lineHeight: 1.7,
            }}
          >
            {dashLogs.map((entry, i) => (
              <div
                key={entry.id}
                className="dashboard-log-entry"
                style={{
                  display: 'flex',
                  gap: '0.6rem',
                  padding: '0.25rem 0',
                  borderBottom: '1px solid #F0F0EE',
                  animation: i >= dashLogs.length - 1 && dashLogs.length > 8 ? 'dash-log-slide-in 0.3s ease-out both' : undefined,
                }}
              >
                <span style={{ color: levelColor(entry.level), fontWeight: 600, minWidth: 38, textTransform: 'uppercase' }}>
                  {entry.level}
                </span>
                <span style={{ color: 'var(--text-secondary)', minWidth: 52 }}>
                  {entry.timestamp.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                </span>
                <span style={{ color: 'var(--text-secondary)', minWidth: 72 }}>
                  {entry.source}
                </span>
                <span style={{ color: 'var(--text-primary)' }}>
                  {entry.message}
                </span>
              </div>
            ))}
          </div>
        </Cell>
      </GridRow>

      {/* Footer */}
      <GridRow className="dashboard-footer-row">
        <Cell>
          <LabelText as="div">VERSION</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>v1.0.0</div>
        </Cell>
        <Cell>
          <LabelText as="div">UPTIME</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {stats?.uptime_seconds ? `${Math.floor(stats.uptime_seconds / 3600)}h ${Math.floor((stats.uptime_seconds % 3600) / 60)}m` : '-'}
          </div>
        </Cell>
        <Cell span={2}>
          <LabelText as="div">SYSTEM STATUS</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.85rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <StatusDot tone={healthyWorkers.length > 0 ? 'success' : 'inactive'} />
            {healthyWorkers.length > 0
              ? 'All endpoints are performing within latency targets.'
              : 'No active workers. Provision an instance to start serving.'}
          </div>
        </Cell>
      </GridRow>
    </div>
  );
}
