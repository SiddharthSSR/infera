import type { ApiKeyRecord, WorkspaceQuotaRecord } from '../../types';
import { ActionButton, Badge, Cell, ControlInput, ControlSelect, GridRow, LabelText } from '../shared';

type ProviderOptionField = {
  key: string;
  label: string;
  placeholder: string;
  defaultValue?: string;
  required?: boolean;
};

type ConfigurableProvider = {
  id: 'runpod' | 'vastai' | 'e2e';
  name: string;
  endpointPlaceholder: string;
  apiSecretLabel?: string;
  apiSecretPlaceholder?: string;
  optionFields?: ProviderOptionField[];
};

type ProviderHealthRow = {
  id: string;
  name: string;
  config?: {
    configured?: boolean;
    endpoint?: string;
    updated_at: string;
  };
  status?: {
    active_instances?: number;
    account_id?: string;
    capabilities?: {
      known_regions?: string[];
    };
    balance?: number;
  };
  liveState: {
    label: string;
    tone?: string;
    detail: string;
  };
  capabilities: string[];
};

export function WorkspaceOperationsSection({
  canManageProviderConfigs,
  providerHealthRows,
  formatDate,
  onDeleteProviderConfig,
  configurableProviders,
  selectedProvider,
  onSelectedProviderChange,
  providerAPIKey,
  onProviderAPIKeyChange,
  providerAPISecret,
  onProviderAPISecretChange,
  providerEndpoint,
  onProviderEndpointChange,
  selectedProviderMeta,
  providerOptions,
  onProviderOptionChange,
  savingProviderConfig,
  onSaveProviderConfig,
  canViewQuota,
  quota,
  canManageQuota,
  requestLimit,
  onRequestLimitChange,
  tokenLimit,
  onTokenLimitChange,
  enforceHardLimits,
  onEnforceHardLimitsChange,
  savingQuota,
  onSaveQuota,
  canManageKeys,
  serviceAccounts,
  onRevokeServiceAccount,
  onOpenApiKeys,
  newServiceAccountName,
  onNewServiceAccountNameChange,
  newServiceAccountRole,
  onNewServiceAccountRoleChange,
  serviceAccountRoles,
  creatingServiceAccount,
  onCreateServiceAccount,
}: {
  canManageProviderConfigs: boolean;
  providerHealthRows: ProviderHealthRow[];
  formatDate: (value?: string | null) => string;
  onDeleteProviderConfig: (provider: string) => void;
  configurableProviders: ConfigurableProvider[];
  selectedProvider: ConfigurableProvider['id'];
  onSelectedProviderChange: (value: ConfigurableProvider['id']) => void;
  providerAPIKey: string;
  onProviderAPIKeyChange: (value: string) => void;
  providerAPISecret: string;
  onProviderAPISecretChange: (value: string) => void;
  providerEndpoint: string;
  onProviderEndpointChange: (value: string) => void;
  selectedProviderMeta: ConfigurableProvider;
  providerOptions: Record<string, string>;
  onProviderOptionChange: (key: string, value: string) => void;
  savingProviderConfig: boolean;
  onSaveProviderConfig: () => void;
  canViewQuota: boolean;
  quota: WorkspaceQuotaRecord | null;
  canManageQuota: boolean;
  requestLimit: string;
  onRequestLimitChange: (value: string) => void;
  tokenLimit: string;
  onTokenLimitChange: (value: string) => void;
  enforceHardLimits: boolean;
  onEnforceHardLimitsChange: (value: boolean) => void;
  savingQuota: boolean;
  onSaveQuota: () => void;
  canManageKeys: boolean;
  serviceAccounts: ApiKeyRecord[];
  onRevokeServiceAccount: (keyId: string) => void;
  onOpenApiKeys: () => void;
  newServiceAccountName: string;
  onNewServiceAccountNameChange: (value: string) => void;
  newServiceAccountRole: string;
  onNewServiceAccountRoleChange: (value: string) => void;
  serviceAccountRoles: readonly string[];
  creatingServiceAccount: boolean;
  onCreateServiceAccount: () => void;
}) {
  return (
    <GridRow className="workspace-ops-row">
      <Cell span={2} className="workspace-provider-cell">
        <LabelText as="div" style={{ marginBottom: '1.5rem' }}>PROVIDER CONFIGS</LabelText>
        {canManageProviderConfigs ? (
          <>
            <div className="responsive-scroll-x" style={{ marginBottom: '1.5rem' }}>
              <table className="data-table responsive-scroll-x-content">
                <thead>
                  <tr>
                    <th scope="col">PROVIDER</th>
                    <th scope="col">CONFIG</th>
                    <th scope="col">LIVE STATE</th>
                    <th scope="col">ENDPOINT</th>
                    <th scope="col">ACTIVE</th>
                    <th scope="col">UPDATED</th>
                    <th scope="col" style={{ textAlign: 'right' }}>ACTION</th>
                  </tr>
                </thead>
                <tbody>
                  {providerHealthRows.map((provider) => (
                    <tr key={provider.id}>
                      <td>{provider.name}</td>
                      <td><Badge>{provider.config?.configured ? 'CONFIGURED' : 'NOT CONFIGURED'}</Badge></td>
                      <td>
                        <Badge tone={provider.liveState.tone as 'warning' | 'error' | '' | undefined || undefined}>
                          {provider.liveState.label}
                        </Badge>
                      </td>
                      <td className="mono">{provider.config ? (provider.config.endpoint || 'default') : '—'}</td>
                      <td>{provider.status?.active_instances ?? 0}</td>
                      <td>{provider.config ? formatDate(provider.config.updated_at) : '—'}</td>
                      <td style={{ textAlign: 'right' }}>
                        {provider.config ? (
                          <ActionButton variant="destructive" onClick={() => onDeleteProviderConfig(provider.id)}>
                            DELETE
                          </ActionButton>
                        ) : (
                          <span style={{ color: 'var(--text-secondary)', fontSize: '0.8rem' }}>—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="workspace-provider-health-grid" style={{ display: 'grid', gap: '1rem', marginBottom: '1.5rem' }}>
              {providerHealthRows.map((provider) => (
                <div key={`${provider.id}-health`} className="workspace-provider-card">
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: '1rem', alignItems: 'flex-start', flexWrap: 'wrap' }}>
                    <div>
                      <LabelText as="div" style={{ marginBottom: '0.5rem' }}>{provider.name.toUpperCase()}</LabelText>
                      <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                        <Badge>{provider.config?.configured ? 'CONFIGURED' : 'NOT CONFIGURED'}</Badge>
                        <Badge tone={provider.liveState.tone as 'warning' | 'error' | '' | undefined || undefined}>{provider.liveState.label}</Badge>
                      </div>
                    </div>
                    <div className="mono" style={{ color: 'var(--text-secondary)' }}>
                      {provider.status?.account_id || provider.config?.endpoint || (provider.config ? 'default endpoint' : 'not configured')}
                    </div>
                  </div>

                  <div style={{ marginTop: '1rem', color: 'var(--text-secondary)', fontSize: '0.88rem', lineHeight: 1.6 }}>
                    {provider.liveState.detail}
                  </div>

                  <div className="workspace-provider-meta" style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: '0.9rem', marginTop: '1rem' }}>
                    <div>
                      <LabelText as="div">ACTIVE INSTANCES</LabelText>
                      <div className="mono" style={{ marginTop: '0.4rem' }}>{provider.status?.active_instances ?? 0}</div>
                    </div>
                    <div>
                      <LabelText as="div">REGIONS</LabelText>
                      <div style={{ marginTop: '0.4rem', fontSize: '0.88rem', color: 'var(--text-secondary)' }}>
                        {provider.status?.capabilities?.known_regions?.length
                          ? provider.status.capabilities.known_regions.join(', ')
                          : 'Default'}
                      </div>
                    </div>
                    <div>
                      <LabelText as="div">BILLING SIGNAL</LabelText>
                      <div className="mono" style={{ marginTop: '0.4rem' }}>
                        {provider.status?.balance != null ? `$${provider.status.balance.toFixed(2)}` : '—'}
                      </div>
                    </div>
                  </div>

                  <div style={{ marginTop: '1rem', display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                    {provider.capabilities.length > 0 ? (
                      provider.capabilities.map((capability) => (
                        <Badge key={`${provider.id}-${capability}`}>{capability}</Badge>
                      ))
                    ) : (
                      <span style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>Capabilities will appear when live provider status is available.</span>
                    )}
                  </div>
                </div>
              ))}
            </div>

            <div style={{ display: 'grid', gap: '1rem' }}>
              <div>
                <LabelText as="div">PROVIDER</LabelText>
                <ControlSelect value={selectedProvider} onChange={(e) => onSelectedProviderChange(e.target.value as ConfigurableProvider['id'])}>
                  {configurableProviders.map((provider) => (
                    <option key={provider.id} value={provider.id}>{provider.name}</option>
                  ))}
                </ControlSelect>
              </div>
              <div>
                <LabelText as="div">API KEY</LabelText>
                <ControlInput type="password" value={providerAPIKey} onChange={(e) => onProviderAPIKeyChange(e.target.value)} placeholder="Write-only" />
              </div>
              <div>
                <LabelText as="div">{selectedProviderMeta.apiSecretLabel || 'API SECRET'}</LabelText>
                <ControlInput type="password" value={providerAPISecret} onChange={(e) => onProviderAPISecretChange(e.target.value)} placeholder={selectedProviderMeta.apiSecretPlaceholder || 'Optional write-only secret'} />
              </div>
              <div>
                <LabelText as="div">ENDPOINT</LabelText>
                <ControlInput value={providerEndpoint} onChange={(e) => onProviderEndpointChange(e.target.value)} placeholder={selectedProviderMeta.endpointPlaceholder} />
              </div>
              {(selectedProviderMeta.optionFields || []).map((field) => (
                <div key={field.key}>
                  <LabelText as="div">{field.label}</LabelText>
                  <ControlInput
                    value={providerOptions[field.key] || ''}
                    onChange={(e) => onProviderOptionChange(field.key, e.target.value)}
                    placeholder={field.placeholder}
                  />
                </div>
              ))}
              {selectedProvider === 'e2e' && (
                <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem', lineHeight: 1.6 }}>
                  E2E requires an API key, auth token, and the target IAM/team/project identifiers. Leave endpoint blank to use the default TIR API base, and keep location set unless your project is pinned elsewhere.
                </div>
              )}
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                Stored secrets are never shown again after save. Update a provider by submitting a new key or token for the selected provider. Non-secret options reload when you revisit the provider.
              </div>
              <div>
                <ActionButton variant="primary" disabled={savingProviderConfig} onClick={onSaveProviderConfig}>
                  {savingProviderConfig ? 'SAVING...' : 'SAVE PROVIDER CONFIG'}
                </ActionButton>
              </div>
            </div>
          </>
        ) : (
          <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
            Provider configuration is restricted to workspace owners and admins.
          </div>
        )}
      </Cell>

      <Cell span={2} className="workspace-quota-cell">
        <LabelText as="div" style={{ marginBottom: '1.5rem' }}>WORKSPACE QUOTA</LabelText>
        {canViewQuota && quota ? (
          <>
            <div className="workspace-quota-inputs" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem' }}>
              <div>
                <LabelText as="div">MONTHLY REQUEST LIMIT</LabelText>
                <ControlInput value={requestLimit} disabled={!canManageQuota} onChange={(e) => onRequestLimitChange(e.target.value)} placeholder="Unlimited" />
              </div>
              <div>
                <LabelText as="div">MONTHLY TOKEN LIMIT</LabelText>
                <ControlInput value={tokenLimit} disabled={!canManageQuota} onChange={(e) => onTokenLimitChange(e.target.value)} placeholder="Unlimited" />
              </div>
            </div>
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '1.25rem', fontSize: '0.9rem' }}>
              <input type="checkbox" checked={enforceHardLimits} disabled={!canManageQuota} onChange={(e) => onEnforceHardLimitsChange(e.target.checked)} />
              Enforce hard limits before routing inference traffic
            </label>
            <div style={{ marginTop: '1rem', fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
              Last updated {formatDate(quota.updated_at)}
            </div>
            {canManageQuota && (
              <ActionButton variant="primary" style={{ marginTop: '1.25rem' }} disabled={savingQuota} onClick={onSaveQuota}>
                {savingQuota ? 'SAVING...' : 'SAVE QUOTA'}
              </ActionButton>
            )}
          </>
        ) : (
          <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
            You do not have permission to view quota settings for this workspace.
          </div>
        )}
      </Cell>

      <Cell span={2} className="workspace-service-cell">
        <LabelText as="div" style={{ marginBottom: '1.5rem' }}>SERVICE ACCOUNTS</LabelText>
        {canManageKeys ? (
          <>
            <div className="responsive-scroll-x" style={{ marginBottom: '1.5rem' }}>
              <table className="data-table responsive-scroll-x-content">
                <thead>
                  <tr>
                    <th scope="col">NAME</th>
                    <th scope="col">ROLE</th>
                    <th scope="col">PREFIX</th>
                    <th scope="col">LAST USED</th>
                    <th scope="col" style={{ textAlign: 'right' }}>ACTION</th>
                  </tr>
                </thead>
                <tbody>
                  {serviceAccounts.map((key) => (
                    <tr key={key.id}>
                      <td>{key.name}</td>
                      <td><Badge>{key.role.toUpperCase()}</Badge></td>
                      <td className="mono">{key.key_prefix}</td>
                      <td>{formatDate(key.last_used)}</td>
                      <td style={{ textAlign: 'right' }}>
                        <ActionButton variant="destructive" onClick={() => onRevokeServiceAccount(key.id)}>REVOKE</ActionButton>
                      </td>
                    </tr>
                  ))}
                  {serviceAccounts.length === 0 && (
                    <tr>
                      <td colSpan={5} style={{ color: 'var(--text-secondary)', padding: '1.5rem 0' }}>
                        No service accounts yet.
                        <div className="help-actions" style={{ justifyContent: 'center' }}>
                          <ActionButton onClick={() => document.querySelector<HTMLInputElement>('input[placeholder="ci-bot"]')?.focus()}>CREATE ONE</ActionButton>
                          <ActionButton onClick={onOpenApiKeys}>OPEN API KEYS</ActionButton>
                        </div>
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>

            <div className="workspace-service-create-row" style={{ display: 'grid', gridTemplateColumns: '1.4fr 1fr auto', gap: '1rem', alignItems: 'end' }}>
              <div>
                <LabelText as="div">NAME</LabelText>
                <ControlInput value={newServiceAccountName} onChange={(e) => onNewServiceAccountNameChange(e.target.value)} placeholder="ci-bot" />
              </div>
              <div>
                <LabelText as="div">ROLE</LabelText>
                <ControlSelect value={newServiceAccountRole} onChange={(e) => onNewServiceAccountRoleChange(e.target.value)}>
                  {serviceAccountRoles.map((candidate) => (
                    <option key={candidate} value={candidate}>{candidate}</option>
                  ))}
                </ControlSelect>
              </div>
              <ActionButton variant="primary" disabled={creatingServiceAccount} onClick={onCreateServiceAccount}>
                {creatingServiceAccount ? 'CREATING...' : 'CREATE'}
              </ActionButton>
            </div>
          </>
        ) : (
          <div style={{ color: 'var(--text-secondary)', fontSize: '0.9rem' }}>
            Service account management is restricted to workspace owners and admins.
          </div>
        )}
      </Cell>
    </GridRow>
  );
}
