import type { Instance, Worker } from '../types';

export type InstanceReadiness = {
  label: string;
  detail: string;
  tone: '' | 'warning' | 'error' | 'inactive';
  serving: boolean;
  verified: boolean;
};

const WORKER_HEARTBEAT_STALE_MS = 90 * 1000;
const WORKER_CONNECT_TIMEOUT_MS = 5 * 60 * 1000;
const MODEL_LOAD_SLOW_MS = 10 * 60 * 1000;

function parseTime(value?: string): number | null {
  if (!value) return null;
  const ts = Date.parse(value);
  return Number.isFinite(ts) ? ts : null;
}

function elapsedMinutes(startTimeMs: number | null, now: number): number | null {
  if (startTimeMs == null) return null;
  return Math.max(1, Math.round((now - startTimeMs) / 60000));
}

function readinessFromRegistrationStatus(instance: Instance): InstanceReadiness | null {
  switch (instance.worker_registration_status) {
    case 'provider_running_no_network':
      return {
        label: 'NO NETWORK',
        detail: instance.provider_network_error || 'Provider reports this node running, but no public/proxy endpoint is available yet.',
        tone: 'error',
        serving: false,
        verified: false,
      };
    case 'provider_running_worker_unregistered':
      return {
        label: 'WORKER NOT REGISTERED',
        detail: instance.last_worker_registration_error || 'Provider reports this node running, but no gateway worker registered before the deadline.',
        tone: 'error',
        serving: false,
        verified: false,
      };
    case 'worker_unreachable':
      return {
        label: 'WORKER UNREACHABLE',
        detail: instance.last_worker_registration_error || 'Worker endpoint is known but not reachable.',
        tone: 'error',
        serving: false,
        verified: false,
      };
    case 'worker_health_unavailable':
      return {
        label: 'HEALTH UNAVAILABLE',
        detail: instance.last_worker_registration_error || 'Worker endpoint responded without a usable health signal.',
        tone: 'warning',
        serving: false,
        verified: false,
      };
    case 'model_loading':
      return {
        label: 'MODEL LOADING',
        detail: 'Worker is reachable and loading the assigned model.',
        tone: 'warning',
        serving: false,
        verified: false,
      };
    case 'model_load_failed':
      return {
        label: 'MODEL LOAD FAILED',
        detail: instance.last_worker_registration_error || 'Worker failed while loading the assigned model.',
        tone: 'error',
        serving: false,
        verified: false,
      };
    case 'registration_failed':
      return {
        label: 'REGISTRATION FAILED',
        detail: instance.last_worker_registration_error || 'Worker failed gateway registration.',
        tone: 'error',
        serving: false,
        verified: false,
      };
    case 'heartbeat_missing':
      return {
        label: 'HEARTBEAT MISSING',
        detail: instance.last_worker_registration_error || 'Worker is linked to this node, but heartbeat data is missing.',
        tone: 'error',
        serving: false,
        verified: false,
      };
    case 'registered_unhealthy':
      return {
        label: 'WORKER UNHEALTHY',
        detail: instance.last_worker_registration_error || 'Worker is registered but not healthy.',
        tone: 'warning',
        serving: false,
        verified: false,
      };
    default:
      return null;
  }
}

export function getInstanceReadiness(instance: Instance, workers: Worker[] | undefined, now = new Date()): InstanceReadiness {
  const lifecycleReadiness = readinessFromRegistrationStatus(instance);
  if (lifecycleReadiness) return lifecycleReadiness;

  const nowMs = now.getTime();
  const linkedWorker = workers?.find((worker) => worker.worker_id === instance.worker_id);
  const assignedModels = instance.models || [];
  const loadedModels = new Set(linkedWorker?.models || []);
  const loadedAssignedModels = assignedModels.filter((model) => loadedModels.has(model));
  const createdAtMs = parseTime(instance.created_at);
  const heartbeatMs = parseTime(linkedWorker?.last_heartbeat);
  const instanceAgeMinutes = elapsedMinutes(createdAtMs, nowMs);
  const heartbeatAgeMs = heartbeatMs == null ? null : nowMs - heartbeatMs;
  const heartbeatFresh = heartbeatAgeMs != null && heartbeatAgeMs <= WORKER_HEARTBEAT_STALE_MS;

  switch (instance.status) {
    case 'error':
      return {
        label: 'FAILED',
        detail: instance.error || 'Provider reported an instance error during startup or serving.',
        tone: 'error',
        serving: false,
        verified: false,
      };
    case 'pending':
    case 'provisioning':
      return {
        label: 'PROVISIONING',
        detail: instanceAgeMinutes && instanceAgeMinutes >= 10
          ? `Provider accepted the request, but the node is still provisioning after ${instanceAgeMinutes} minutes.`
          : 'Provider accepted the request. Waiting for the node to boot and join the cluster.',
        tone: 'warning',
        serving: false,
        verified: false,
      };
    case 'stopping':
      return {
        label: 'STOPPING',
        detail: 'This node is shutting down and will stop serving shortly.',
        tone: 'warning',
        serving: false,
        verified: false,
      };
    case 'stopped':
      return {
        label: 'STOPPED',
        detail: 'Infrastructure exists, but the node is not currently serving.',
        tone: 'inactive',
        serving: false,
        verified: false,
      };
    case 'terminating':
    case 'terminated':
      return {
        label: 'TERMINATED',
        detail: 'This node is being removed or has already been removed.',
        tone: 'inactive',
        serving: false,
        verified: false,
      };
  }

  if (instance.status !== 'running') {
    return {
      label: 'STARTING',
      detail: 'Node state has not reached a serving-ready stage yet.',
      tone: 'warning',
      serving: false,
      verified: false,
    };
  }

  if (!instance.worker_id) {
    return {
      label: createdAtMs != null && nowMs-createdAtMs >= WORKER_CONNECT_TIMEOUT_MS ? 'WORKER NOT CONNECTED' : 'WAITING FOR WORKER',
      detail: createdAtMs != null && nowMs-createdAtMs >= WORKER_CONNECT_TIMEOUT_MS
        ? `Node has been running for ${instanceAgeMinutes} minutes without a worker connection.`
        : 'Compute is live, but the worker process has not connected back to the gateway yet.',
      tone: createdAtMs != null && nowMs-createdAtMs >= WORKER_CONNECT_TIMEOUT_MS ? 'error' : 'warning',
      serving: false,
      verified: false,
    };
  }

  if (!linkedWorker) {
    return {
      label: createdAtMs != null && nowMs-createdAtMs >= WORKER_CONNECT_TIMEOUT_MS ? 'WORKER MISSING' : 'WORKER CONNECTING',
      detail: createdAtMs != null && nowMs-createdAtMs >= WORKER_CONNECT_TIMEOUT_MS
        ? `Worker link exists, but no active worker heartbeat has been observed for ${instanceAgeMinutes} minutes.`
        : 'Worker is linked to the node, but it is not yet visible in the active worker set.',
      tone: createdAtMs != null && nowMs-createdAtMs >= WORKER_CONNECT_TIMEOUT_MS ? 'error' : 'warning',
      serving: false,
      verified: false,
    };
  }

  if (linkedWorker.status !== 'healthy') {
    return {
      label: linkedWorker.status === 'offline' || linkedWorker.status === 'unhealthy' ? 'WORKER UNHEALTHY' : 'WORKER DEGRADED',
      detail: `Worker is connected but currently ${linkedWorker.status}.`,
      tone: 'warning',
      serving: false,
      verified: false,
    };
  }

  if (!heartbeatFresh) {
    return {
      label: assignedModels.length > 0 && loadedAssignedModels.length === assignedModels.length ? 'SERVING UNVERIFIED' : 'HEARTBEAT STALE',
      detail: 'Worker health data is stale, so serving readiness cannot be verified from the latest heartbeat.',
      tone: 'warning',
      serving: assignedModels.length > 0 && loadedAssignedModels.length === assignedModels.length,
      verified: false,
    };
  }

  if (assignedModels.length === 0) {
    return {
      label: 'READY VERIFIED',
      detail: 'Node is healthy and recently heartbeat-verified, ready to accept model assignments.',
      tone: '',
      serving: false,
      verified: true,
    };
  }

  if (loadedAssignedModels.length === 0) {
    return {
      label: createdAtMs != null && nowMs-createdAtMs >= MODEL_LOAD_SLOW_MS ? 'MODEL LOAD DELAY' : 'MODEL LOADING',
      detail: createdAtMs != null && nowMs-createdAtMs >= MODEL_LOAD_SLOW_MS
        ? `Assigned model load is taking longer than expected after ${instanceAgeMinutes} minutes.`
        : `Worker is healthy. Waiting for ${assignedModels.length === 1 ? 'the assigned model' : `${assignedModels.length} assigned models`} to finish loading.`,
      tone: 'warning',
      serving: false,
      verified: false,
    };
  }

  if (loadedAssignedModels.length < assignedModels.length) {
    return {
      label: 'PARTIAL READY',
      detail: `${loadedAssignedModels.length}/${assignedModels.length} assigned models are loaded on a recently healthy worker.`,
      tone: 'warning',
      serving: false,
      verified: false,
    };
  }

  return {
    label: 'SERVING VERIFIED',
    detail: assignedModels.length === 1
      ? `${assignedModels[0].split('/').pop()} is loaded and confirmed by a recent worker heartbeat.`
      : `${assignedModels.length} assigned models are loaded and confirmed by recent worker heartbeats.`,
    tone: '',
    serving: true,
    verified: true,
  };
}
