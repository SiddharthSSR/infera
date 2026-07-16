import type { ReactNode } from 'react';
import { toast } from 'sonner';

import { ActionButton, Badge, StatusDot } from '../shared';
import { formatGPUDisplayName, instanceStatusClass, instanceStatusLabel } from '../../lib/labels';
import { getInstanceReadiness } from '../../lib/instanceReadiness';
import type { NodeIncident } from '../../lib/instanceIncidents';
import { useStartInstance, useStopInstance, useTerminateInstance } from '../../hooks/useInfrastructureApi';
import type { Instance, Worker } from '../../types';

function useInstanceActions(instance: Instance) {
  const terminateMutation = useTerminateInstance();
  const startMutation = useStartInstance();
  const stopMutation = useStopInstance();
  const isLoading = terminateMutation.isPending || startMutation.isPending || stopMutation.isPending;

  const handleStart = async () => {
    try {
      await startMutation.mutateAsync(instance.id);
      toast.success('Instance started');
    } catch (err) {
      console.error('Failed to start instance', err);
      toast.error(err instanceof Error ? err.message : 'Failed to start');
    }
  };

  const handleStop = async () => {
    try {
      await stopMutation.mutateAsync(instance.id);
      toast.success('Instance stopped');
    } catch (err) {
      console.error('Failed to stop instance', err);
      toast.error(err instanceof Error ? err.message : 'Failed to stop');
    }
  };

  const handleTerminate = async () => {
    if (!confirm('Terminate this instance?')) return;
    try {
      await terminateMutation.mutateAsync(instance.id);
      toast.success('Terminated');
    } catch (err) {
      console.error('Failed to terminate instance', err);
      toast.error('Failed to terminate');
    }
  };

  return { isLoading, handleStart, handleStop, handleTerminate };
}

export function InstanceActions({
  instance,
  compact = false,
  incidentActions,
}: {
  instance: Instance;
  compact?: boolean;
  incidentActions?: ReactNode;
}) {
  const { isLoading, handleStart, handleStop, handleTerminate } = useInstanceActions(instance);
  const buttonStyle = compact ? { fontSize: '0.65rem' } : { fontSize: '0.65rem', marginRight: '1rem' };

  return (
    <>
      {incidentActions}
      {instance.status === 'stopped' && (
        <ActionButton style={buttonStyle} disabled={isLoading} onClick={handleStart}>START</ActionButton>
      )}
      {instance.status === 'running' && (
        <ActionButton style={buttonStyle} disabled={isLoading} onClick={handleStop}>STOP</ActionButton>
      )}
      {instance.status !== 'terminating' && instance.status !== 'terminated' && (
        <ActionButton
          variant="destructive"
          style={{ fontSize: '0.65rem' }}
          disabled={isLoading}
          onClick={handleTerminate}
        >
          TERMINATE
        </ActionButton>
      )}
    </>
  );
}

export function InstanceRow({
  instance,
  workers,
  highlighted,
  incident,
  incidentActions,
}: {
  instance: Instance;
  workers: Worker[] | undefined;
  highlighted?: boolean;
  incident?: NodeIncident | null;
  incidentActions?: ReactNode;
}) {
  const statusClass = instanceStatusClass(instance.status);
  const statusLabel = instanceStatusLabel(instance.status);
  const readiness = getInstanceReadiness(instance, workers);

  return (
    <tr id={`instance-row-${instance.id}`} style={{ borderBottom: '1px solid #EEEEEC', background: incident?.tone === 'warning' ? 'rgba(249, 168, 37, 0.06)' : highlighted ? 'rgba(244, 242, 238, 0.7)' : 'transparent' }}>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle' }}>
        <div className="mono">{instance.name || instance.id.slice(0, 16)}</div>
        <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginTop: 2 }}>
          {instance.gpu_count}x {formatGPUDisplayName(instance.gpu_type)}
          {instance.models && instance.models.length > 0 && (
            <> &middot; {instance.models[0].split('/').pop()}</>
          )}
        </div>
      </td>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: '0.85rem' }}>
          <StatusDot tone={statusClass as 'success' | 'warning' | 'error' | 'inactive' | undefined} />
          {statusLabel}
        </div>
        <div style={{ marginTop: '0.45rem', display: 'flex', alignItems: 'center', gap: '0.45rem', flexWrap: 'wrap' }}>
          <Badge tone={readiness.tone || undefined}>{readiness.label}</Badge>
        </div>
        <div style={{ marginTop: '0.35rem', fontSize: '0.75rem', color: 'var(--text-secondary)', lineHeight: 1.5, maxWidth: '22rem' }}>
          {readiness.detail}
        </div>
        {incident && (
          <div style={{ marginTop: '0.65rem', maxWidth: '22rem' }}>
            <Badge tone={incident.tone || undefined}>{incident.title}</Badge>
            <div style={{ marginTop: '0.35rem', fontSize: '0.75rem', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
              {incident.detail}
            </div>
          </div>
        )}
      </td>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle' }}>
        <div className="mono">${instance.cost_per_hour.toFixed(2)}/hr</div>
      </td>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle' }}>
        {instance.public_ip ? (
          <div className="mono" style={{ fontSize: '0.8rem' }}>{instance.public_ip}</div>
        ) : (
          <span style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>-</span>
        )}
      </td>
      <td style={{ padding: '1.5rem 0', verticalAlign: 'middle', textAlign: 'right' }}>
        <InstanceActions instance={instance} incidentActions={incidentActions} />
      </td>
    </tr>
  );
}
