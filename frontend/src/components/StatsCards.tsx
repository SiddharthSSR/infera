import { Server, Cpu, Clock, Activity } from 'lucide-react';
import type { Stats } from '../types';

interface StatsCardsProps {
  stats: Stats | undefined;
  isLoading: boolean;
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
