import { useState } from 'react';
import { toast } from 'sonner';

interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  created: string;
  lastUsed: string;
  scopes: string[];
}

export function ApiKeys() {
  const [keys, setKeys] = useState<ApiKey[]>([
    {
      id: '1',
      name: 'Production-Main',
      prefix: 'sk_live_4f92...',
      created: 'June 14, 2024',
      lastUsed: '2 mins ago',
      scopes: ['Inference', 'Logs'],
    },
    {
      id: '2',
      name: 'Staging-Worker',
      prefix: 'sk_test_a2b1...',
      created: 'June 10, 2024',
      lastUsed: 'Yesterday',
      scopes: ['Inference'],
    },
  ]);

  const [newKeyName, setNewKeyName] = useState('');
  const [scopes, setScopes] = useState({ inference: true, logs: false, cluster: false });

  const handleGenerate = () => {
    if (!newKeyName.trim()) {
      toast.error('Please enter a key name');
      return;
    }

    const selectedScopes = [];
    if (scopes.inference) selectedScopes.push('Inference');
    if (scopes.logs) selectedScopes.push('Logs & Metrics');
    if (scopes.cluster) selectedScopes.push('Full Access');

    const newKey: ApiKey = {
      id: Math.random().toString(36).slice(2),
      name: newKeyName,
      prefix: `sk_live_${Math.random().toString(36).slice(2, 6)}...`,
      created: new Date().toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' }),
      lastUsed: 'Never',
      scopes: selectedScopes,
    };

    setKeys(prev => [...prev, newKey]);
    setNewKeyName('');
    setScopes({ inference: true, logs: false, cluster: false });
    toast.success('API key generated successfully');
  };

  const handleRevoke = (id: string) => {
    if (!confirm('Revoke this API key? This cannot be undone.')) return;
    setKeys(prev => prev.filter(k => k.id !== id));
    toast.success('API key revoked');
  };

  return (
    <div className="animate-fade-in">
      <div className="grid-row">
        {/* Keys Table */}
        <div className="cell" style={{ gridColumn: 'span 3' }}>
          <div className="label-text" style={{ marginBottom: '2rem' }}>
            ACTIVE AUTHENTICATION TOKENS
          </div>

          <table className="data-table">
            <thead>
              <tr>
                <th>NAME / PREFIX</th>
                <th>CREATED</th>
                <th>LAST USED</th>
                <th>SCOPE</th>
                <th style={{ textAlign: 'right' }}>ACTION</th>
              </tr>
            </thead>
            <tbody>
              {keys.map(key => (
                <tr key={key.id}>
                  <td>
                    <div style={{ fontWeight: 500 }}>{key.name}</div>
                    <div className="mono" style={{ color: 'var(--text-secondary)', marginTop: 4 }}>
                      {key.prefix}
                    </div>
                  </td>
                  <td>{key.created}</td>
                  <td>{key.lastUsed}</td>
                  <td>
                    {key.scopes.map(scope => (
                      <span key={scope} className="scope-tag">{scope}</span>
                    ))}
                  </td>
                  <td style={{ textAlign: 'right' }}>
                    <button className="action-btn destructive" onClick={() => handleRevoke(key.id)}>
                      REVOKE
                    </button>
                  </td>
                </tr>
              ))}
              {keys.length === 0 && (
                <tr>
                  <td colSpan={5} style={{ textAlign: 'center', color: 'var(--text-secondary)', padding: '3rem 0' }}>
                    No API keys. Create one to get started.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {/* Create New Key */}
        <div className="cell" style={{ backgroundColor: 'var(--bg-accent)' }}>
          <div className="label-text" style={{ marginBottom: '1.5rem' }}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M12 2v20M2 12h20" />
            </svg>
            CREATE NEW KEY
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div className="label-text">KEY NAME</div>
            <input
              type="text"
              className="control-input"
              placeholder="e.g. Development"
              value={newKeyName}
              onChange={e => setNewKeyName(e.target.value)}
            />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div className="label-text">PERMISSIONS SCOPE</div>
            <div style={{ marginTop: '1rem', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.9rem' }}>
                <input type="checkbox" checked={scopes.inference} onChange={e => setScopes(s => ({ ...s, inference: e.target.checked }))} />
                Inference
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.9rem' }}>
                <input type="checkbox" checked={scopes.logs} onChange={e => setScopes(s => ({ ...s, logs: e.target.checked }))} />
                Logs &amp; Metrics
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.9rem' }}>
                <input type="checkbox" checked={scopes.cluster} onChange={e => setScopes(s => ({ ...s, cluster: e.target.checked }))} />
                Cluster Management
              </label>
            </div>
          </div>

          <button
            className="action-btn"
            style={{ width: '100%', textAlign: 'left', padding: '1rem 0' }}
            onClick={handleGenerate}
          >
            GENERATE NEW KEY
          </button>

          <div style={{ marginTop: '4rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
            <p>Security Note: Keys are only displayed once upon creation. Store them securely in your vault.</p>
          </div>
        </div>
      </div>

      {/* Footer */}
      <div className="grid-row" style={{ borderBottom: 'none' }}>
        <div className="cell">
          <div className="label-text">QUOTA</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {keys.length} of 10 keys used
          </div>
        </div>
        <div className="cell">
          <div className="label-text">ENCRYPTION</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>AES-256-GCM</div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text">DOCUMENTATION</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.9rem' }}>
            Learn how to authenticate your requests using the Bearer scheme.
            <div style={{ marginTop: '1rem' }}>
              <a href="/api/health" target="_blank" rel="noopener noreferrer" className="action-btn" style={{ textDecoration: 'none' }}>
                VIEW AUTH GUIDE
              </a>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
