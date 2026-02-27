import { Server, Cpu, HardDrive, AlertCircle, CheckCircle2, Clock } from 'lucide-react';
import type { Worker } from '../types';

interface WorkersListProps {
  workers: Worker[] | undefined;
  isLoading: boolean;
}


function StatusBadge({ status }: { status: Worker['status'] }) {
  const colors = {
    healthy: 'bg-emerald-500/20 text-emerald-400 border-emerald-500/30',
    degraded: 'bg-amber-500/20 text-amber-400 border-amber-500/30',
    unhealthy: 'bg-red-500/20 text-red-400 border-red-500/30',
    draining: 'bg-blue-500/20 text-blue-400 border-blue-500/30',
    offline: 'bg-gray-500/20 text-gray-400 border-gray-500/30',
  };

  const icons = {
    healthy: CheckCircle2,
    degraded: AlertCircle,
    unhealthy: AlertCircle,
    draining: Clock,
    offline: AlertCircle,
  };

  const Icon = icons[status];

  return (
    <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border ${colors[status]}`}>
      <Icon className="w-3 h-3" />
      {status}
    </span>
  );
}

function WorkerCard({ worker }: { worker: Worker }) {
  const memoryPercent = worker.memory_total > 0 
    ? (worker.memory_used / worker.memory_total) * 100 
    : 0;
  const gpuPercent = worker.gpu_utilization * 100;

  return (
    <div className="bg-gray-900/50 border border-gray-800 rounded-lg p-4 hover:border-gray-700 transition-colors">
      <div className="flex items-start justify-between mb-3">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <Server className="w-4 h-4 text-gray-400" />
            <span className="font-medium text-white">{worker.worker_id}</span>
          </div>
          <div className="text-xs text-gray-500">{worker.address}</div>
        </div>
        <StatusBadge status={worker.status} />
      </div>

      {/* Models */}
      <div className="mb-3">
        <div className="flex flex-wrap gap-1.5">
          {worker.models.length > 0 ? (
            worker.models.map((model) => (
              <span 
                key={model} 
                className="px-2 py-0.5 bg-gray-800 text-gray-300 text-xs rounded"
              >
                {model}
              </span>
            ))
          ) : (
            <span className="text-gray-500 text-xs">No models loaded</span>
          )}
        </div>
      </div>

      {/* Metrics */}
      <div className="grid grid-cols-2 gap-3 text-sm">
        <div>
          <div className="flex items-center gap-1.5 text-gray-400 mb-1">
            <Cpu className="w-3.5 h-3.5" />
            <span className="text-xs">GPU</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex-1 h-1.5 bg-gray-800 rounded-full overflow-hidden">
              <div 
                className="h-full bg-infera-500 rounded-full transition-all"
                style={{ width: `${gpuPercent}%` }}
              />
            </div>
            <span className="text-xs text-gray-300 w-10 text-right">{gpuPercent.toFixed(0)}%</span>
          </div>
        </div>

        <div>
          <div className="flex items-center gap-1.5 text-gray-400 mb-1">
            <HardDrive className="w-3.5 h-3.5" />
            <span className="text-xs">Memory</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex-1 h-1.5 bg-gray-800 rounded-full overflow-hidden">
              <div 
                className="h-full bg-purple-500 rounded-full transition-all"
                style={{ width: `${memoryPercent}%` }}
              />
            </div>
            <span className="text-xs text-gray-300 w-10 text-right">{memoryPercent.toFixed(0)}%</span>
          </div>
        </div>
      </div>

      {/* Stats Row */}
      <div className="flex items-center gap-4 mt-3 pt-3 border-t border-gray-800 text-xs text-gray-400">
        <div>
          <span className="text-gray-300">{worker.requests_per_sec.toFixed(1)}</span> req/s
        </div>
        <div>
          <span className="text-gray-300">{worker.avg_latency_ms.toFixed(0)}</span>ms avg
        </div>
        <div>
          <span className="text-gray-300">{worker.queue_depth}</span> queued
        </div>
      </div>
    </div>
  );
}

export function WorkersList({ workers, isLoading }: WorkersListProps) {
  if (isLoading) {
    return (
      <div className="card">
        <h2 className="text-lg font-semibold text-white mb-4">Workers</h2>
        <div className="space-y-3">
          {[1, 2].map((i) => (
            <div key={i} className="h-32 bg-gray-800 rounded-lg animate-pulse" />
          ))}
        </div>
      </div>
    );
  }

  if (!workers || workers.length === 0) {
    return (
      <div className="card">
        <h2 className="text-lg font-semibold text-white mb-4">Workers</h2>
        <div className="text-center py-8">
          <Server className="w-12 h-12 text-gray-600 mx-auto mb-3" />
          <p className="text-gray-400">No workers connected</p>
          <p className="text-gray-500 text-sm mt-1">Start a worker to begin</p>
        </div>
      </div>
    );
  }

  return (
    <div className="card">
      <h2 className="text-lg font-semibold text-white mb-4">
        Workers <span className="text-gray-500 font-normal">({workers.length})</span>
      </h2>
      <div className="space-y-3">
        {workers.map((worker) => (
          <WorkerCard key={worker.worker_id} worker={worker} />
        ))}
      </div>
    </div>
  );
}
