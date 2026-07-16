import { ActionButton, Badge, Cell, GridRow, LabelText } from '../shared';
import { ActionGroup } from '../ActionGroup';
import { MetadataList } from '../MetadataList';
import { SectionHeader } from '../SectionHeader';
import type { LiveWorkspaceOperations } from '../../lib/liveWorkspaceOperations';
import type { OnboardingItem } from '../../lib/onboardingChecklist';
import type { WorkspaceMaturity } from '../../lib/workspaceMaturity';

type MetadataValueItem = {
  label: string;
  value: string;
  mono?: boolean;
};

export function DashboardOverviewRow({
  workspaceMaturity,
  workspaceSnapshotItems,
  onWorkspaceMaturityAction,
  onOpenWorkspace,
  liveWorkspaceOperations,
  liveOperationsItems,
  onOpenNodes,
  onOpenModels,
  isNewWorkspace,
  checklistCompletedCount,
  firstWorkspaceChecklist,
  onChecklistAction,
}: {
  workspaceMaturity: WorkspaceMaturity;
  workspaceSnapshotItems: MetadataValueItem[];
  onWorkspaceMaturityAction: () => void;
  onOpenWorkspace: () => void;
  liveWorkspaceOperations: LiveWorkspaceOperations;
  liveOperationsItems: MetadataValueItem[];
  onOpenNodes: () => void;
  onOpenModels: () => void;
  isNewWorkspace: boolean;
  checklistCompletedCount: number;
  firstWorkspaceChecklist: OnboardingItem[];
  onChecklistAction: (action: OnboardingItem['action']) => void;
}) {
  return (
    <GridRow>
      <Cell span={2} className="dashboard-maturity-cell">
        <SectionHeader
          eyebrow="WORKSPACE STATE"
          title={workspaceMaturity.headline}
          description={workspaceMaturity.detail}
          badge={<Badge tone={workspaceMaturity.tone || undefined}>{workspaceMaturity.label}</Badge>}
          actions={(
            <ActionGroup compact>
              <ActionButton onClick={onWorkspaceMaturityAction}>
                {workspaceMaturity.actionLabel}
              </ActionButton>
              {workspaceMaturity.state !== 'serving_verified' && workspaceMaturity.action !== 'open_workspace' && (
                <ActionButton onClick={onOpenWorkspace}>OPEN WORKSPACE</ActionButton>
              )}
            </ActionGroup>
          )}
        />
        <div style={{ marginTop: '1.25rem' }}>
          <MetadataList items={workspaceSnapshotItems} columns={3} />
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
                  <ActionButton onClick={onOpenNodes}>OPEN NODES</ActionButton>
                  <ActionButton onClick={onOpenModels}>OPEN MODELS</ActionButton>
                </ActionGroup>
              )}
            />
            <div style={{ marginTop: '1.25rem' }}>
              <MetadataList items={liveOperationsItems} columns={2} />
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
                    <ActionButton style={{ marginTop: '0.85rem' }} onClick={() => onChecklistAction(item.action)}>
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
  );
}
