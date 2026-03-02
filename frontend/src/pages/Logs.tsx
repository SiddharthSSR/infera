import { useState, useEffect, useRef } from 'react';
import { Terminal, Play, Pause, Trash2, Download, Filter, AlertCircle, Info, AlertTriangle, CheckCircle, Search } from 'lucide-react';

interface LogEntry {
  id: string;
  timestamp: Date;
  level: 'info' | 'warn' | 'error' | 'debug';
  source: string;
  message: string;
}

function generateMockLog(): LogEntry {
  const levels: LogEntry['level'][] = ['info', 'info', 'info', 'debug', 'warn', 'error'];
  const sources = ['gateway', 'worker', 'router', 'provider'];
  const messages = [
    'Request processed successfully',
    'Worker heartbeat received',
    'Model inference completed in 245ms',
    'Health check passed',
    'GPU utilization: 65%',
    'Batch processed: 8 requests',
    'Rate limit warning: 90% capacity',
    'Model loading started',
    'Streaming response initiated',
  ];

  return {
    id: Math.random().toString(36).slice(2),
    timestamp: new Date(),
    level: levels[Math.floor(Math.random() * levels.length)],
    source: sources[Math.floor(Math.random() * sources.length)],
    message: messages[Math.floor(Math.random() * messages.length)],
  };
}

function LogLine({ log }: { log: LogEntry }) {
  const levelConfig = {
    info: { icon: Info, color: 'text-infera-400', bg: 'bg-infera-500/10' },
    warn: { icon: AlertTriangle, color: 'text-accent-yellow', bg: 'bg-accent-yellow/10' },
    error: { icon: AlertCircle, color: 'text-accent-red', bg: 'bg-accent-red/10' },
    debug: { icon: CheckCircle, color: 'text-surface-400', bg: 'bg-surface-800' },
  };

  const { icon: Icon, color, bg } = levelConfig[log.level];

  return (
    <div className="flex items-start gap-3 py-2 px-3 hover:bg-surface-900/50 font-mono text-sm">
      <span className="text-surface-500 text-xs w-20 flex-shrink-0">{log.timestamp.toLocaleTimeString()}</span>
      <div className={`w-5 h-5 rounded flex items-center justify-center flex-shrink-0 ${bg}`}>
        <Icon className={`w-3 h-3 ${color}`} />
      </div>
      <span className="text-surface-400 w-16 flex-shrink-0 uppercase text-xs">{log.source}</span>
      <span className="text-surface-200 flex-1">{log.message}</span>
    </div>
  );
}

export function Logs() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [isStreaming, setIsStreaming] = useState(true);
  const [filter, setFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const logsEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!isStreaming) return;
    setLogs(Array.from({ length: 20 }, generateMockLog));
    const interval = setInterval(() => {
      setLogs(prev => [...prev, generateMockLog()].slice(-500));
    }, 2000);
    return () => clearInterval(interval);
  }, [isStreaming]);

  useEffect(() => {
    if (isStreaming) logsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs, isStreaming]);

  const filteredLogs = logs.filter(log => {
    if (filter !== 'all' && log.level !== filter) return false;
    if (searchQuery && !log.message.toLowerCase().includes(searchQuery.toLowerCase())) return false;
    return true;
  });

  const handleExport = () => {
    const content = filteredLogs.map(log => `[${log.timestamp.toISOString()}] [${log.level.toUpperCase()}] [${log.source}] ${log.message}`).join('\n');
    const blob = new Blob([content], { type: 'text/plain' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `infera-logs-${new Date().toISOString().slice(0, 10)}.txt`;
    a.click();
  };

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <button onClick={() => setIsStreaming(!isStreaming)} className={`inline-flex items-center gap-2 px-4 py-2.5 rounded-xl font-medium transition-all ${isStreaming ? 'bg-accent-green/10 text-accent-green border border-accent-green/20 hover:bg-accent-green hover:text-white' : 'bg-surface-800 text-surface-100 border border-surface-700 hover:bg-surface-700'}`}>
            {isStreaming ? <><Pause className="w-4 h-4" />Streaming</> : <><Play className="w-4 h-4" />Paused</>}
          </button>

          <div className="relative">
            <select value={filter} onChange={(e) => setFilter(e.target.value)} className="appearance-none bg-surface-900 border border-surface-700 rounded-xl px-4 py-2 pl-9 pr-10 text-sm text-surface-100 focus:outline-none cursor-pointer">
              <option value="all">All Levels</option>
              <option value="info">Info</option>
              <option value="warn">Warning</option>
              <option value="error">Error</option>
              <option value="debug">Debug</option>
            </select>
            <Filter className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-surface-400" />
          </div>

          <div className="relative">
            <input type="text" value={searchQuery} onChange={(e) => setSearchQuery(e.target.value)} placeholder="Search logs..." className="bg-surface-900 border border-surface-700 rounded-xl py-2 pl-9 pr-4 text-sm text-surface-100 placeholder-surface-500 focus:outline-none focus:ring-2 focus:ring-infera-500/50 w-48" />
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-surface-400" />
          </div>
        </div>

        <div className="flex items-center gap-2">
          <button onClick={() => setLogs([])} className="inline-flex items-center gap-2 px-3 py-2 text-sm text-surface-400 hover:text-surface-100 hover:bg-surface-800 rounded-xl transition-colors">
            <Trash2 className="w-4 h-4" />Clear
          </button>
          <button onClick={handleExport} className="inline-flex items-center gap-2 px-3 py-2 text-sm text-surface-400 hover:text-surface-100 hover:bg-surface-800 rounded-xl transition-colors">
            <Download className="w-4 h-4" />Export
          </button>
        </div>
      </div>

      <div className="bg-surface-900/80 backdrop-blur-sm border border-surface-800 rounded-2xl overflow-hidden">
        <div className="flex items-center gap-2 px-4 py-3 border-b border-surface-800 bg-surface-900/50">
          <Terminal className="w-4 h-4 text-infera-400" />
          <span className="text-sm font-medium text-white">System Logs</span>
          <span className="text-xs text-surface-500">({filteredLogs.length} entries)</span>
          {isStreaming && <span className="w-2 h-2 rounded-full bg-accent-green animate-pulse ml-auto" />}
        </div>

        <div className="h-[calc(100vh-20rem)] overflow-y-auto scrollbar-thin bg-surface-950">
          {filteredLogs.length === 0 ? (
            <div className="flex items-center justify-center h-full text-surface-500">No logs to display</div>
          ) : (
            filteredLogs.map(log => <LogLine key={log.id} log={log} />)
          )}
          <div ref={logsEndRef} />
        </div>
      </div>
    </div>
  );
}