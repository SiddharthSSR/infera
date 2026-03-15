import type { DeploymentAttemptSummary } from './deploymentHistory';
import type { WorkspaceMaturityState } from './workspaceMaturity';

export type LiveWorkspaceVerificationFreshness = 'fresh' | 'recent' | 'stale' | 'missing';

export type LiveWorkspaceOperations = {
  show: boolean;
  headline: string;
  detail: string;
  activeServingModels: number;
  activeNodes: number;
  degradedRuntimeCount: number;
  verificationFreshness: LiveWorkspaceVerificationFreshness;
  verificationLabel: string;
  operatorIssueTitle: string | null;
  operatorIssueDetail: string | null;
};

type ModelServingState =
  | 'not_deployed'
  | 'runtime_pending'
  | 'serving_unverified'
  | 'serving_verified'
  | 'serving_failed'
  | 'degraded';

type AttentionItem = {
  severity: 'critical' | 'warning' | 'info';
  title: string;
  detail: string;
};

function getLatestPassedVerificationAt(deploymentSummaries: DeploymentAttemptSummary[]): string | null {
  let latest: string | null = null;
  for (const summary of deploymentSummaries) {
    const verifiedAt = summary.attempt.inference_verification?.status === 'passed'
      ? summary.attempt.inference_verification.verified_at
      : null;
    if (!verifiedAt) continue;
    if (!latest || Date.parse(verifiedAt) > Date.parse(latest)) latest = verifiedAt;
  }
  return latest;
}

function deriveVerificationFreshness(deploymentSummaries: DeploymentAttemptSummary[]): {
  freshness: LiveWorkspaceVerificationFreshness;
  label: string;
} {
  const latestVerifiedAt = getLatestPassedVerificationAt(deploymentSummaries);
  if (!latestVerifiedAt) {
    return { freshness: 'missing', label: 'NO LIVE VERIFICATION' };
  }

  const ageMs = Date.now() - Date.parse(latestVerifiedAt);
  if (ageMs <= 30 * 60 * 1000) {
    return { freshness: 'fresh', label: 'FRESH VERIFICATION' };
  }
  if (ageMs <= 6 * 60 * 60 * 1000) {
    return { freshness: 'recent', label: 'RECENT VERIFICATION' };
  }
  return { freshness: 'stale', label: 'STALE VERIFICATION' };
}

export function buildLiveWorkspaceOperations(input: {
  maturityState: WorkspaceMaturityState;
  modelServingStates: ModelServingState[];
  activeNodeCount: number;
  deploymentSummaries: DeploymentAttemptSummary[];
  operationalAttentionQueue: AttentionItem[];
}): LiveWorkspaceOperations {
  const show = input.maturityState !== 'new' && input.maturityState !== 'setup_in_progress';
  const verification = deriveVerificationFreshness(input.deploymentSummaries);
  const activeServingModels = input.modelServingStates.filter((state) => state === 'serving_verified' || state === 'serving_unverified').length;
  const degradedRuntimeCount = input.modelServingStates.filter((state) => state === 'degraded' || state === 'serving_failed').length;
  const operatorIssue = input.operationalAttentionQueue.find((item) => item.severity === 'critical' || item.severity === 'warning') || null;

  if (!show) {
    return {
      show: false,
      headline: '',
      detail: '',
      activeServingModels,
      activeNodes: input.activeNodeCount,
      degradedRuntimeCount,
      verificationFreshness: verification.freshness,
      verificationLabel: verification.label,
      operatorIssueTitle: null,
      operatorIssueDetail: null,
    };
  }

  if (operatorIssue) {
    return {
      show: true,
      headline: 'Live serving exists, but runtime operations need attention.',
      detail: operatorIssue.detail,
      activeServingModels,
      activeNodes: input.activeNodeCount,
      degradedRuntimeCount,
      verificationFreshness: verification.freshness,
      verificationLabel: verification.label,
      operatorIssueTitle: operatorIssue.title,
      operatorIssueDetail: operatorIssue.detail,
    };
  }

  if (verification.freshness === 'fresh') {
    return {
      show: true,
      headline: 'Live workspace operations look healthy right now.',
      detail: 'Serving capacity is up, the latest verification is fresh, and there is no immediate operator issue at the top of the queue.',
      activeServingModels,
      activeNodes: input.activeNodeCount,
      degradedRuntimeCount,
      verificationFreshness: verification.freshness,
      verificationLabel: verification.label,
      operatorIssueTitle: null,
      operatorIssueDetail: null,
    };
  }

  if (verification.freshness === 'recent') {
    return {
      show: true,
      headline: 'Serving is live and recently verified.',
      detail: 'The workspace is operating normally, but the last clean inference verification is no longer fresh enough to treat as immediate.',
      activeServingModels,
      activeNodes: input.activeNodeCount,
      degradedRuntimeCount,
      verificationFreshness: verification.freshness,
      verificationLabel: verification.label,
      operatorIssueTitle: null,
      operatorIssueDetail: null,
    };
  }

  return {
    show: true,
    headline: 'Serving is live, but verification freshness needs attention.',
    detail: verification.freshness === 'stale'
      ? 'The workspace has previously passed inference verification, but that proof is now stale.'
      : 'The workspace has active runtime, but there is still no clean inference verification record yet.',
    activeServingModels,
    activeNodes: input.activeNodeCount,
    degradedRuntimeCount,
    verificationFreshness: verification.freshness,
    verificationLabel: verification.label,
    operatorIssueTitle: null,
    operatorIssueDetail: null,
  };
}
