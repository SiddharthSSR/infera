import { useState, useEffect, useRef } from 'react';
import { Terminal, Play, Pause, Trash2, Download, Filter, AlertCircle, Info, AlertTriangle, CheckCircle, Search } from 'lucide-react';
import { cn } from '../lib/utils';

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
    info: { icon: Info, color: 'text-primary', bg: 'bg-primary/10' },
    warn: { icon: AlertTriangle, color: 'text-warning', bg: 'bg-warning/10' },
    error: { icon: AlertCircle, color: 'text-destructive', bg: 'bg-destructive/10' },
    debug: { icon: CheckCircle, color: 'text-muted-foreground', bg: 'bg-muted' },
  };

  const { icon: Icon, color, bg } = levelConfig[log.level];

  return (
    <div className="flex items-start gap-3 py-2 px-3 hover:bg-muted/50 font-mono text-sm">
      <span className="text-muted-foreground text-xs w-20 flex-shrink-0">{log.timestamp.toLocaleTimeString()}</span>
      <div className={cn("w-5 h-5 rounded flex items-center justify-center flex-shrink-0", bg)}>
        <Icon className={cn("w-3 h-3", color)} />
      </div>
      <span className="text-muted-foreground w-16 flex-shrink-0 uppercase text-xs">{log.source}</span>
      <span className="text-foreground flex-1">{log.message}</span>
    </div>
  );
}

export function Logs() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [isStreaming, setIsStreaming] = useState(true);
  const [filter, setFilter] = useState<string>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const logsEndRef = useRef<HTMLDivElement>(null);
  const [initialLoading, setInitialLoading] = useState(true);

  useEffect(() => {
    if (!isStreaming) return;
    const initial = Array.from({ length: 20 }, generateMockLog);
    setLogs(initial);
    setInitialLoading(false);
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
          <button onClick={() => setIsStreaming(!isStreaming)} className={cn(
            "inline-flex items-center gap-2 px-4 py-2.5 rounded-xl font-medium transition-all",
            isStreaming
              ? "bg-success/10 text-success border border-success/20 hover:bg-success hover:text-success-foreground"
              : "bg-muted text-foreground border border-border hover:bg-accent"
          )}>
            {isStreaming ? <><Pause className="w-4 h-4" />Streaming</> : <><Play className="w-4 h-4" />Paused</>}
          </button>

          <div className="relative">
            <select value={filter} onChange={(e) => setFilter(e.target.value)} className="appearance-none bg-input border border-border rounded-xl px-4 py-2 pl-9 pr-10 text-sm text-foreground focus:outline-none cursor-pointer">
              <option value="all">All Levels</option>
              <option value="info">Info</option>
              <option value="warn">Warning</option>
              <option value="error">Error</option>
              <option value="debug">Debug</option>
            </select>
            <Filter className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
          </div>

          <div className="relative">
            <input type="text" value={searchQuery} onChange={(e) => setSearchQuery(e.target.value)} placeholder="Search logs..." className="bg-input border border-border rounded-xl py-2 pl-9 pr-4 text-sm text-foreground placeholder-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring w-48" />
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
          </div>
        </div>

        <div className="flex items-center gap-2">
          <button onClick={() => setLogs([])} className="inline-flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-muted rounded-xl transition-colors">
            <Trash2 className="w-4 h-4" />Clear
          </button>
          <button onClick={handleExport} className="inline-flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-muted rounded-xl transition-colors">
            <Download className="w-4 h-4" />Export
          </button>
        </div>
      </div>

      <div className="bg-card backdrop-blur-sm border border-border rounded-2xl overflow-hidden">
        <div className="flex items-center gap-2 px-4 py-3 border-b border-border bg-muted/50">
          <Terminal className="w-4 h-4 text-primary" />
          <span className="text-sm font-medium text-foreground">System Logs</span>
          <span className="text-xs text-muted-foreground">({filteredLogs.length} entries)</span>
          {isStreaming && <span className="w-2 h-2 rounded-full bg-success animate-pulse ml-auto" />}
        </div>

        <div className="h-[calc(100vh-20rem)] overflow-y-auto scrollbar-thin bg-background">
          {initialLoading ? (
            <div className="p-4 space-y-2">
              {Array.from({ length: 8 }).map((_, i) => (
                <div key={i} className="h-6 bg-muted rounded animate-pulse" />
              ))}
            </div>
          ) : filteredLogs.length === 0 ? (
            <div className="flex items-center justify-center h-full text-muted-foreground">No logs to display</div>
          ) : (
            filteredLogs.map(log => <LogLine key={log.id} log={log} />)
          )}
          <div ref={logsEndRef} />
        </div>
      </div>
    </div>
  );
}
