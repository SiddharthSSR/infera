import { useState, useEffect, useRef } from 'react';

interface LogEntry {
  id: string;
  timestamp: Date;
  level: 'info' | 'warn' | 'error' | 'debug';
  source: string;
  message: string;
}

function generateMockLog(): LogEntry {
  const levels: LogEntry['level'][] = ['info', 'info', 'info', 'debug', 'warn', 'error'];
  const sources = ['GATEWAY-01', 'WORKER-02', 'SCHEDULER', 'AUTOSCALER', 'INFERENCE-01', 'NODE-MANAGER'];
  const messages = [
    'Request accepted: model inference [req_9a2b8c]',
    'KV Cache hit rate: 0.92 for block 8410',
    'Streaming response completed in 412ms',
    'Health check passed. Latency stable.',
    'Prefill phase latency: 12ms | Decoding: 40 tokens/sec',
    'New configuration applied: Max Batch Size = 64',
    'Worker heartbeat received',
    'GPU utilization: 65%',
    'Rate limit warning: 90% capacity',
    'Node approaching thermal threshold (82C)',
    'CUDA_OUT_OF_MEMORY: Failed to allocate attention_mask',
    'Re-routing pending tasks to cluster-us-east-b',
  ];

  return {
    id: Math.random().toString(36).slice(2),
    timestamp: new Date(),
    level: levels[Math.floor(Math.random() * levels.length)],
    source: sources[Math.floor(Math.random() * sources.length)],
    message: messages[Math.floor(Math.random() * messages.length)],
  };
}

export function Logs() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [isStreaming, setIsStreaming] = useState(true);
  const [levelFilter, setLevelFilter] = useState<string>('all');
  const [sourceFilter, setSourceFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');

  useEffect(() => {
    const initial = Array.from({ length: 15 }, generateMockLog);
    setLogs(initial);
  }, []);

  useEffect(() => {
    if (!isStreaming) return;
    const interval = setInterval(() => {
      setLogs(prev => [...prev, generateMockLog()].slice(-500));
    }, 2000);
    return () => clearInterval(interval);
  }, [isStreaming]);

  const logsContainerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (isStreaming && logsContainerRef.current) {
      logsContainerRef.current.scrollTop = logsContainerRef.current.scrollHeight;
    }
  }, [logs, isStreaming]);

  const filteredLogs = logs.filter(log => {
    if (levelFilter !== 'all' && log.level !== levelFilter) return false;
    if (sourceFilter !== 'all' && log.source !== sourceFilter) return false;
    if (searchQuery && !log.message.toLowerCase().includes(searchQuery.toLowerCase())) return false;
    return true;
  });

  const sources = [...new Set(logs.map(l => l.source))];

  const handleExport = () => {
    const content = filteredLogs
      .map(log => `${log.timestamp.toISOString()}\t${log.level.toUpperCase()}\t${log.source}\t${log.message}`)
      .join('\n');
    const blob = new Blob([content], { type: 'text/csv' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `infera-logs-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
  };

  const levelClass = (level: string) => {
    const map: Record<string, string> = { info: 'level-info', warn: 'level-warn', error: 'level-error', debug: 'level-debug' };
    return map[level] || '';
  };

  return (
    <div className="animate-fade-in" style={{ display: 'flex', flexDirection: 'column', height: 'calc(100vh - 180px)', overflow: 'hidden' }}>
      {/* Filter Bar */}
      <div style={{
        backgroundColor: 'var(--bg-accent)',
        padding: '1rem 2rem',
        display: 'flex',
        gap: '2rem',
        alignItems: 'flex-end',
        borderBottom: 'var(--grid-line)',
      }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          <div className="label-text">SEARCH</div>
          <input
            type="text"
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            placeholder="Filter by message or ID..."
            style={{
              background: 'transparent', border: 'none',
              borderBottom: '1px solid var(--text-primary)',
              fontFamily: 'var(--font-main)', fontSize: '0.85rem',
              padding: '2px 0', width: 240, outline: 'none', color: 'var(--text-primary)',
            }}
          />
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          <div className="label-text">LOG LEVEL</div>
          <select className="filter-select" value={levelFilter} onChange={e => setLevelFilter(e.target.value)}>
            <option value="all">ALL LEVELS</option>
            <option value="info">INFO</option>
            <option value="warn">WARN</option>
            <option value="error">ERROR</option>
            <option value="debug">DEBUG</option>
          </select>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          <div className="label-text">SOURCE</div>
          <select className="filter-select" value={sourceFilter} onChange={e => setSourceFilter(e.target.value)}>
            <option value="all">ALL SOURCES</option>
            {sources.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </div>
        <div style={{ marginLeft: 'auto' }}>
          <button className="action-btn" onClick={handleExport}>EXPORT .CSV</button>
        </div>
      </div>

      {/* Log Table */}
      <div ref={logsContainerRef} style={{ flexGrow: 1, overflowY: 'auto', minHeight: 0 }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontFamily: 'var(--font-mono)', fontSize: '0.8rem' }}>
          <thead>
            <tr>
              <th className="label-text" style={{ textAlign: 'left', padding: '1rem 2rem', borderBottom: 'var(--grid-line)', position: 'sticky', top: 0, background: 'var(--bg-paper)' }}>
                Timestamp
              </th>
              <th className="label-text" style={{ textAlign: 'left', padding: '1rem 0.5rem', borderBottom: 'var(--grid-line)', position: 'sticky', top: 0, background: 'var(--bg-paper)' }}>
                Level
              </th>
              <th className="label-text" style={{ textAlign: 'left', padding: '1rem 0.5rem', borderBottom: 'var(--grid-line)', position: 'sticky', top: 0, background: 'var(--bg-paper)' }}>
                Source
              </th>
              <th className="label-text" style={{ textAlign: 'left', padding: '1rem 2rem 1rem 0.5rem', borderBottom: 'var(--grid-line)', position: 'sticky', top: 0, background: 'var(--bg-paper)' }}>
                Message
              </th>
            </tr>
          </thead>
          <tbody>
            {filteredLogs.map(log => (
              <tr key={log.id}>
                <td style={{ padding: '0.75rem 2rem', borderBottom: '1px solid #EEEEEC', color: 'var(--text-secondary)', width: 160, verticalAlign: 'top' }}>
                  {log.timestamp.toISOString().slice(0, 19).replace('T', ' ')}
                </td>
                <td style={{ padding: '0.75rem 0.5rem', borderBottom: '1px solid #EEEEEC', verticalAlign: 'top' }}>
                  <span className={`log-level ${levelClass(log.level)}`}>{log.level.toUpperCase()}</span>
                </td>
                <td style={{ padding: '0.75rem 0.5rem', borderBottom: '1px solid #EEEEEC', fontWeight: 500, color: 'var(--text-primary)', width: 140, verticalAlign: 'top' }}>
                  {log.source}
                </td>
                <td style={{ padding: '0.75rem 2rem 0.75rem 0.5rem', borderBottom: '1px solid #EEEEEC', color: 'var(--text-secondary)', verticalAlign: 'top' }}>
                  {log.message}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Footer */}
      <div className="grid-row" style={{ borderTop: 'var(--grid-line)', borderBottom: 'none' }}>
        <div className="cell">
          <div className="label-text">LIVE STATUS</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem', display: 'flex', alignItems: 'center', gap: 8 }}>
            <span className={`status-dot ${isStreaming ? '' : 'inactive'}`} />
            {isStreaming ? 'Connected to Stream' : 'Paused'}
          </div>
        </div>
        <div className="cell">
          <div className="label-text">ENTRIES</div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {filteredLogs.length} shown
          </div>
        </div>
        <div className="cell" style={{ gridColumn: 'span 2' }}>
          <div className="label-text">LOGGING CONTROLS</div>
          <div style={{ marginTop: '0.5rem', display: 'flex', gap: '2rem' }}>
            <button className="action-btn" onClick={() => setIsStreaming(!isStreaming)}>
              {isStreaming ? 'PAUSE STREAM' : 'RESUME STREAM'}
            </button>
            <button className="action-btn" onClick={() => setLogs([])}>CLEAR SCREEN</button>
            <button className="action-btn" onClick={handleExport}>ARCHIVE LOGS</button>
          </div>
        </div>
      </div>
    </div>
  );
}
