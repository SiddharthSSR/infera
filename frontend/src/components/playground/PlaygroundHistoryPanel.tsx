import type { PlaygroundHistoryEntry } from '../../lib/chat-context';

interface PlaygroundHistoryPanelProps {
  history: PlaygroundHistoryEntry[];
  onClearHistory: () => void;
}

export function PlaygroundHistoryPanel({ history, onClearHistory }: PlaygroundHistoryPanelProps) {
  return (
    <>
      {history.length === 0 ? (
        <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)', padding: '1rem 0' }}>
          No requests yet. Run an inference or agent task to see history.
        </div>
      ) : (
        history.map((entry, index) => (
          <button
            type="button"
            key={entry.id}
            style={{
              padding: '1rem 0',
              cursor: 'pointer',
              opacity: index === 0 ? 1 : 0.7,
              background: 'none',
              border: 'none',
              borderBottom: '1px solid #E5E2DE',
              width: '100%',
              textAlign: 'left',
            }}
          >
            <span className="mono" style={{ fontSize: '0.65rem', color: 'var(--text-secondary)', display: 'block', marginBottom: '0.25rem' }}>
              {entry.time} - {entry.latencyMs}ms
            </span>
            <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', marginBottom: '0.35rem', flexWrap: 'wrap' }}>
              <span className="mono" style={{ fontSize: '0.62rem', color: 'var(--text-secondary)' }}>
                {entry.mode === 'agent' ? (entry.agentID || 'agent').toUpperCase() : 'CHAT'}
              </span>
              {entry.statusLabel && (
                <span className="mono" style={{ fontSize: '0.62rem', color: 'var(--text-secondary)' }}>
                  {entry.statusLabel}
                </span>
              )}
            </div>
            <div
              style={{
                fontSize: '0.85rem',
                whiteSpace: 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                color: 'var(--text-primary)',
              }}
            >
              {entry.preview}
            </div>
            {entry.promptTokens != null && (
              <span className="mono" style={{ fontSize: '0.6rem', color: 'var(--text-secondary)', marginTop: '0.25rem', display: 'block' }}>
                {entry.promptTokens} + {entry.completionTokens} tokens
              </span>
            )}
          </button>
        ))
      )}

      {history.length > 0 && (
        <div style={{ marginTop: '1rem' }}>
          <button
            className="btn-secondary"
            style={{ width: '100%', borderStyle: 'dashed', opacity: 0.5 }}
            onClick={onClearHistory}
          >
            CLEAR HISTORY
          </button>
        </div>
      )}
    </>
  );
}
