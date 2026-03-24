import type { ReactNode } from 'react';
import type { Instance } from '../types';

export function InstanceMobileCard({
  anchorId,
  instance,
  statusClass,
  statusLabel,
  readiness,
  incident,
  actions,
}: {
  anchorId?: string;
  instance: Instance;
  statusClass: string;
  statusLabel: string;
  readiness?: { label: string; detail: string; tone?: string };
  incident?: { title: string; detail: string; tone?: string };
  actions?: ReactNode;
}) {
  const showIncident = Boolean(
    incident
    && (
      !readiness
      || incident.title !== readiness.label
      || incident.detail !== readiness.detail
    ),
  );

  return (
    <div id={anchorId} className="mobile-data-card">
      <div className="mobile-data-card-header">
        <div>
          <div className="mobile-data-title mono" style={{ fontSize: '0.9rem' }}>
            {instance.name || instance.id.slice(0, 16)}
          </div>
          <div className="mobile-data-subtitle">
            {instance.gpu_count}x {instance.gpu_type.replace('_', ' ')}
            {instance.models && instance.models.length > 0 && <> &middot; {instance.models[0].split('/').pop()}</>}
          </div>
        </div>
        <div className="mobile-status-inline">
          <span className={`status-dot ${statusClass}`} />
          {statusLabel}
        </div>
      </div>
      <div className="mobile-data-meta">
        <div>
          <span className="label-text">COST</span>{' '}
          <span className="mono">${instance.cost_per_hour.toFixed(2)}/hr</span>
        </div>
        {readiness && (
          <div>
            <span className="label-text">READINESS</span>{' '}
            <span className={`badge ${readiness.tone ? `status-${readiness.tone}` : ''}`}>{readiness.label}</span>
            <div style={{ marginTop: '0.35rem', lineHeight: 1.5 }}>{readiness.detail}</div>
          </div>
        )}
        {showIncident && incident && (
          <div>
            <span className="label-text">INCIDENT</span>{' '}
            <span className={`badge ${incident.tone ? `status-${incident.tone}` : ''}`}>{incident.title}</span>
            <div style={{ marginTop: '0.35rem', lineHeight: 1.5 }}>{incident.detail}</div>
          </div>
        )}
        <div>
          <span className="label-text">ENDPOINT</span>{' '}
          <span className="mono">{instance.public_ip || '-'}</span>
        </div>
      </div>
      {actions && <div className="mobile-data-actions">{actions}</div>}
    </div>
  );
}
