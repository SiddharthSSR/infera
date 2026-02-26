import { Server, Cpu, Clock, Activity } from 'lucide-react';
import type { Stats } from '../types';

interface StatsCardsProps {
  stats: Stats | undefined;
  isLoading: boolean;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`;
}

function formatUptime(seconds: number): string {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}

export function StatsCards({ stats, isLoading }: StatsCardsProps) {
  const cards = [
    {
      title: 'Workers',
      value: stats ? `${stats.workers.healthy}/${stats.workers.total}` : '-',
      subtitle: 'healthy',
      icon: Server,
      color: 'text-emerald-400',
    },
    {
      title: 'Models',
      value: stats?.models.available ?? '-',
      subtitle: 'available',
      icon: Cpu,
      color: 'text-blue-400',
    },
    {
      title: 'Requests',
      value: stats ? `${stats.requests.per_second.toFixed(1)}/s` : '-',
      subtitle: `${stats?.requests.queue_depth ?? 0} queued`,
      icon: Activity,
      color: 'text-purple-400',
    },
    {
      title: 'Latency',
      value: stats ? `${stats.latency.avg_ms.toFixed(0)}ms` : '-',
      subtitle: 'avg',
      icon: Clock,
      color: 'text-amber-400',
    },
  ];

  return (
    <>
      {cards.map((card) => (
        <div key={card.title} className="stat-card">
          <div className="flex items-center justify-between mb-2">
            <span className="text-gray-400 text-sm font-medium">{card.title}</span>
            <card.icon className={`w-5 h-5 ${card.color}`} />
          </div>
          <div className="text-2xl font-bold text-white">
            {isLoading ? (
              <div className="h-8 w-16 bg-gray-800 rounded animate-pulse" />
            ) : (
              card.value
            )}
          </div>
          <div className="text-gray-500 text-sm">{card.subtitle}</div>
        </div>
      ))}
    </>
  );
}
