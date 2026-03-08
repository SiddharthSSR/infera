import { useState, useEffect } from 'react';
import { toast } from 'sonner';
import { fetchApiKeys, createApiKey, revokeApiKey, type ApiKeyRecord } from '../lib/api';

export function ApiKeys() {
  const [keys, setKeys] = useState<ApiKeyRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyRole, setNewKeyRole] = useState<'user' | 'admin'>('user');
  const [createdKey, setCreatedKey] = useState<string | null>(null);

  const loadKeys = async () => {
    try {
      const data = await fetchApiKeys();
      setKeys(data);
    } catch {
      // May fail if not admin — show empty
      setKeys([]);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { loadKeys(); }, []);

  const handleGenerate = async () => {
    if (!newKeyName.trim()) {
      toast.error('Please enter a key name');
      return;
    }

    try {
      const result = await createApiKey(newKeyName.trim(), newKeyRole);
      setCreatedKey(result.key);
      setNewKeyName('');
      setNewKeyRole('user');
      toast.success('API key created');
      loadKeys();
    } catch (err: any) {
      toast.error(err.message || 'Failed to create key');
    }
  };

  const handleRevoke = async (id: string) => {
    if (!confirm('Revoke this API key? This cannot be undone.')) return;
    try {
      await revokeApiKey(id);
      toast.success('API key revoked');
      loadKeys();
    } catch (err: any) {
      toast.error(err.message || 'Failed to revoke key');
    }
  };

  const handleCopyKey = () => {
    if (createdKey) {
      navigator.clipboard.writeText(createdKey);
      toast.success('Key copied to clipboard');
    }
  };

  const formatDate = (dateStr: string | null) => {
    if (!dateStr) return 'Never';
    try {
      return new Date(dateStr).toLocaleDateString('en-US', {
        month: 'short', day: 'numeric', year: 'numeric',
      });
    } catch {
      return dateStr;
    }
  };

  return (
    <div className="animate-fade-in">
      {/* Show newly created key banner */}
      {createdKey && (
        <div style={{
          padding: '1.5rem 2rem',
          backgroundColor: '#E8F5E9',
          borderBottom: 'var(--grid-line)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: '1rem',
        }}>
          <div>
            <div style={{ fontWeight: 600, fontSize: '0.8rem', marginBottom: '0.5rem' }}>
              NEW API KEY — COPY NOW (shown only once)
            </div>
            <code className="mono" style={{ fontSize: '0.85rem', wordBreak: 'break-all' }}>
              {createdKey}
            </code>
          </div>
          <div style={{ display: 'flex', gap: '0.5rem', flexShrink: 0 }}>
            <button className="btn-primary" onClick={handleCopyKey}>COPY</button>
            <button className="btn-secondary" onClick={() => setCreatedKey(null)}>DISMISS</button>
          </div>
        </div>
      )}

      <div className="grid-row">
        {/* Keys Table */}
        <div className="cell" style={{ gridColumn: 'span 3' }}>
          <div className="label-text" style={{ marginBottom: '2rem' }}>
            ACTIVE AUTHENTICATION TOKENS
          </div>

          {loading ? (
            <div style={{ padding: '3rem 0', textAlign: 'center', color: 'var(--text-secondary)' }}>
              Loading keys...
            </div>
          ) : (
            <table className="data-table">
              <thead>
                <tr>
                  <th>NAME / PREFIX</th>
                  <th>ROLE</th>
                  <th>CREATED</th>
                  <th>LAST USED</th>
                  <th>STATUS</th>
                  <th style={{ textAlign: 'right' }}>ACTION</th>
                </tr>
              </thead>
              <tbody>
                {keys.map(key => (
                  <tr key={key.id}>
                    <td>
                      <div style={{ fontWeight: 500 }}>{key.name}</div>
                      <div className="mono" style={{ color: 'var(--text-secondary)', marginTop: 4 }}>
                        {key.key_prefix}
                      </div>
                    </td>
                    <td>
                      <span className="badge">{key.role.toUpperCase()}</span>
                    </td>
                    <td>{formatDate(key.created_at)}</td>
                    <td>{formatDate(key.last_used)}</td>
                    <td>
                      <span style={{
                        color: key.status === 'active' ? 'var(--color-success)' : 'var(--color-error)',
                        fontWeight: 600,
                        fontSize: '0.75rem',
                        textTransform: 'uppercase',
                      }}>
                        {key.status}
                      </span>
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      {key.status === 'active' && (
                        <button className="action-btn destructive" onClick={() => handleRevoke(key.id)}>
                          REVOKE
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
                {keys.length === 0 && (
                  <tr>
                    <td colSpan={6} style={{ textAlign: 'center', color: 'var(--text-secondary)', padding: '3rem 0' }}>
                      No API keys. Create one to get started.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          )}
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
              placeholder="e.g. Production"
              value={newKeyName}
              onChange={e => setNewKeyName(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleGenerate()}
            />
          </div>

          <div style={{ marginBottom: '2rem' }}>
            <div className="label-text">ROLE</div>
            <div style={{ marginTop: '1rem', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.9rem' }}>
                <input
                  type="radio"
                  name="role"
                  checked={newKeyRole === 'user'}
                  onChange={() => setNewKeyRole('user')}
                />
                User — inference &amp; read access
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.9rem' }}>
                <input
                  type="radio"
                  name="role"
                  checked={newKeyRole === 'admin'}
                  onChange={() => setNewKeyRole('admin')}
                />
                Admin — full access including key management
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
            <p>Keys are SHA-256 hashed. The full key is only shown once upon creation.</p>
          </div>
        </div>
      </div>

      {/* Footer */}
      <div className="grid-row" style={{ borderBottom: 'none' }}>
        <div className="cell">
          <div className="label-text">ACTIVE KEYS</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {keys.filter(k => k.status === 'active').length} active
          </div>
        </div>
        <div className="cell">
          <div className="label-text">SECURITY</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>SHA-256 hashed, Bearer auth</div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text">USAGE</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.9rem' }}>
            Pass your key via <code className="mono">Authorization: Bearer inf_...</code> header.
          </div>
        </div>
      </div>
    </div>
  );
}
