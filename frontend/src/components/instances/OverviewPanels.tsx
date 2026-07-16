import type { ReactNode } from 'react';

import { ActionButton, Cell, GridRow, LabelText, ProgressBar, StatusDot } from '../shared';

type ProvisioningState = {
  title: string;
  detail: string;
  action: string;
} | null;

export function InstancesMetricsRow({
  filteredInstanceCount,
  totalInstanceCount,
  totalGpuUtil,
  totalMemUsed,
  totalMemTotal,
  runningCount,
}: {
  filteredInstanceCount: number;
  totalInstanceCount: number;
  totalGpuUtil: number;
  totalMemUsed: number;
  totalMemTotal: number;
  runningCount: number;
}) {
  const hasRunningNodes = runningCount > 0;

  return (
    <GridRow className="instances-metrics-row">
      <Cell style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
        <LabelText as="div" icon={
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
            <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
            <line x1="8" y1="21" x2="16" y2="21" /><line x1="12" y1="17" x2="12" y2="21" />
          </svg>
        }>TOTAL INSTANCES</LabelText>
        <div className="value-text">{filteredInstanceCount}</div>
        <div style={{ marginTop: '1rem', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
          {totalInstanceCount} total
        </div>
      </Cell>
      <Cell style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
        <LabelText as="div" icon={
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M12 2v20M2 12h20" />
          </svg>
        }>AVG GPU UTIL</LabelText>
        <div className="value-text">{totalGpuUtil}%</div>
        <ProgressBar value={totalGpuUtil} />
      </Cell>
      <Cell style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
        <LabelText as="div" icon={
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
            <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
          </svg>
        }>MEMORY USAGE</LabelText>
        <div className="value-text">
          {totalMemTotal > 0 ? `${(totalMemUsed / 1073741824).toFixed(1)} / ${(totalMemTotal / 1073741824).toFixed(1)} GB` : '-'}
        </div>
        <ProgressBar value={totalMemTotal > 0 ? (totalMemUsed / totalMemTotal * 100) : 0} />
      </Cell>
      <Cell style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
        <LabelText as="div" icon={
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
          </svg>
        }>STATUS</LabelText>
        <div className="value-text" style={{ fontSize: '1.25rem' }}>
          {runningCount} Running
        </div>
        <div style={{ marginTop: '1rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <StatusDot tone={hasRunningNodes ? 'success' : 'inactive'} />
          <span style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
            {hasRunningNodes ? 'Operational' : 'No active nodes'}
          </span>
        </div>
      </Cell>
    </GridRow>
  );
}

export function NodeOverviewPanel({
  statusFilter,
  onStatusFilterChange,
  drilldownModel,
  drilldownFocus,
  drilldownModelLabel,
  filteredInstanceCount,
  provisioningState,
  isMobile,
  onClearModelFilter,
  onEmptyPrimaryAction,
  onOpenModels,
  onOpenQuickstart,
  onProvisionNewNode,
  mobileContent,
  desktopContent,
}: {
  statusFilter: string;
  onStatusFilterChange: (value: string) => void;
  drilldownModel: string | null;
  drilldownFocus: string | null;
  drilldownModelLabel: string;
  filteredInstanceCount: number;
  provisioningState: ProvisioningState;
  isMobile: boolean;
  onClearModelFilter: () => void;
  onEmptyPrimaryAction: () => void;
  onOpenModels: () => void;
  onOpenQuickstart: () => void;
  onProvisionNewNode: () => void;
  mobileContent: ReactNode;
  desktopContent: ReactNode;
}) {
  return (
    <Cell className="instances-list-cell" span={3}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '2rem' }}>
        <LabelText as="div">NODE OVERVIEW</LabelText>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <select
            className="filter-select"
            value={statusFilter}
            onChange={(event) => onStatusFilterChange(event.target.value)}
          >
            <option value="active">Active</option>
            <option value="running">Running</option>
            <option value="stopped">Stopped</option>
            <option value="all">All</option>
          </select>
          {drilldownModel && (
            <ActionButton onClick={onClearModelFilter}>
              CLEAR MODEL FILTER
            </ActionButton>
          )}
        </div>
      </div>

      {filteredInstanceCount === 0 ? (
        <div style={{ textAlign: 'center', padding: '4rem 0', color: 'var(--text-secondary)' }}>
          <div style={{ fontSize: '0.9rem', marginBottom: '0.75rem' }}>
            {drilldownModel
              ? `No ${drilldownFocus === 'degraded' ? 'degraded ' : ''}instances found for ${drilldownModelLabel}`
              : provisioningState?.title || 'No instances found'}
          </div>
          <div style={{ maxWidth: '34rem', margin: '0 auto 1.25rem', lineHeight: 1.6 }}>
            {drilldownModel
              ? 'This model does not currently match the selected node view. Clear the drilldown to return to the full cluster inventory.'
              : provisioningState?.detail || 'Provision your first node to start serving models from this workspace.'}
          </div>
          <div className="help-actions" style={{ justifyContent: 'center' }}>
            <ActionButton onClick={onEmptyPrimaryAction}>
              {drilldownModel ? 'CLEAR DRILLDOWN' : provisioningState?.action || 'PROVISION NEW NODE'}
            </ActionButton>
            <ActionButton onClick={onOpenModels}>OPEN MODELS</ActionButton>
            <ActionButton onClick={onOpenQuickstart}>OPEN QUICKSTART</ActionButton>
          </div>
        </div>
      ) : isMobile ? mobileContent : desktopContent}

      <ActionButton style={{ marginTop: '2rem' }} onClick={onProvisionNewNode}>
        PROVISION NEW NODE
      </ActionButton>
    </Cell>
  );
}
