import { useState } from 'react';
import { 
  Server, Play, Square, Trash2, DollarSign, 
  Cpu, Clock, AlertCircle, CheckCircle2, Loader2 
} from 'lucide-react';
import type { Instance } from '../types';
import { useTerminateInstance, useStartInstance, useStopInstance } from '../hooks/useInfrastructureApi';

interface InstancesListProps {
  instances: Instance[] | undefined;
  isLoading: boolean;
  onProvision: () => void;
}

function StatusBadge({ status }: { status: Instance['status'] }) {
  const colors: Record<Instance['status'], string> = {
    pending: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30',
    provisioning: 'bg-blue-500/20 text-blue-400 border-blue-500/30',
    running: 'bg-emerald-500/20 text-emerald-400 border-emerald-500/30',
    stopping: 'bg-orange-500/20 text-orange-400 border-orange-500/30',
    stopped: 'bg-gray-500/20 text-gray-400 border-gray-500/30',
    terminating: 'bg-red-500/20 text-red-400 border-red-500/30',
    terminated: 'bg-gray-500/20 text-gray-500 border-gray-500/30',
    error: 'bg-red-500/20 text-red-400 border-red-500/30',
  };

  const icons: Record<Instance['status'], typeof CheckCircle2> = {
    pending: Clock,
    provisioning: Loader2,
    running: CheckCircle2,
    stopping: Clock,
    stopped: Square,
    terminating: Loader2,
    terminated: Square,
    error: AlertCircle,
  };

  const Icon = icons[status];
  const isSpinning = status === 'provisioning' || status === 'terminating';

  return (
    <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border ${colors[status]}`}>
      <Icon className={`w-3 h-3 ${isSpinning ? 'animate-spin' : ''}`} />
      {status}
    </span>
  );
}

function GPUBadge({ gpuType, count }: { gpuType: string; count: number }) {
  return (
    <span className="inline-flex items-center gap-1.5 px-2 py-0.5 bg-purple-500/20 text-purple-300 text-xs rounded border border-purple-500/30">
      <Cpu className="w-3 h-3" />
      {count}x {gpuType.replace('_', ' ')}
    </span>
  );
}

function InstanceCard({ instance }: { instance: Instance }) {
  const [isDeleting, setIsDeleting] = useState(false);
  const terminateMutation = useTerminateInstance();
  const startMutation = useStartInstance();
  const stopMutation = useStopInstance();

  const handleTerminate = async () => {
    if (!confirm('Are you sure you want to terminate this instance?')) return;
    setIsDeleting(true);
    try {
      await terminateMutation.mutateAsync(instance.id);
    } finally {
      setIsDeleting(false);
    }
  };

  const handleStart = () => startMutation.mutate(instance.id);
  const handleStop = () => stopMutation.mutate(instance.id);

  const isRunning = instance.status === 'running';
  const isStopped = instance.status === 'stopped';

  return (
    <div className="bg-gray-900/50 border border-gray-800 rounded-lg p-4 hover:border-gray-700 transition-colors">
      <div className="flex items-start justify-between mb-3">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <Server className="w-4 h-4 text-gray-400" />
            <span className="font-medium text-white">{instance.name}</span>
            <span className="text-xs text-gray-500">({instance.id})</span>
          </div>
          <div className="flex items-center gap-2 text-xs text-gray-500">
            <span className="capitalize">{instance.provider}</span>
            {instance.spot_instance && (
              <span className="px-1.5 py-0.5 bg-amber-500/20 text-amber-400 rounded">spot</span>
            )}
          </div>
        </div>
        <StatusBadge status={instance.status} />
      </div>

      <div className="mb-3">
        <GPUBadge gpuType={instance.gpu_type} count={instance.gpu_count} />
      </div>

      {isRunning && instance.public_ip && (
        <div className="mb-3 p-2 bg-gray-800/50 rounded text-xs">
          <div className="flex items-center justify-between">
            <span className="text-gray-400">HTTP:</span>
            <code className="text-gray-300">{instance.public_ip}:{instance.http_port}</code>
          </div>
        </div>
      )}

      {instance.worker_id && (
        <div className="mb-3 text-xs">
          <span className="text-gray-400">Worker: </span>
          <span className="text-infera-400">{instance.worker_id}</span>
        </div>
      )}

      {instance.error && (
        <div className="mb-3 p-2 bg-red-500/10 border border-red-500/20 rounded text-xs text-red-400">
          {instance.error}
        </div>
      )}

      <div className="flex items-center justify-between pt-3 border-t border-gray-800">
        <div className="flex items-center gap-1 text-sm">
          <DollarSign className="w-4 h-4 text-emerald-400" />
          <span className="text-white font-medium">{instance.cost_per_hour.toFixed(2)}</span>
          <span className="text-gray-500">/hr</span>
        </div>

        <div className="flex items-center gap-2">
          {isStopped && (
            <button
              onClick={handleStart}
              disabled={startMutation.isPending}
              className="p-1.5 text-emerald-400 hover:bg-emerald-500/20 rounded transition-colors disabled:opacity-50"
              title="Start"
            >
              <Play className="w-4 h-4" />
            </button>
          )}
          {isRunning && (
            <button
              onClick={handleStop}
              disabled={stopMutation.isPending}
              className="p-1.5 text-amber-400 hover:bg-amber-500/20 rounded transition-colors disabled:opacity-50"
              title="Stop"
            >
              <Square className="w-4 h-4" />
            </button>
          )}
          <button
            onClick={handleTerminate}
            disabled={isDeleting}
            className="p-1.5 text-red-400 hover:bg-red-500/20 rounded transition-colors disabled:opacity-50"
            title="Terminate"
          >
            {isDeleting ? <Loader2 className="w-4 h-4 animate-spin" /> : <Trash2 className="w-4 h-4" />}
          </button>
        </div>
      </div>
    </div>
  );
}

export function InstancesList({ instances, isLoading, onProvision }: InstancesListProps) {
  const activeInstances = instances?.filter(i => 
    i.status !== 'terminated' && i.status !== 'terminating'
  ) || [];

  if (isLoading) {
    return (
      <div className="card">
        <h2 className="text-lg font-semibold text-white mb-4">GPU Instances</h2>
        <div className="space-y-3">
          {[1, 2].map((i) => (
            <div key={i} className="h-32 bg-gray-800 rounded-lg animate-pulse" />
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="card">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white">
          GPU Instances <span className="text-gray-500 font-normal">({activeInstances.length})</span>
        </h2>
        <button onClick={onProvision} className="btn-primary text-sm">
          + New Instance
        </button>
      </div>

      {activeInstances.length === 0 ? (
        <div className="text-center py-8">
          <Server className="w-12 h-12 text-gray-600 mx-auto mb-3" />
          <p className="text-gray-400">No GPU instances</p>
          <p className="text-gray-500 text-sm mt-1">Provision an instance to get started</p>
          <button onClick={onProvision} className="btn-secondary mt-4 text-sm">
            Provision Instance
          </button>
        </div>
      ) : (
        <div className="space-y-3">
          {activeInstances.map((instance) => (
            <InstanceCard key={instance.id} instance={instance} />
          ))}
        </div>
      )}
    </div>
  );
}
