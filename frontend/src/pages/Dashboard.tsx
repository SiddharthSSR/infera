import { useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useDeploymentAttempts } from '../hooks/useDeploymentApi';
import { useCosts, useInstances, useProviders } from '../hooks/useInfrastructureApi';
import { useModels, useStats, useWorkers } from '../hooks/useRuntimeApi';
import { GridRow, Cell, LabelText, StatusDot, ActionButton } from '../components/shared';
import { DashboardSkeleton } from '../components/DashboardSkeleton';
import { MetricCard } from '../components/MetricCard';
import { ServingStatusRow, type ServingStatusItem } from '../components/ServingStatusRow';
import { AttentionQueue } from '../components/AttentionQueue';
import { ActionGroup } from '../components/ActionGroup';
import { DashboardLogsPanel } from '../components/dashboard/DashboardLogsPanel';
import { DashboardDeployedModelsPanel } from '../components/dashboard/DashboardDeployedModelsPanel';
import { DashboardOverviewRow } from '../components/dashboard/DashboardOverviewRow';
import { DashboardOperationalDrilldownPanel } from '../components/dashboard/DashboardOperationalDrilldownPanel';
import { QuickConfigurationPanel } from '../components/dashboard/QuickConfigurationPanel';
import { DashboardTrendsRow } from '../components/dashboard/DashboardTrendsRow';
import { SectionHeader } from '../components/SectionHeader';
import { useDashboardLogs } from '../hooks/useDashboardLogs';
import { useDashboardViewState } from '../hooks/useDashboardViewState';
import { useDashboardWorkspaceState } from '../hooks/useDashboardWorkspaceState';
import { useAuthSession } from '../lib/auth-context';

type DashboardAction = 'open_clusters' | 'open_models' | 'open_workspace' | 'verify_now' | 'open_docs' | 'open_api_keys';

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
  const { data: workers, isLoading: loadingWorkers, isError: errorWorkers } = useWorkers(workspaceID);
  const { data: stats, isLoading: loadingStats, isError: errorStats } = useStats();
  const { data: instances, isLoading: loadingInstances } = useInstances();
  const { data: costs, isLoading: loadingCosts } = useCosts();
  const { data: models, isLoading: loadingModels } = useModels();
  const { data: providers, isLoading: loadingProviders } = useProviders();
  const { data: deploymentAttempts = [] } = useDeploymentAttempts(workspaceID);
  const isLoading = loadingWorkers || loadingStats || loadingInstances || loadingCosts || loadingModels || loadingProviders;
  const { dashLogs, dashLogsRef } = useDashboardLogs();
  const {
    quota,
    usageRows,
    workspaceInvites,
    workspaceServiceAccounts,
    canEditQuota,
    handleQuickConfigSave,
  } = useDashboardWorkspaceState({
    workspaceID,
    role,
  });

  const {
    gatewayDown,
    providerGatewayMismatch,
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
  } = useDashboardViewState({
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
  });

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

  const servingStatusItems: ServingStatusItem[] = [
    { label: 'SERVING VERIFIED', value: servingVerifiedCount, description: 'Models that answered a real verification request successfully.', actionLabel: 'OPEN MODELS', onAction: () => navigate('/models') },
    { label: 'VERIFY PENDING', value: servingUnverifiedCount, description: 'Models that look runtime-ready but still need or are awaiting inference verification.', actionLabel: 'VERIFY SERVING', onAction: () => navigate('/models') },
    { label: 'DEGRADED DEPLOYMENTS', value: degradedDeploymentCount, description: 'Recent attempts that failed, lost their node, or are serving with an explicit error signal.', actionLabel: 'VIEW FAILED NODES', onAction: () => navigate('/instances') },
    { label: 'PENDING DEPLOYMENTS', value: pendingDeploymentCount, description: 'Nodes still provisioning, connecting a worker, or loading assigned models.', actionLabel: 'OPEN NODES', onAction: () => navigate('/instances') },
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

      {(workers?.length || 0) === 0 && (
        <GridRow>
          <Cell span={4}>
            <div className="help-callout" role="alert">
              {providerGatewayMismatch?.kind === 'provider_active_without_workers'
                ? `Connected providers report ${providerGatewayMismatch.activeProviderInstances} active instance${providerGatewayMismatch.activeProviderInstances === 1 ? '' : 's'}, but the gateway has no registered workers. Check worker startup, shared authentication, and gateway registration.`
                : 'No healthy inference workers are registered. The gateway is reachable, but inference requests will fail until a worker is restored.'}
            </div>
          </Cell>
        </GridRow>
      )}

      <DashboardOverviewRow
        workspaceMaturity={workspaceMaturity}
        workspaceSnapshotItems={workspaceSnapshotItems.map((item) => ({ ...item, value: String(item.value) }))}
        onWorkspaceMaturityAction={() => handleDashboardAction(workspaceMaturity.action)}
        onOpenWorkspace={() => navigate('/workspace')}
        liveWorkspaceOperations={liveWorkspaceOperations}
        liveOperationsItems={liveOperationsItems.map((item) => ({ ...item, value: String(item.value) }))}
        onOpenNodes={() => navigate('/instances')}
        onOpenModels={() => navigate('/models')}
        isNewWorkspace={isNewWorkspace}
        checklistCompletedCount={checklistCompletedCount}
        firstWorkspaceChecklist={firstWorkspaceChecklist}
        onChecklistAction={handleDashboardAction}
      />

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

      <DashboardTrendsRow
        deploymentTrend={deploymentTrend}
        deploymentHistoryPreview={deploymentHistoryPreview}
        hiddenDeploymentHistoryCount={hiddenDeploymentHistoryCount}
        verificationTrend={verificationTrend}
        usageTrend={usageTrend}
        usageTrendMaxRequests={usageTrendMaxRequests}
        onOpenModels={() => navigate('/models')}
        onOpenDocs={() => navigate('/docs')}
        onOpenWorkspace={() => navigate('/workspace')}
        onDeployModel={() => navigate('/models')}
      />

      {/* Deployed models + Operational drilldown */}
      <GridRow className="dashboard-main-row" style={{ flexGrow: 1 }}>
        <Cell span={2} className="dashboard-models-cell">
          <DashboardDeployedModelsPanel
            loadedModels={loadedModels}
            onDeployModel={() => navigate('/models')}
            onOpenNodes={() => navigate('/instances')}
            onOpenOnboarding={() => navigate('/getting-started')}
          />
        </Cell>

        <Cell span={2} className="dashboard-overview-cell" bg="var(--bg-accent)">
          <DashboardOperationalDrilldownPanel
            latestFailure={latestFailure}
            latestVerification={latestVerification}
            hasBillingAttention={billingAttention.length > 0}
            nodeOverviewRows={nodeOverviewRows}
            recentActivity={recentActivity}
            healthyWorkers={healthyWorkers}
            onOpenNodes={() => navigate('/instances')}
            onOpenModels={() => navigate('/models')}
            onViewUsage={() => navigate('/workspace')}
            onOpenQuickstart={() => navigate('/getting-started')}
            onRemediationAction={(action) => {
              if (action === 'open_workspace') navigate('/workspace');
              else if (action === 'verify_inference') navigate('/models');
              else navigate('/instances');
            }}
          />
        </Cell>
      </GridRow>

      {/* Quick Configuration + System Logs */}
      <GridRow>
        <Cell span={2}>
          <QuickConfigurationPanel
            quickConfigFields={quickConfigFields}
            canEditQuota={canEditQuota}
            onSave={handleQuickConfigSave}
          />
        </Cell>

        <Cell span={2} bg="var(--bg-accent)">
          <DashboardLogsPanel
            dashLogs={dashLogs}
            dashLogsRef={dashLogsRef}
            onOpenLogs={() => navigate('/logs')}
          />
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
