import { useRef } from 'react';

export interface DashboardLogEntry {
  id: string;
  timestamp: Date;
  level: 'info' | 'warn' | 'error' | 'debug';
  source: string;
  message: string;
}

export function useDashboardLogs() {
  const dashLogsRef = useRef<HTMLDivElement>(null);

  return {
    dashLogs: [] as DashboardLogEntry[],
    dashLogsRef,
  };
}
