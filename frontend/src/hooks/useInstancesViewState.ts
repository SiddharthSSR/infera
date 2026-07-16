import { useMemo, useState } from 'react';

import { getInstanceReadiness } from '../lib/instanceReadiness';
import type { Instance, Worker } from '../types';

export function useInstancesViewState({
  searchParams,
  instances,
  workers,
}: {
  searchParams: URLSearchParams;
  instances: Instance[] | undefined;
  workers: Worker[] | undefined;
}) {
  const [statusFilter, setStatusFilter] = useState<string>('active');

  const drilldownModel = searchParams.get('model');
  const drilldownFocus = searchParams.get('focus');
  const drilldownModelLabel = drilldownModel?.split('/').pop() || drilldownModel || '';
  const allInstances = useMemo(() => instances ?? [], [instances]);
  const healthyWorkers = useMemo(
    () => workers?.filter((worker) => worker.status === 'healthy') || [],
    [workers],
  );
  const filteredInstances = useMemo(() => {
    return allInstances.filter((instance) => {
      if (drilldownModel && !(instance.models || []).includes(drilldownModel)) return false;
      if (statusFilter === 'active' && ['terminated', 'terminating'].includes(instance.status)) return false;
      if (statusFilter !== 'active' && statusFilter !== 'all' && instance.status !== statusFilter) return false;
      if (drilldownFocus === 'degraded') {
        const tone = getInstanceReadiness(instance, workers).tone;
        return tone === 'warning' || tone === 'error';
      }
      return true;
    });
  }, [allInstances, drilldownFocus, drilldownModel, statusFilter, workers]);

  return {
    statusFilter,
    setStatusFilter,
    drilldownModel,
    drilldownFocus,
    drilldownModelLabel,
    filteredInstances,
    totalInstanceCount: allInstances.length,
    healthyWorkers,
    totalGpuUtil: healthyWorkers.length > 0
      ? Math.round(healthyWorkers.reduce((sum, worker) => sum + worker.gpu_utilization, 0) / healthyWorkers.length)
      : 0,
    totalMemUsed: healthyWorkers.reduce((sum, worker) => sum + worker.memory_used, 0),
    totalMemTotal: healthyWorkers.reduce((sum, worker) => sum + worker.memory_total, 0),
    runningCount: filteredInstances.filter((instance) => instance.status === 'running').length,
    totalCostPerHour: filteredInstances.reduce((sum, instance) => sum + instance.cost_per_hour, 0),
  };
}
