import { API_BASE, authFetch, readResponseError } from './apiCore';
import { parseDeploymentAttemptResponse, parseDeploymentAttemptsResponse } from './deploymentAttempts';
import type { DeploymentAttemptRecord } from './deploymentHistory';

export async function fetchDeploymentAttempts(): Promise<DeploymentAttemptRecord[]> {
  const response = await authFetch(`${API_BASE}/api/deployments`);
  if (!response.ok) throw new Error(await readResponseError(response, 'Failed to fetch deployment history'));
  return parseDeploymentAttemptsResponse(await response.json()).attempts;
}

export async function updateDeploymentVerification(
  attemptId: string,
  verification: NonNullable<DeploymentAttemptRecord['inference_verification']>,
): Promise<DeploymentAttemptRecord> {
  const response = await authFetch(`${API_BASE}/api/deployments/${attemptId}/verification`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(verification),
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to update deployment verification'));
  }
  return parseDeploymentAttemptResponse(await response.json()).attempt;
}

export async function markDeploymentAutoVerificationRequested(
  attemptId: string,
  requestedAt: string,
): Promise<DeploymentAttemptRecord> {
  const response = await authFetch(`${API_BASE}/api/deployments/${attemptId}/auto-verification`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ requested_at: requestedAt }),
  });
  if (!response.ok) {
    throw new Error(await readResponseError(response, 'Failed to update deployment auto verification'));
  }
  return parseDeploymentAttemptResponse(await response.json()).attempt;
}
