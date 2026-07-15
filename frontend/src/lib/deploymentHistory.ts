import type {
  DeploymentAttemptRecord,
  DeploymentInferenceVerification,
  Instance,
  ProvisionRequest,
  Worker,
} from '../types';
import { getInstanceReadiness, type InstanceReadiness } from './instanceReadiness';

const STORAGE_PREFIX = 'infera:deployment-attempts:';
const MAX_ATTEMPTS = 10;

export type { DeploymentAttemptRecord, DeploymentInferenceVerification } from '../types';

export type DeploymentAttemptSummary = {
  attempt: DeploymentAttemptRecord;
  readiness: InstanceReadiness & { label: string; detail: string };
  instance: Instance | null;
  retryable: boolean;
  inferenceVerified: boolean;
  autoVerificationRequested: boolean;
};

export type DeploymentTimelineStep = {
  label: string;
  state: 'done' | 'active' | 'pending' | 'failed' | 'stopped' | 'terminated';
};

export type DeploymentRemediation = {
  label: string;
  detail: string;
  action: 'open_workspace' | 'view_capacity' | 'retry_config' | 'focus_instance' | 'verify_inference';
};

function deploymentPriority(summary: DeploymentAttemptSummary): number {
  if (summary.inferenceVerified) return 6;
  if (summary.readiness.label === 'SERVING VERIFIED') return 5;
  if (summary.readiness.serving) return 4;
  if (summary.instance?.status === 'running') return 3;
  if (summary.instance) return 2;
  if (summary.attempt.outcome === 'request_failed') return 0;
  return 1;
}

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
      workspace_id: workspaceID,
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
      workspace_id: workspaceID,
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

export function recordInferenceVerification(
  workspaceID: string | undefined,
  attemptID: string,
  verification: DeploymentInferenceVerification,
): DeploymentAttemptRecord[] {
  if (!workspaceID) return [];

  const attempts = normalizeAttempts(
    readDeploymentAttempts(workspaceID).map((attempt) => (
      attempt.id === attemptID
        ? {
            ...attempt,
            updated_at: verification.verified_at,
            inference_verification: verification,
          }
        : attempt
    )),
  );

  writeDeploymentAttempts(workspaceID, attempts);
  return attempts;
}

export function markAutoVerificationRequested(
  workspaceID: string | undefined,
  attemptID: string,
  requestedAt = new Date().toISOString(),
): DeploymentAttemptRecord[] {
  if (!workspaceID) return [];

  const attempts = normalizeAttempts(
    readDeploymentAttempts(workspaceID).map((attempt) => (
      attempt.id === attemptID
        ? {
            ...attempt,
            auto_verification_requested_at: attempt.auto_verification_requested_at || requestedAt,
          }
        : attempt
    )),
  );

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
      inferenceVerified: false,
      autoVerificationRequested: false,
    };
  }

  if (!liveInstance) {
    const inferenceVerified = attempt.inference_verification?.status === 'passed';
    const nodeRan = inferenceVerified || Boolean(attempt.auto_verification_requested_at);
    return {
      attempt,
      instance: null,
      retryable: true,
      readiness: nodeRan
        ? {
            label: 'NODE TERMINATED',
            detail: inferenceVerified
              ? 'This node ran and served traffic successfully before being removed.'
              : 'This node was running and reached a healthy state before being removed.',
            tone: 'inactive',
            serving: false,
            verified: false,
          }
        : {
            label: 'INSTANCE NOT FOUND',
            detail: 'The original node is no longer present in the current workspace inventory. Retry the same configuration if you still need capacity.',
            tone: 'inactive',
            serving: false,
            verified: false,
          },
      inferenceVerified,
      autoVerificationRequested: Boolean(attempt.auto_verification_requested_at),
    };
  }

  const readiness = getInstanceReadiness(liveInstance, workers, now);
  return {
    attempt,
    instance: liveInstance,
    retryable: ['error', 'stopped', 'terminated', 'terminating'].includes(liveInstance.status),
    readiness,
    inferenceVerified: attempt.inference_verification?.status === 'passed',
    autoVerificationRequested: Boolean(attempt.auto_verification_requested_at),
  };
}

export function selectPrimaryDeploymentSummary(
  summaries: DeploymentAttemptSummary[],
): DeploymentAttemptSummary | null {
  if (summaries.length === 0) return null;

  return [...summaries].sort((left, right) => {
    const priorityDelta = deploymentPriority(right) - deploymentPriority(left);
    if (priorityDelta !== 0) return priorityDelta;
    return Date.parse(right.attempt.updated_at) - Date.parse(left.attempt.updated_at);
  })[0] || null;
}

function hasReason(reason: string | undefined, ...patterns: string[]) {
  const normalized = (reason || '').toLowerCase();
  return patterns.some((pattern) => normalized.includes(pattern));
}

export function getDeploymentTimeline(summary: DeploymentAttemptSummary): DeploymentTimelineStep[] {
  const steps: DeploymentTimelineStep[] = [
    { label: 'Requested', state: 'done' },
    { label: 'Provider accepted', state: 'pending' },
    { label: 'Node running', state: 'pending' },
    { label: 'Worker connected', state: 'pending' },
    { label: 'Models ready', state: 'pending' },
    { label: 'Serving verified', state: 'pending' },
    { label: 'First inference', state: 'pending' },
  ];

  if (summary.attempt.outcome === 'request_failed') {
    steps[1].state = 'failed';
    return steps;
  }

  steps[1].state = 'done';

  if (!summary.instance) {
    // Node ran and was later removed — reflect how far it got before disappearing.
    const inferenceVerified = summary.attempt.inference_verification?.status === 'passed';
    const nodeRan = inferenceVerified || Boolean(summary.attempt.auto_verification_requested_at);
    if (nodeRan) {
      steps[2].state = 'terminated';
      steps[3].state = 'done';
      steps[4].state = 'done';
      steps[5].state = inferenceVerified ? 'done' : 'done';
      steps[6].state = inferenceVerified ? 'done' : 'pending';
    } else {
      steps[2].state = 'failed';
    }
    return steps;
  }

  switch (summary.instance.status) {
    case 'pending':
    case 'provisioning':
      steps[2].state = 'active';
      return steps;
    case 'error':
      steps[2].state = 'failed';
      return steps;
    case 'stopped':
    case 'stopping':
      steps[2].state = 'stopped';
      steps[3].state = 'done';
      steps[4].state = 'done';
      steps[5].state = summary.attempt.inference_verification?.status === 'passed' ? 'done' : 'pending';
      steps[6].state = summary.attempt.inference_verification?.status === 'passed' ? 'done' : 'pending';
      return steps;
    case 'terminating':
    case 'terminated':
      steps[2].state = 'terminated';
      steps[3].state = 'done';
      steps[4].state = 'done';
      steps[5].state = summary.attempt.inference_verification?.status === 'passed' ? 'done' : 'pending';
      steps[6].state = summary.attempt.inference_verification?.status === 'passed' ? 'done' : 'pending';
      return steps;
    case 'running':
      steps[2].state = 'done';
      break;
  }

  switch (summary.readiness.label) {
    case 'WAITING FOR WORKER':
    case 'WORKER CONNECTING':
      steps[3].state = 'active';
      return steps;
    case 'WORKER NOT CONNECTED':
    case 'WORKER MISSING':
    case 'WORKER UNHEALTHY':
    case 'WORKER DEGRADED':
      steps[3].state = 'failed';
      return steps;
    default:
      steps[3].state = 'done';
  }

  switch (summary.readiness.label) {
    case 'READY VERIFIED':
      steps[4].state = 'done';
      return steps;
    case 'MODEL LOADING':
    case 'MODEL LOAD DELAY':
      steps[4].state = 'active';
      return steps;
    case 'PARTIAL READY':
      steps[4].state = 'active';
      steps[5].state = 'pending';
      return steps;
    case 'HEARTBEAT STALE':
      steps[4].state = 'done';
      steps[5].state = 'active';
      return steps;
    case 'SERVING UNVERIFIED':
      steps[4].state = 'done';
      steps[5].state = 'active';
      return steps;
    case 'SERVING VERIFIED':
      steps[4].state = 'done';
      steps[5].state = 'done';
      if (summary.attempt.inference_verification?.status === 'passed') {
        steps[6].state = 'done';
      } else if (summary.attempt.inference_verification?.status === 'failed') {
        steps[6].state = 'failed';
      } else {
        steps[6].state = 'active';
      }
      return steps;
    case 'FAILED':
      steps[4].state = 'failed';
      return steps;
    default:
      return steps;
  }
}

export function getDeploymentRemediation(summary: DeploymentAttemptSummary): DeploymentRemediation | null {
  if (summary.attempt.outcome === 'request_failed') {
    const reason = summary.attempt.failure_reason;

    if (hasReason(reason, 'auth', 'credential', 'api key', 'forbidden', 'unauthor', 'provider config')) {
      return {
        label: 'FIX PROVIDER SETUP',
        detail: 'This failure points to provider credentials or workspace configuration. Review provider setup before retrying.',
        action: 'open_workspace',
      };
    }

    if (hasReason(reason, 'offering', 'inventory', 'capacity', 'unavailable', 'no gpu', 'no live')) {
      return {
        label: 'VIEW CAPACITY',
        detail: 'This request failed because matching capacity was unavailable. Reopen provisioning and inspect live offerings.',
        action: 'view_capacity',
      };
    }

    return {
      label: 'RETRY CONFIG',
      detail: 'The provider request failed before creating a node. Retry the same configuration after reviewing the request.',
      action: 'retry_config',
    };
  }

  if (!summary.instance) {
    const nodeRan = summary.attempt.inference_verification?.status === 'passed'
      || Boolean(summary.attempt.auto_verification_requested_at);
    if (nodeRan) {
      return {
        label: 'PROVISION NEW NODE',
        detail: 'This node ran successfully and has since been removed. Provision a new node with the same configuration if you need capacity again.',
        action: 'retry_config',
      };
    }
    return {
      label: 'RETRY CONFIG',
      detail: 'The original node is no longer present. Reopen the previous configuration to reprovision it.',
      action: 'retry_config',
    };
  }

  const { instance } = summary;
  if (instance.status === 'terminated' || instance.status === 'terminating') {
    return {
      label: 'PROVISION NEW NODE',
      detail: 'This node has been terminated. Provision a new node with the same configuration if you need capacity again.',
      action: 'retry_config',
    };
  }
  if (instance.status === 'stopped' || instance.status === 'stopping') {
    return {
      label: 'PROVISION NEW NODE',
      detail: 'This node is stopped. Provision a new node with the same configuration if you need capacity again.',
      action: 'retry_config',
    };
  }

  if (summary.readiness.label === 'SERVING VERIFIED' && !summary.inferenceVerified) {
    if (summary.attempt.inference_verification?.status === 'failed') {
      return {
        label: 'FOCUS NODE',
        detail: `Runtime looks ready, but the first live inference failed${summary.attempt.inference_verification.error ? `: ${summary.attempt.inference_verification.error}` : '.'}`,
        action: 'focus_instance',
      };
    }

    return {
      label: summary.autoVerificationRequested ? 'VERIFY NOW' : 'VERIFY SERVING',
      detail: summary.autoVerificationRequested
        ? 'Automatic first-inference verification has already been queued for this deployment. You can still run it manually if needed.'
        : 'The runtime is healthy. Run one small inference request to confirm the model is actually answering traffic.',
      action: 'verify_inference',
    };
  }

  switch (summary.readiness.label) {
    case 'FAILED':
      return {
        label: 'FOCUS NODE',
        detail: 'Inspect the node entry and provider error, then retry or terminate it from the main cluster list.',
        action: 'focus_instance',
      };
    case 'WORKER NOT CONNECTED':
    case 'WORKER MISSING':
    case 'WORKER UNHEALTHY':
    case 'WORKER DEGRADED':
    case 'HEARTBEAT STALE':
    case 'MODEL LOAD DELAY':
    case 'PARTIAL READY':
    case 'SERVING UNVERIFIED':
      return {
        label: 'FOCUS NODE',
        detail: 'Jump to the live node row to inspect current runtime state before retrying or restarting it.',
        action: 'focus_instance',
      };
    case 'WAITING FOR WORKER':
    case 'WORKER CONNECTING':
    case 'MODEL LOADING':
    case 'PROVISIONING':
      return {
        label: 'TRACK NODE',
        detail: 'The deployment is still progressing. Jump to the live node row and monitor the next transition.',
        action: 'focus_instance',
      };
    default:
      return null;
  }
}

export function clearDeploymentAttempts(workspaceID: string | undefined) {
  if (!canUseStorage() || !workspaceID) return;
  window.localStorage.removeItem(storageKey(workspaceID));
}
