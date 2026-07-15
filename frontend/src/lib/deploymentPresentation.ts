import type {
  DeploymentAttemptSummary,
  DeploymentInferenceVerification,
} from './deploymentHistory';

export function getLatestDeploymentTitle(summary: DeploymentAttemptSummary): string {
  return (
    summary.attempt.selected_model_name
    || summary.attempt.instance_name
    || summary.attempt.request.name
    || summary.attempt.instance_id?.slice(0, 16)
    || 'Recent deployment'
  );
}

export function getDeploymentAttemptTitle(summary: DeploymentAttemptSummary): string {
  return (
    summary.attempt.selected_model_name
    || summary.attempt.instance_name
    || summary.attempt.request.name
    || summary.attempt.request.models?.[0]?.split('/').pop()
    || 'Provisioning attempt'
  );
}

export function formatInferenceVerificationCopy(
  verification: DeploymentInferenceVerification | undefined,
  formatAttemptTime: (value: string) => string,
  formatVerificationLatency: (latencyMs?: number) => string | null,
): string | null {
  if (!verification) return null;

  if (verification.status === 'passed') {
    const latency = verification.latency_ms != null
      ? formatVerificationLatency(verification.latency_ms) || `${verification.latency_ms}ms`
      : null;

    return `Verified on ${formatAttemptTime(verification.verified_at)}${latency ? ` in ${latency}` : ''}${verification.response_preview ? `. Response: ${verification.response_preview}` : '.'}`;
  }

  return `Inference check failed on ${formatAttemptTime(verification.verified_at)}${verification.error ? `: ${verification.error}` : '.'}`;
}
