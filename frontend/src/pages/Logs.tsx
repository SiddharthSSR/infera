import { useState, useEffect, useRef, useCallback, useMemo, type ReactNode } from 'react';
import { useIsMobile } from '../hooks/useIsMobile';
import { LabelText, GridRow, Cell, ActionButton } from '../components/shared';

interface LogEntry {
  id: string;
  timestamp: Date;
  level: 'info' | 'warn' | 'error' | 'debug';
  source: string;
  message: string;
}

const LOG_LEVELS = ['info', 'warn', 'error', 'debug'] as const;
type LogLevel = (typeof LOG_LEVELS)[number];

const LEVEL_COLORS: Record<LogLevel, string> = {
  info: 'var(--color-success)',
  warn: 'var(--color-warning)',
  error: 'var(--color-error)',
  debug: 'var(--text-secondary)',
};

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

/* ------------------------------------------------------------------ */
/*  Search highlight                                                    */
/* ------------------------------------------------------------------ */

function HighlightSearch({ text, query }: { text: string; query: string }): ReactNode {
  if (!query) return text;
  const lower = text.toLowerCase();
  const lowerQ = query.toLowerCase();
  const parts: ReactNode[] = [];
  let cursor = 0;
  let idx = lower.indexOf(lowerQ, cursor);
  while (idx !== -1) {
    if (idx > cursor) parts.push(text.slice(cursor, idx));
    parts.push(
      <mark
        key={idx}
        style={{ background: 'rgba(249, 168, 37, 0.2)', color: 'inherit', padding: '1px 2px', borderRadius: 2 }}
      >
        {text.slice(idx, idx + query.length)}
      </mark>,
    );
    cursor = idx + query.length;
    idx = lower.indexOf(lowerQ, cursor);
  }
  if (cursor < text.length) parts.push(text.slice(cursor));
  return <>{parts}</>;
}

/* ------------------------------------------------------------------ */
/*  Level filter pill                                                   */
/* ------------------------------------------------------------------ */

function LevelPill({
  level,
  active,
  onToggle,
}: {
  level: LogLevel;
  active: boolean;
  onToggle: () => void;
}) {
  const color = LEVEL_COLORS[level];
  return (
    <button
      type="button"
      onClick={onToggle}
      className="log-level-pill"
      style={{
        color: active ? color : 'var(--text-secondary)',
        borderColor: active ? color : 'var(--border-color)',
        background: active ? undefined : 'transparent',
      }}
    >
      {level.toUpperCase()}
    </button>
  );
}

/* ------------------------------------------------------------------ */
/*  Virtual scroll hook                                                 */
/* ------------------------------------------------------------------ */

const ROW_HEIGHT = 36;
const BUFFER_ROWS = 10;

function useVirtualScroll(containerRef: React.RefObject<HTMLDivElement | null>, totalCount: number) {
  const [scrollTop, setScrollTop] = useState(0);
  const [containerHeight, setContainerHeight] = useState(600);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const measure = () => setContainerHeight(el.clientHeight);
    measure();

    const ro = new ResizeObserver(measure);
    ro.observe(el);

    const handleScroll = () => setScrollTop(el.scrollTop);
    el.addEventListener('scroll', handleScroll, { passive: true });

    return () => {
      ro.disconnect();
      el.removeEventListener('scroll', handleScroll);
    };
  }, [containerRef]);

  const startIndex = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - BUFFER_ROWS);
  const visibleCount = Math.ceil(containerHeight / ROW_HEIGHT) + BUFFER_ROWS * 2;
  const endIndex = Math.min(totalCount, startIndex + visibleCount);
  const totalHeight = totalCount * ROW_HEIGHT;
  const offsetY = startIndex * ROW_HEIGHT;

  return { startIndex, endIndex, totalHeight, offsetY };
}

/* ------------------------------------------------------------------ */
/*  Logs page                                                           */
/* ------------------------------------------------------------------ */

const REFRESH_OPTIONS = [
  { label: '1s', ms: 1000 },
  { label: '5s', ms: 5000 },
  { label: '10s', ms: 10000 },
  { label: '30s', ms: 30000 },
] as const;

export function Logs() {
  const isMobile = useIsMobile(900);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [isStreaming, setIsStreaming] = useState(true);
  const [refreshInterval, setRefreshInterval] = useState(5000);
  const [activeLevels, setActiveLevels] = useState<Set<LogLevel>>(new Set(LOG_LEVELS));
  const [sourceFilter, setSourceFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const logsContainerRef = useRef<HTMLDivElement>(null);
  const autoScrollRef = useRef(true);
  const [newLogIds, setNewLogIds] = useState<Set<string>>(new Set());

  useEffect(() => {
    const initial = Array.from({ length: 50 }, generateMockLog);
    setLogs(initial);
  }, []);

  useEffect(() => {
    if (!isStreaming) return;
    const interval = setInterval(() => {
      const entry = generateMockLog();
      setNewLogIds(prev => {
        const next = new Set(prev);
        next.add(entry.id);
        return next;
      });
      setLogs(prev => [...prev, entry].slice(-10000));
      // Clear the animation flag after it completes
      setTimeout(() => {
        setNewLogIds(prev => {
          const next = new Set(prev);
          next.delete(entry.id);
          return next;
        });
      }, 350);
    }, refreshInterval);
    return () => clearInterval(interval);
  }, [isStreaming, refreshInterval]);

  // Auto-scroll when streaming and user is at the bottom
  useEffect(() => {
    const el = logsContainerRef.current;
    if (!el || !isStreaming || !autoScrollRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [logs, isStreaming]);

  // Track whether user is at bottom
  useEffect(() => {
    const el = logsContainerRef.current;
    if (!el) return;
    const handleScroll = () => {
      autoScrollRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    };
    el.addEventListener('scroll', handleScroll, { passive: true });
    return () => el.removeEventListener('scroll', handleScroll);
  }, []);

  const toggleLevel = useCallback((level: LogLevel) => {
    setActiveLevels(prev => {
      const next = new Set(prev);
      if (next.has(level)) {
        if (next.size > 1) next.delete(level);
      } else {
        next.add(level);
      }
      return next;
    });
  }, []);

  const filteredLogs = useMemo(() => logs.filter(log => {
    if (!activeLevels.has(log.level)) return false;
    if (sourceFilter !== 'all' && log.source !== sourceFilter) return false;
    if (searchQuery && !log.message.toLowerCase().includes(searchQuery.toLowerCase())) return false;
    return true;
  }), [logs, activeLevels, sourceFilter, searchQuery]);

  const sources = useMemo(() => [...new Set(logs.map(l => l.source))], [logs]);

  const { startIndex, endIndex, totalHeight, offsetY } = useVirtualScroll(logsContainerRef, filteredLogs.length);
  const visibleRows = filteredLogs.slice(startIndex, endIndex);

  const handleExport = () => {
    const header = 'Timestamp\tLevel\tSource\tMessage';
    const rows = filteredLogs
      .map(log => `${log.timestamp.toISOString()}\t${log.level.toUpperCase()}\t${log.source}\t${log.message}`);
    const content = [header, ...rows].join('\n');
    const blob = new Blob([content], { type: 'text/tab-separated-values' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `infera-logs-${new Date().toISOString().slice(0, 10)}.tsv`;
    a.click();
  };

  const levelClass = (level: string) => {
    const map: Record<string, string> = { info: 'level-info', warn: 'level-warn', error: 'level-error', debug: 'level-debug' };
    return map[level] || '';
  };

  return (
    <div className="animate-fade-in" style={{ display: 'flex', flexDirection: 'column', height: isMobile ? 'calc(100vh - 124px)' : 'calc(100vh - 160px)', overflow: 'hidden' }}>
      {/* Filter Bar */}
      <div style={{
        backgroundColor: 'var(--bg-accent)',
        padding: isMobile ? '1rem' : '0.9rem 1.25rem',
        display: 'flex',
        gap: '1rem',
        flexWrap: 'wrap',
        alignItems: 'flex-end',
        borderBottom: 'var(--grid-line)',
      }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', flex: isMobile ? '1 1 100%' : '0 0 auto' }}>
          <LabelText as="div">SEARCH</LabelText>
          <input
            type="text"
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            placeholder="Filter by message or ID..."
            style={{
              background: 'transparent', border: 'none',
              borderBottom: '1px solid var(--text-primary)',
              fontFamily: 'var(--font-main)', fontSize: '0.85rem',
              padding: '2px 0', width: isMobile ? '100%' : 220, outline: 'none', color: 'var(--text-primary)',
            }}
          />
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', flex: isMobile ? '1 1 100%' : '0 0 auto' }}>
          <LabelText as="div">LOG LEVEL</LabelText>
          <div style={{ display: 'flex', gap: '0.35rem', flexWrap: 'wrap' }}>
            {LOG_LEVELS.map(level => (
              <LevelPill
                key={level}
                level={level}
                active={activeLevels.has(level)}
                onToggle={() => toggleLevel(level)}
              />
            ))}
          </div>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', flex: isMobile ? '1 1 calc(50% - 0.5rem)' : '0 0 auto' }}>
          <LabelText as="div">SOURCE</LabelText>
          <select className="filter-select" value={sourceFilter} onChange={e => setSourceFilter(e.target.value)}>
            <option value="all">ALL SOURCES</option>
            {sources.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
        </div>
        <div style={{ marginLeft: isMobile ? 0 : 'auto', display: 'flex', gap: '1.5rem', alignItems: 'flex-end', flexWrap: 'wrap', flex: isMobile ? '1 1 100%' : '0 0 auto' }}>
          <div style={{ display: 'grid', gap: '0.35rem' }}>
            <LabelText as="div">VISIBLE</LabelText>
            <div className="mono" style={{ fontSize: '0.8rem' }}>{filteredLogs.length} log lines</div>
          </div>
          <div style={{ display: 'grid', gap: '0.35rem' }}>
            <LabelText as="div">STREAM</LabelText>
            <button
              type="button"
              className="log-stream-toggle"
              onClick={() => setIsStreaming(v => !v)}
            >
              <span
                className="log-stream-dot"
                style={{
                  background: isStreaming ? 'var(--color-success)' : 'var(--border-color)',
                  animation: isStreaming ? 'status-pulse 1.5s ease-in-out infinite' : 'none',
                }}
              />
              <span style={{ color: isStreaming ? 'var(--color-success)' : 'var(--text-secondary)', fontWeight: 600 }}>
                {isStreaming ? 'LIVE' : 'PAUSED'}
              </span>
            </button>
          </div>
          <div style={{ display: 'grid', gap: '0.35rem' }}>
            <LabelText as="div">INTERVAL</LabelText>
            <div style={{ display: 'flex', gap: '0.5rem' }}>
              {REFRESH_OPTIONS.map(opt => (
                <button
                  key={opt.ms}
                  type="button"
                  className="log-interval-btn"
                  style={{
                    color: refreshInterval === opt.ms ? 'var(--text-primary)' : 'var(--text-secondary)',
                    borderBottomColor: refreshInterval === opt.ms ? 'var(--text-primary)' : 'transparent',
                  }}
                  onClick={() => setRefreshInterval(opt.ms)}
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* Log Table */}
      {isMobile ? (
        <div ref={logsContainerRef} aria-live="polite" style={{ flexGrow: 1, overflowY: 'auto', minHeight: 0, padding: '1rem' }}>
          <div style={{ height: totalHeight, position: 'relative' }}>
            <div style={{ position: 'absolute', top: offsetY, left: 0, right: 0 }}>
              {visibleRows.map(log => (
                <div
                  key={log.id}
                  className="mobile-data-card"
                  style={{
                    marginBottom: '0.5rem',
                    animation: newLogIds.has(log.id) ? 'dash-log-slide-in 0.3s ease-out both' : undefined,
                  }}
                >
                  <div className="mobile-data-card-header">
                    <span className={`log-level ${levelClass(log.level)}`}>{log.level.toUpperCase()}</span>
                    <span className="mono" style={{ color: 'var(--text-secondary)', fontSize: '0.7rem' }}>
                      {log.timestamp.toISOString().slice(11, 19)}
                    </span>
                  </div>
                  <div className="mobile-data-subtitle mono" style={{ color: 'var(--text-primary)' }}>
                    {log.source}
                  </div>
                  <div className="mobile-log-message">
                    <HighlightSearch text={log.message} query={searchQuery} />
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      ) : (
        <div ref={logsContainerRef} className="responsive-scroll-x" style={{ flexGrow: 1, overflowY: 'auto', minHeight: 0 }}>
          <table className="responsive-scroll-x-content" style={{ width: '100%', borderCollapse: 'collapse', fontFamily: 'var(--font-mono)', fontSize: '0.8rem' }}>
            <thead>
              <tr>
                <th scope="col" className="label-text" style={{ textAlign: 'left', padding: '1rem 2rem', borderBottom: 'var(--grid-line)', position: 'sticky', top: 0, background: 'var(--bg-paper)', zIndex: 1 }}>
                  Timestamp
                </th>
                <th scope="col" className="label-text" style={{ textAlign: 'left', padding: '1rem 0.5rem', borderBottom: 'var(--grid-line)', position: 'sticky', top: 0, background: 'var(--bg-paper)', zIndex: 1 }}>
                  Level
                </th>
                <th scope="col" className="label-text" style={{ textAlign: 'left', padding: '1rem 0.5rem', borderBottom: 'var(--grid-line)', position: 'sticky', top: 0, background: 'var(--bg-paper)', zIndex: 1 }}>
                  Source
                </th>
                <th scope="col" className="label-text" style={{ textAlign: 'left', padding: '1rem 2rem 1rem 0.5rem', borderBottom: 'var(--grid-line)', position: 'sticky', top: 0, background: 'var(--bg-paper)', zIndex: 1 }}>
                  Message
                </th>
              </tr>
            </thead>
            <tbody aria-live="polite">
              {/* Spacer for rows above visible range */}
              {startIndex > 0 && (
                <tr aria-hidden="true">
                  <td colSpan={4} style={{ height: offsetY, padding: 0, border: 'none' }} />
                </tr>
              )}
              {visibleRows.map(log => (
                <tr
                  key={log.id}
                  style={{
                    height: ROW_HEIGHT,
                    animation: newLogIds.has(log.id) ? 'dash-log-slide-in 0.3s ease-out both' : undefined,
                  }}
                >
                  <td style={{ padding: '0.6rem 1.25rem', borderBottom: '1px solid #EEEEEC', color: 'var(--text-secondary)', width: 160, verticalAlign: 'top' }}>
                    {log.timestamp.toISOString().slice(0, 19).replace('T', ' ')}
                  </td>
                  <td style={{ padding: '0.6rem 0.5rem', borderBottom: '1px solid #EEEEEC', verticalAlign: 'top' }}>
                    <span className={`log-level ${levelClass(log.level)}`}>{log.level.toUpperCase()}</span>
                  </td>
                  <td style={{ padding: '0.6rem 0.5rem', borderBottom: '1px solid #EEEEEC', fontWeight: 500, color: 'var(--text-primary)', width: 140, verticalAlign: 'top' }}>
                    {log.source}
                  </td>
                  <td style={{ padding: '0.6rem 1.25rem 0.6rem 0.5rem', borderBottom: '1px solid #EEEEEC', color: 'var(--text-secondary)', verticalAlign: 'top' }}>
                    <HighlightSearch text={log.message} query={searchQuery} />
                  </td>
                </tr>
              ))}
              {/* Spacer for rows below visible range */}
              {endIndex < filteredLogs.length && (
                <tr aria-hidden="true">
                  <td colSpan={4} style={{ height: (filteredLogs.length - endIndex) * ROW_HEIGHT, padding: 0, border: 'none' }} />
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {/* Footer */}
      <GridRow style={{ borderTop: 'var(--grid-line)' }}>
        <Cell>
          <LabelText as="div">LIVE STATUS</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem', display: 'flex', alignItems: 'center', gap: 8 }}>
            <span
              className="log-stream-dot"
              style={{
                background: isStreaming ? 'var(--color-success)' : 'var(--border-color)',
                animation: isStreaming ? 'status-pulse 1.5s ease-in-out infinite' : 'none',
              }}
            />
            <span style={{ fontWeight: 600, color: isStreaming ? 'var(--color-success)' : 'var(--text-secondary)' }}>
              {isStreaming ? 'LIVE' : 'PAUSED'}
            </span>
            {isStreaming && (
              <span style={{ color: 'var(--text-secondary)', fontWeight: 400 }}>
                every {REFRESH_OPTIONS.find(o => o.ms === refreshInterval)?.label}
              </span>
            )}
          </div>
        </Cell>
        <Cell>
          <LabelText as="div">ENTRIES</LabelText>
          <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
            {filteredLogs.length} shown
          </div>
        </Cell>
        <Cell span={2}>
          <LabelText as="div">LOGGING CONTROLS</LabelText>
          <div style={{ marginTop: '0.5rem', display: 'flex', gap: '2rem', flexWrap: 'wrap' }}>
            <ActionButton onClick={() => setIsStreaming(!isStreaming)}>
              {isStreaming ? 'PAUSE STREAM' : 'RESUME STREAM'}
            </ActionButton>
            <ActionButton onClick={() => setLogs([])}>CLEAR SCREEN</ActionButton>
            <ActionButton onClick={handleExport}>EXPORT .TSV</ActionButton>
          </div>
        </Cell>
      </GridRow>
    </div>
  );
}
