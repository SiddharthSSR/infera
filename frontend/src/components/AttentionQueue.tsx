import { SectionHeader } from './SectionHeader';

type AttentionSeverity = 'critical' | 'warning' | 'info';
type AttentionAction = 'open_clusters' | 'open_models' | 'open_workspace' | 'verify_now';

export type AttentionItem = {
  id: string;
  severity: AttentionSeverity;
  title: string;
  detail: string;
  actionLabel: string;
  action: AttentionAction;
  timestamp?: string;
};

function getSeverityClass(severity: AttentionSeverity) {
  switch (severity) {
    case 'critical': return 'dashboard-alert-critical';
    case 'warning': return 'dashboard-alert-warning';
    default: return 'dashboard-alert-info';
  }
}

function getBadgeClass(severity: AttentionSeverity) {
  switch (severity) {
    case 'critical': return 'status-error';
    case 'warning': return 'status-warning';
    default: return 'status-inactive';
  }
}

function formatTime(timestamp?: string) {
  if (!timestamp) return null;
  return new Date(timestamp).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });
}

export function AttentionQueue({
  items,
  onAction,
}: {
  items: AttentionItem[];
  onAction: (action: AttentionAction) => void;
}) {
  return (
    <div className="grid-row dashboard-alerts-row">
      <div className="cell dashboard-alerts-cell" style={{ gridColumn: 'span 4' }}>
        <SectionHeader
          eyebrow="ATTENTION QUEUE"
          title="Priority actions"
          description="The next operational issues that need action now."
          badge={<div className="badge status-inactive">{items.length} OPEN</div>}
        />

        {items.length > 0 ? (
          <div className="dashboard-alert-list" style={{ marginTop: '1.4rem' }}>
            {items.map((item, index) => (
              <div
                key={item.id}
                className={`dashboard-alert-item ${getSeverityClass(item.severity)}`}
                style={{
                  animation: `fade-slide-in 0.3s ease-out ${index * 0.06}s both`,
                }}
              >
                <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '1rem' }}>
                  <div>
                    <div className="dashboard-alert-title-row">
                      <span className={`badge ${getBadgeClass(item.severity)}`}>
                        {item.severity.toUpperCase()}
                      </span>
                      <span style={{ fontSize: '0.95rem', fontWeight: 500 }}>{item.title}</span>
                    </div>
                    <div className="dashboard-summary-text">{item.detail}</div>
                  </div>
                  {item.timestamp && (
                    <div style={{ fontSize: '0.74rem', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-secondary)' }}>
                      {formatTime(item.timestamp)}
                    </div>
                  )}
                </div>
                <button
                  className="action-btn"
                  style={{ marginTop: '0.95rem' }}
                  onClick={() => onAction(item.action)}
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
  );
}
