import type { DeploymentAttemptRecord } from './deploymentHistory';
import type { Instance, Worker } from '../types';
import { getInstanceReadiness } from './instanceReadiness';

export type ModelVerificationFreshness = 'fresh' | 'recent' | 'stale' | 'never';

export type ModelRuntimeDrilldown = {
  activeNodes: number;
  degradedNodes: number;
  pendingNodes: number;
  verificationFreshness: ModelVerificationFreshness;
  verificationLabel: string;
  latestVerificationAt?: string;
  latestVerificationLatencyMs?: number;
  latestVerificationError?: string;
  latestIssue: string | null;
};

const FRESH_VERIFICATION_MS = 30 * 60 * 1000;
const RECENT_VERIFICATION_MS = 6 * 60 * 60 * 1000;

function latestVerificationForModel(modelID: string, attempts: DeploymentAttemptRecord[]) {
  return attempts
    .filter((attempt) => attempt.inference_verification?.model === modelID)
    .sort((left, right) => Date.parse(right.inference_verification?.verified_at || right.updated_at) - Date.parse(left.inference_verification?.verified_at || left.updated_at))[0]
    ?.inference_verification;
}

function verificationFreshness(verifiedAt?: string): { freshness: ModelVerificationFreshness; label: string } {
  if (!verifiedAt) {
    return { freshness: 'never', label: 'NO VERIFY' };
  }

  const ageMs = Date.now() - Date.parse(verifiedAt);
  if (ageMs <= FRESH_VERIFICATION_MS) {
    return { freshness: 'fresh', label: 'FRESH VERIFY' };
  }
  if (ageMs <= RECENT_VERIFICATION_MS) {
    return { freshness: 'recent', label: 'RECENT VERIFY' };
  }
  return { freshness: 'stale', label: 'STALE VERIFY' };
}

export function deriveModelRuntimeDrilldown(
  modelID: string,
  instances: Instance[],
  workers: Worker[] | undefined,
  attempts: DeploymentAttemptRecord[],
  now = new Date(),
): ModelRuntimeDrilldown {
  const relatedInstances = instances.filter((instance) => (instance.models || []).includes(modelID));
  const readinessList = relatedInstances.map((instance) => getInstanceReadiness(instance, workers, now));
  const latestVerification = latestVerificationForModel(modelID, attempts);
  const freshness = verificationFreshness(latestVerification?.verified_at);
  const degradedNodes = readinessList.filter((readiness) => readiness.tone === 'error').length;
  const pendingNodes = readinessList.filter((readiness) => readiness.tone === 'warning').length;
  const latestIssue =
    latestVerification?.status === 'failed'
      ? latestVerification.error || 'Latest live inference verification failed.'
      : readinessList.find((readiness) => readiness.tone === 'error')?.detail
        || readinessList.find((readiness) => readiness.tone === 'warning')?.detail
        || null;

  return {
    activeNodes: relatedInstances.length,
    degradedNodes: latestVerification?.status === 'failed' ? Math.max(1, degradedNodes) : degradedNodes,
    pendingNodes,
    verificationFreshness: freshness.freshness,
    verificationLabel: freshness.label,
    latestVerificationAt: latestVerification?.verified_at,
    latestVerificationLatencyMs: latestVerification?.latency_ms,
    latestVerificationError: latestVerification?.status === 'failed' ? latestVerification.error : undefined,
    latestIssue,
  };
}
