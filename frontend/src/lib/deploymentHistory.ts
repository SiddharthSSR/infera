import type { Instance, ProvisionRequest, Worker } from '../types';
import { getInstanceReadiness, type InstanceReadiness } from './instanceReadiness';

const STORAGE_PREFIX = 'infera:deployment-attempts:';
const MAX_ATTEMPTS = 10;

export type DeploymentAttemptRecord = {
  id: string;
  created_at: string;
  updated_at: string;
  outcome: 'provisioned' | 'request_failed';
  request: ProvisionRequest & { name?: string };
  selected_model_name?: string;
  instance_id?: string;
  instance_name?: string;
  failure_reason?: string;
};

export type DeploymentAttemptSummary = {
  attempt: DeploymentAttemptRecord;
  readiness: InstanceReadiness & { label: string; detail: string };
  instance: Instance | null;
  retryable: boolean;
};

function storageKey(workspaceID: string) {
  return `${STORAGE_PREFIX}${workspaceID}`;
}

function canUseStorage() {
  return typeof window !== 'undefined' && typeof window.localStorage !== 'undefined';
}

function newAttemptID() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return `attempt_${Date.now()}_${Math.random().toString(36).slice(2, 10)}`;
}

function normalizeAttempts(attempts: DeploymentAttemptRecord[]) {
  return [...attempts]
    .sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at))
    .slice(0, MAX_ATTEMPTS);
}

function writeDeploymentAttempts(workspaceID: string, attempts: DeploymentAttemptRecord[]) {
  if (!canUseStorage() || !workspaceID) return;
  window.localStorage.setItem(storageKey(workspaceID), JSON.stringify(normalizeAttempts(attempts)));
}

export function readDeploymentAttempts(workspaceID: string | undefined): DeploymentAttemptRecord[] {
  if (!canUseStorage() || !workspaceID) return [];

  try {
    const raw = window.localStorage.getItem(storageKey(workspaceID));
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return normalizeAttempts(parsed.filter(Boolean));
  } catch {
    return [];
  }
}

export function recordProvisionedAttempt(
  workspaceID: string | undefined,
  request: ProvisionRequest & { name?: string },
  instance: Instance,
  selectedModelName?: string,
): DeploymentAttemptRecord[] {
  if (!workspaceID) return [];

  const now = new Date().toISOString();
  const attempts = normalizeAttempts([
    {
      id: newAttemptID(),
      created_at: now,
      updated_at: now,
      outcome: 'provisioned',
      request,
      selected_model_name: selectedModelName,
      instance_id: instance.id,
      instance_name: instance.name,
    },
    ...readDeploymentAttempts(workspaceID),
  ]);

  writeDeploymentAttempts(workspaceID, attempts);
  return attempts;
}

export function recordFailedAttempt(
  workspaceID: string | undefined,
  request: ProvisionRequest & { name?: string },
  failureReason: string,
): DeploymentAttemptRecord[] {
  if (!workspaceID) return [];

  const now = new Date().toISOString();
  const attempts = normalizeAttempts([
    {
      id: newAttemptID(),
      created_at: now,
      updated_at: now,
      outcome: 'request_failed',
      request,
      failure_reason: failureReason,
    },
    ...readDeploymentAttempts(workspaceID),
  ]);

  writeDeploymentAttempts(workspaceID, attempts);
  return attempts;
}

export function summarizeDeploymentAttempt(
  attempt: DeploymentAttemptRecord,
  instances: Instance[] | undefined,
  workers: Worker[] | undefined,
  now = new Date(),
): DeploymentAttemptSummary {
  const liveInstance = attempt.instance_id
    ? instances?.find((instance) => instance.id === attempt.instance_id) || null
    : null;

  if (attempt.outcome === 'request_failed') {
    return {
      attempt,
      instance: null,
      retryable: true,
      readiness: {
        label: 'REQUEST FAILED',
        detail: attempt.failure_reason || 'The provider request failed before an instance was created.',
        tone: 'error',
        serving: false,
        verified: false,
      },
    };
  }

  if (!liveInstance) {
    return {
      attempt,
      instance: null,
      retryable: true,
      readiness: {
        label: 'INSTANCE NOT FOUND',
        detail: 'The original node is no longer present in the current workspace inventory. Retry the same configuration if you still need capacity.',
        tone: 'inactive',
        serving: false,
        verified: false,
      },
    };
  }

  const readiness = getInstanceReadiness(liveInstance, workers, now);
  return {
    attempt,
    instance: liveInstance,
    retryable: ['error', 'stopped', 'terminated', 'terminating'].includes(liveInstance.status),
    readiness,
  };
}

export function clearDeploymentAttempts(workspaceID: string | undefined) {
  if (!canUseStorage() || !workspaceID) return;
  window.localStorage.removeItem(storageKey(workspaceID));
}
