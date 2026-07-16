import { ActionButton, ControlInput, LabelText, StatusDot } from '../shared';
import { providerStateBadge } from '../../lib/labels';
import { getProviderDisplayName } from '../../lib/providerInventory';
import type { ProviderStatus, Worker } from '../../types';

type ScalingControls = {
  minNodes: number;
  maxNodes: number;
  trigger: number;
  errors: {
    minNodes: string;
    maxNodes: string;
    trigger: string;
  };
  dirty: boolean;
  hasErrors: boolean;
  onMinNodesChange: (value: number) => void;
  onMaxNodesChange: (value: number) => void;
  onTriggerChange: (value: number) => void;
  onApply: () => void;
};

export function InfrastructureSidebar({
  providerRail,
  providerStatuses,
  configuredProviders,
  healthyWorkers,
  connectedProviderCount,
  scaling,
}: {
  providerRail: string[];
  providerStatuses: ProviderStatus[];
  configuredProviders: string[];
  healthyWorkers: Worker[];
  connectedProviderCount: number;
  scaling: ScalingControls;
}) {
  const activeModels = [...new Set(healthyWorkers.flatMap((worker) => worker.models || []))];

  return (
    <>
      <LabelText as="div" style={{ marginBottom: '2rem' }}>WORKSPACE INFRASTRUCTURE</LabelText>

      <div style={{ marginBottom: '2.5rem' }}>
        <LabelText as="div">PROVIDERS</LabelText>
        <div style={{ marginTop: '0.9rem', display: 'grid', gap: '0.65rem' }}>
          {providerRail.map((providerName) => {
            const status = providerStatuses.find((provider) => provider.provider === providerName);
            const configured = configuredProviders.includes(providerName);
            const badge = providerStateBadge(status, configured);

            return (
              <div key={providerName} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '1rem' }}>
                <span style={{ fontSize: '0.85rem' }}>{getProviderDisplayName(providerName)}</span>
                <span className={`badge ${badge.tone ? `status-${badge.tone}` : ''}`}>{badge.label}</span>
              </div>
            );
          })}
        </div>
      </div>

      <div style={{ marginBottom: '2.5rem' }}>
        <LabelText as="div">TOTAL WORKERS</LabelText>
        <div className="mono" style={{ fontSize: '1.25rem', marginTop: '0.5rem' }}>
          {healthyWorkers.length}
        </div>
      </div>

      <div style={{ marginBottom: '2.5rem' }}>
        <LabelText as="div">ACTIVE MODELS</LabelText>
        <div style={{ marginTop: '0.5rem' }}>
          {activeModels.length > 0 ? (
            activeModels.map((model) => (
              <div key={model} style={{ fontSize: '0.85rem', padding: '0.25rem 0' }}>
                {model.split('/').pop()}
              </div>
            ))
          ) : (
            <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)' }}>None</div>
          )}
        </div>
      </div>

      <div style={{ marginTop: '4rem', borderTop: '1px solid var(--border-color)', paddingTop: '2rem' }}>
        <LabelText as="div">PLATFORM HEALTH</LabelText>
        <div style={{ marginTop: '1rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '0.85rem', marginBottom: '0.5rem' }}>
            <span>Gateway</span>
            <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', color: 'var(--color-success)' }}><StatusDot tone="success" />OK</span>
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '0.85rem', marginBottom: '0.5rem' }}>
            <span>Router</span>
            <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', color: 'var(--color-success)' }}><StatusDot tone="success" />OK</span>
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '0.85rem' }}>
            <span>Workers</span>
            <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', color: healthyWorkers.length > 0 ? 'var(--color-success)' : 'var(--color-warning)' }}>
              <StatusDot tone={healthyWorkers.length > 0 ? 'success' : 'warning'} />{healthyWorkers.length > 0 ? 'OK' : 'NONE'}
            </span>
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '0.85rem', marginTop: '0.5rem' }}>
            <span>Providers</span>
            <span style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', color: connectedProviderCount > 0 ? 'var(--color-success)' : 'var(--color-warning)' }}>
              <StatusDot tone={connectedProviderCount > 0 ? 'success' : 'warning'} />{connectedProviderCount > 0 ? `${connectedProviderCount} live` : 'CHECK'}
            </span>
          </div>
        </div>
      </div>

      <div style={{ marginTop: '2.5rem', borderTop: '1px solid var(--border-color)', paddingTop: '2rem' }}>
        <LabelText as="div" style={{ marginBottom: '1.25rem' }}>SCALING CONTROLS</LabelText>

        <div style={{ marginBottom: '1rem' }}>
          <label style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-secondary)', marginBottom: '0.4rem' }}>
            MIN NODES
          </label>
          <ControlInput
            type="number"
            value={scaling.minNodes}
            onChange={(event) => scaling.onMinNodesChange(Number(event.target.value))}
            style={{ width: '100%' }}
          />
          {scaling.errors.minNodes && (
            <div style={{ fontSize: '0.75rem', color: 'var(--color-error)', marginTop: '0.3rem', lineHeight: 1.4 }}>
              {scaling.errors.minNodes}
            </div>
          )}
        </div>

        <div style={{ marginBottom: '1rem' }}>
          <label style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-secondary)', marginBottom: '0.4rem' }}>
            MAX NODES
          </label>
          <ControlInput
            type="number"
            value={scaling.maxNodes}
            onChange={(event) => scaling.onMaxNodesChange(Number(event.target.value))}
            style={{ width: '100%' }}
          />
          {scaling.errors.maxNodes && (
            <div style={{ fontSize: '0.75rem', color: 'var(--color-error)', marginTop: '0.3rem', lineHeight: 1.4 }}>
              {scaling.errors.maxNodes}
            </div>
          )}
        </div>

        <div style={{ marginBottom: '1.25rem' }}>
          <label style={{ display: 'block', fontSize: '0.75rem', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-secondary)', marginBottom: '0.4rem' }}>
            AUTOSCALE TRIGGER (%)
          </label>
          <ControlInput
            type="number"
            value={scaling.trigger}
            onChange={(event) => scaling.onTriggerChange(Number(event.target.value))}
            style={{ width: '100%' }}
          />
          {scaling.errors.trigger && (
            <div style={{ fontSize: '0.75rem', color: 'var(--color-error)', marginTop: '0.3rem', lineHeight: 1.4 }}>
              {scaling.errors.trigger}
            </div>
          )}
        </div>

        <ActionButton
          variant="primary"
          disabled={!scaling.dirty || scaling.hasErrors}
          onClick={scaling.onApply}
          style={{ width: '100%' }}
        >
          APPLY CHANGES
        </ActionButton>
      </div>
    </>
  );
}
