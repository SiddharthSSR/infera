import { ActionButton } from '../shared';
import { SectionHeader } from '../SectionHeader';
import type { DashboardLogEntry } from '../../hooks/useDashboardLogs';

function levelColor(level: DashboardLogEntry['level']): string {
  switch (level) {
    case 'info': return 'var(--color-success)';
    case 'warn': return 'var(--color-warning)';
    case 'error': return 'var(--color-error)';
    case 'debug': return 'var(--text-secondary)';
  }
}

export function DashboardLogsPanel({
  dashLogs,
  dashLogsRef,
  onOpenLogs,
}: {
  dashLogs: DashboardLogEntry[];
  dashLogsRef: React.RefObject<HTMLDivElement>;
  onOpenLogs: () => void;
}) {
  return (
    <>
      <SectionHeader
        eyebrow="SYSTEM LOGS"
        title="Live feed"
        description="Recent runtime events from the inference gateway and workers."
        actions={(
          <ActionButton onClick={onOpenLogs}>OPEN FULL LOGS</ActionButton>
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
    </>
  );
}
