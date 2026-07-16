import type { DeploymentAttemptSummary } from './deploymentHistory';
import { getInstanceReadiness } from './instanceReadiness';
import { formatShortTimestamp } from './formatting';
import type { Instance, Worker } from '../types';

export type NodeIncident = {
  title: string;
  detail: string;
  tone: '' | 'warning' | 'error' | 'inactive';
};

export function deriveNodeIncident(
  instance: Instance,
  workers: Worker[] | undefined,
  summary: DeploymentAttemptSummary | null,
): NodeIncident | null {
  const readiness = getInstanceReadiness(instance, workers);
  const verification = summary?.attempt.inference_verification;
  const formatAttemptTime = (value: string) => formatShortTimestamp(value) ?? value;

  if (verification?.status === 'failed') {
    return {
      title: 'INFERENCE CHECK FAILED',
      detail: verification.error
        ? `Latest verification failed on ${formatAttemptTime(verification.verified_at)}: ${verification.error}`
        : `Latest verification failed on ${formatAttemptTime(verification.verified_at)}.`,
      tone: 'error',
    };
  }

  if (instance.status === 'error') {
    return {
      title: 'PROVIDER INCIDENT',
      detail: instance.error || 'Provider reported a node error during startup or serving.',
      tone: 'error',
    };
  }

  switch (readiness.label) {
    case 'WORKER NOT CONNECTED':
    case 'WORKER MISSING':
    case 'WORKER UNHEALTHY':
    case 'WORKER DEGRADED':
      return {
        title: readiness.label,
        detail: readiness.detail,
        tone: readiness.tone,
      };
    case 'MODEL LOADING':
    case 'MODEL LOAD DELAY':
    case 'PARTIAL READY':
      return {
        title: 'MODEL RUNTIME ISSUE',
        detail: readiness.detail,
        tone: readiness.tone,
      };
    case 'HEARTBEAT STALE':
    case 'SERVING UNVERIFIED':
      return {
        title: 'VERIFICATION STALE',
        detail: readiness.detail,
        tone: readiness.tone,
      };
    default:
      return null;
  }
}
