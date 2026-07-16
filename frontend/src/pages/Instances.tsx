import { Suspense } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { GridRow, Cell, LabelText, ActionButton } from '../components/shared';
import { InstancesSkeleton } from '../components/skeletons';
import { deriveNodeIncident } from '../lib/instanceIncidents';
import { getInstanceReadiness } from '../lib/instanceReadiness';
import { useDeploymentAttempts } from '../hooks/useDeploymentApi';
import { useInstances, useOfferings, useProviders } from '../hooks/useInfrastructureApi';
import { useWorkers } from '../hooks/useRuntimeApi';
import { useIsMobile } from '../hooks/useIsMobile';
import { InstanceMobileCard } from '../components/InstanceMobileCard';
import { useAuthSession } from '../lib/auth-context';
import { formatShortTimestamp, formatLatency } from '../lib/formatting';
import { instanceStatusClass, instanceStatusLabel } from '../lib/labels';
import { lazyWithRetry } from '../lib/lazyWithRetry';
import { DeploymentHistorySection, LatestDeploymentBanner } from '../components/instances/DeploymentPanels';
import { InstanceIncidentActions, ModelDrilldownPanel } from '../components/instances/IncidentPanels';
import { InfrastructureSidebar } from '../components/instances/InfrastructureSidebar';
import { InstancesMetricsRow, NodeOverviewPanel } from '../components/instances/OverviewPanels';
import { InstancesFooter } from '../components/instances/InstancesFooter';
import { InstanceActions, InstanceRow } from '../components/instances/InstanceRow';
import { useInstanceDeploymentFlow } from '../hooks/useInstanceDeploymentFlow';
import { useInstancesInventoryState } from '../hooks/useInstancesInventoryState';
import { useInstancesScalingState } from '../hooks/useInstancesScalingState';
import { useInstancesViewState } from '../hooks/useInstancesViewState';
import { useProvisionModalState } from '../hooks/useProvisionModalState';

const LazyProvisionModal = lazyWithRetry(
  () => import('../components/instances/ProvisionModal').then((module) => ({ default: module.ProvisionModal })),
  'instances-provision-modal',
);

// Aliases for backwards-compatible call sites in this file
const getStatusClass = instanceStatusClass;
const getStatusLabel = instanceStatusLabel;
const formatAttemptTime = (value: string) => formatShortTimestamp(value) ?? value;
const formatVerificationLatency = formatLatency;

export function Instances() {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const isMobile = useIsMobile(900);
  const { session } = useAuthSession();
  const workspaceID = session?.workspace?.id;
  const role = session?.key?.role ?? 'user';
  const { data: instances, isLoading } = useInstances();
  const { data: offerings } = useOfferings();
  const { data: providers } = useProviders();
  const { data: workers } = useWorkers();
  const { data: deploymentAttempts = [] } = useDeploymentAttempts(workspaceID);

  const {
    drilldownModel,
    drilldownFocus,
    drilldownModelLabel,
    filteredInstances,
    healthyWorkers,
    runningCount,
    setStatusFilter,
    statusFilter,
    totalCostPerHour,
    totalGpuUtil,
    totalInstanceCount,
    totalMemTotal,
    totalMemUsed,
  } = useInstancesViewState({
    searchParams,
    instances,
    workers,
  });
  const {
    showProvisionModal,
    preselectedModel,
    provisionDraft,
    openFreshProvisionModal,
    openRetryModal,
    closeProvisionModal,
    handleProvisioned,
    handleProvisionFailed,
    openWorkspaceFromModal,
  } = useProvisionModalState({
    searchParams,
    setSearchParams,
    navigate,
    onProvisionedSuccess: () => setStatusFilter('active'),
  });
  const {
    configuredProviders,
    connectedProviders,
    providerRail,
    providerSummary,
    provisioningState,
    visibleOfferings,
    visibleProviderStatuses,
  } = useInstancesInventoryState({
    role,
    workspaceID,
    providers,
    offerings,
    filteredInstances,
  });
  const { scaling } = useInstancesScalingState();

  const focusInstance = (instanceID: string) => {
    const target = document.getElementById(`instance-row-${instanceID}`);
    if (!target) return;
    target.scrollIntoView({ behavior: 'smooth', block: 'center' });
  };

  const {
    deploymentHistory,
    latestDeployment,
    deploymentSummaryByInstanceID,
    incidentRows,
    verifyingAttemptID,
    runInferenceVerification,
    handleRemediation,
  } = useInstanceDeploymentFlow({
    workspaceID,
    deploymentAttempts,
    instances,
    workers,
    filteredInstances,
    onOpenWorkspace: () => navigate('/workspace'),
    onRetry: openRetryModal,
    onFocusInstance: focusInstance,
  });

  if (isLoading) return <InstancesSkeleton />;

  return (
    <div className="instances-page animate-fade-in">
      {latestDeployment && (
        <LatestDeploymentBanner
          latestDeployment={latestDeployment}
          verifyingAttemptID={verifyingAttemptID}
          onRemediation={handleRemediation}
          onRetry={openRetryModal}
          formatAttemptTime={formatAttemptTime}
          formatVerificationLatency={formatVerificationLatency}
        />
      )}

      <GridRow>
        <Cell span={4}>
          <div className="help-callout">
            <LabelText as="div">NODE STATUS GUIDE</LabelText>
            <div className="help-callout-copy">
              <strong>Connected inventory</strong> means the workspace or local provider path can return live status. <strong>Serving verified</strong> means the worker heartbeat is fresh and runtime looks ready. <strong>Inference verified</strong> means a real chat-completions request passed. Treat the latest deployment banner as the fastest path from provisioned node to confirmed serving.
            </div>
            <div className="help-actions">
              <ActionButton onClick={() => navigate('/workspace')}>OPEN WORKSPACE</ActionButton>
              <ActionButton onClick={() => navigate('/docs')}>READ DEPLOYMENT DOCS</ActionButton>
            </div>
          </div>
        </Cell>
      </GridRow>

      {drilldownModel && (
        <ModelDrilldownPanel
          drilldownModelLabel={drilldownModelLabel}
          drilldownFocus={drilldownFocus}
          filteredInstanceCount={filteredInstances.length}
          incidentRows={incidentRows}
          verifyingAttemptID={verifyingAttemptID}
          onClear={() => setSearchParams({}, { replace: true })}
          onOpenModels={() => navigate('/models')}
          onFocusInstance={focusInstance}
          onVerify={(summary) => void runInferenceVerification(summary)}
          onRetry={openRetryModal}
        />
      )}

      <InstancesMetricsRow
        filteredInstanceCount={filteredInstances.length}
        totalInstanceCount={totalInstanceCount}
        totalGpuUtil={totalGpuUtil}
        totalMemUsed={totalMemUsed}
        totalMemTotal={totalMemTotal}
        runningCount={runningCount}
      />

      {/* Main Content Row */}
      <GridRow className="instances-main-row" style={{ flexGrow: 1 }}>
        <NodeOverviewPanel
          statusFilter={statusFilter}
          onStatusFilterChange={setStatusFilter}
          drilldownModel={drilldownModel}
          drilldownFocus={drilldownFocus}
          drilldownModelLabel={drilldownModelLabel}
          filteredInstanceCount={filteredInstances.length}
          provisioningState={provisioningState}
          isMobile={isMobile}
          onClearModelFilter={() => setSearchParams({}, { replace: true })}
          onEmptyPrimaryAction={() => {
            if (drilldownModel) {
              setSearchParams({}, { replace: true });
              return;
            }
            if (provisioningState) {
              navigate('/workspace');
              return;
            }
            openFreshProvisionModal();
          }}
          onOpenModels={() => navigate('/models')}
          onOpenQuickstart={() => navigate('/getting-started')}
          onProvisionNewNode={openFreshProvisionModal}
          mobileContent={(
            <div className="mobile-data-list">
              {filteredInstances.map((instance) => {
                const summary = deploymentSummaryByInstanceID.get(instance.id) || null;
                return (
                  <InstanceMobileCard
                    key={instance.id}
                    anchorId={`instance-row-${instance.id}`}
                    instance={instance}
                    statusClass={getStatusClass(instance.status)}
                    statusLabel={getStatusLabel(instance.status)}
                    readiness={getInstanceReadiness(instance, workers)}
                    incident={deriveNodeIncident(instance, workers, summary) || undefined}
                    actions={(
                      <InstanceActions
                        instance={instance}
                        compact
                        incidentActions={(
                          <InstanceIncidentActions
                            instance={instance}
                            summary={summary}
                            verifyingAttemptID={verifyingAttemptID}
                            compact
                            onVerify={(targetSummary) => void runInferenceVerification(targetSummary)}
                            onRetry={openRetryModal}
                          />
                        )}
                      />
                    )}
                  />
                );
              })}
            </div>
          )}
          desktopContent={(
            <div className="responsive-scroll-x">
              <table className="data-table responsive-scroll-x-content">
                <thead>
                  <tr>
                    <th scope="col">NODE ID</th>
                    <th scope="col">STATUS</th>
                    <th scope="col">COST</th>
                    <th scope="col">ENDPOINT</th>
                    <th scope="col" style={{ textAlign: 'right' }}>ACTIONS</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredInstances.map((instance) => {
                    const summary = deploymentSummaryByInstanceID.get(instance.id) || null;
                    return (
                      <InstanceRow
                        key={instance.id}
                        instance={instance}
                        workers={workers}
                        highlighted={instance.id === latestDeployment?.attempt.instance_id}
                        incident={deriveNodeIncident(instance, workers, summary)}
                        incidentActions={(
                          <InstanceIncidentActions
                            instance={instance}
                            summary={summary}
                            verifyingAttemptID={verifyingAttemptID}
                            onVerify={(targetSummary) => void runInferenceVerification(targetSummary)}
                            onRetry={openRetryModal}
                          />
                        )}
                      />
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        />

        {/* Sidebar */}
        <Cell className="instances-sidebar-cell" bg="var(--bg-accent)">
          <InfrastructureSidebar
            providerRail={providerRail}
            providerStatuses={visibleProviderStatuses}
            configuredProviders={configuredProviders}
            healthyWorkers={healthyWorkers}
            connectedProviderCount={connectedProviders.length}
            scaling={scaling}
          />
        </Cell>
      </GridRow>

      <DeploymentHistorySection
        deploymentHistory={deploymentHistory}
        latestAttemptID={latestDeployment?.attempt.id || null}
        verifyingAttemptID={verifyingAttemptID}
        onRemediation={handleRemediation}
        onRetry={openRetryModal}
        onNewAttempt={openFreshProvisionModal}
        renderInstanceActions={(instance) => <InstanceActions instance={instance} compact />}
        formatAttemptTime={formatAttemptTime}
        formatVerificationLatency={formatVerificationLatency}
      />

      <InstancesFooter
        providerSummary={providerSummary}
        totalCostPerHour={totalCostPerHour}
      />

      <Suspense fallback={null}>
        <LazyProvisionModal
          isOpen={showProvisionModal}
          onClose={closeProvisionModal}
          onProvisioned={handleProvisioned}
          onProvisionFailed={handleProvisionFailed}
          onOpenWorkspace={openWorkspaceFromModal}
          offerings={visibleOfferings}
          preselectedModel={preselectedModel}
          initialDraft={provisionDraft}
          providerStatuses={visibleProviderStatuses}
          configuredProviders={configuredProviders}
        />
      </Suspense>
    </div>
  );
}
