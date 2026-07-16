import { useEffect, useRef, useState } from 'react';

export interface DashboardLogEntry {
  id: string;
  timestamp: Date;
  level: 'info' | 'warn' | 'error' | 'debug';
  source: string;
  message: string;
}

const LOG_LEVELS: DashboardLogEntry['level'][] = ['info', 'info', 'info', 'debug', 'warn', 'error'];
const LOG_SOURCES = ['GATEWAY', 'WORKER', 'SCHEDULER', 'AUTOSCALER', 'INFERENCE'];
const LOG_MESSAGES = [
  'Request accepted: model inference [req_9a2b8c]',
  'KV Cache hit rate: 0.92 for block 8410',
  'Streaming response completed in 412ms',
  'Health check passed. Latency stable.',
  'Prefill phase latency: 12ms | Decode: 40 t/s',
  'Worker heartbeat received',
  'GPU utilization: 65%',
  'Rate limit warning: 90% capacity',
  'Node approaching thermal threshold (82C)',
  'Re-routing pending tasks to cluster-us-east-b',
];

function generateDashboardLog(): DashboardLogEntry {
  return {
    id: Math.random().toString(36).slice(2),
    timestamp: new Date(),
    level: LOG_LEVELS[Math.floor(Math.random() * LOG_LEVELS.length)],
    source: LOG_SOURCES[Math.floor(Math.random() * LOG_SOURCES.length)],
    message: LOG_MESSAGES[Math.floor(Math.random() * LOG_MESSAGES.length)],
  };
}

export function useDashboardLogs() {
  const [dashLogs, setDashLogs] = useState<DashboardLogEntry[]>(() =>
    Array.from({ length: 8 }, generateDashboardLog),
  );
  const dashLogsRef = useRef<HTMLDivElement>(null);
  const [logsPrevCount, setLogsPrevCount] = useState(8);

  useEffect(() => {
    const interval = setInterval(() => {
      setDashLogs((prev) => [...prev, generateDashboardLog()].slice(-30));
    }, 3000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (dashLogs.length > logsPrevCount && dashLogsRef.current) {
      dashLogsRef.current.scrollTop = dashLogsRef.current.scrollHeight;
    }
    setLogsPrevCount(dashLogs.length);
  }, [dashLogs.length, logsPrevCount]);

  return {
    dashLogs,
    dashLogsRef,
  };
}
