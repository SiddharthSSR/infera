import type { OnboardingAction, OnboardingItem } from './onboardingChecklist';

export type WorkspaceMaturityState =
  | 'new'
  | 'setup_in_progress'
  | 'serving_unverified'
  | 'serving_verified'
  | 'attention_required';

export type WorkspaceMaturityAction = OnboardingAction | 'verify_now';

export type WorkspaceMaturityAttention = {
  severity: 'critical' | 'warning' | 'info';
  title: string;
  detail: string;
  actionLabel: string;
  action: 'open_clusters' | 'open_models' | 'open_workspace' | 'verify_now';
};

export type WorkspaceMaturity = {
  state: WorkspaceMaturityState;
  label: string;
  tone: '' | 'warning' | 'error' | 'inactive';
  headline: string;
  detail: string;
  actionLabel: string;
  action: WorkspaceMaturityAction;
};

export function buildWorkspaceMaturity(input: {
  checklist: OnboardingItem[];
  attentionQueue: WorkspaceMaturityAttention[];
  servingVerifiedCount: number;
  servingUnverifiedCount: number;
  pendingDeploymentCount: number;
  activeInstanceCount: number;
}): WorkspaceMaturity {
  const nextChecklistItem = input.checklist.find((item) => !item.done);
  const completedSteps = input.checklist.filter((item) => item.done).length;
  const criticalAttention = input.attentionQueue.find((item) => item.severity === 'critical');
  const warningAttention = input.attentionQueue.find((item) => item.severity === 'warning');

  if (criticalAttention) {
    return {
      state: 'attention_required',
      label: 'ATTENTION REQUIRED',
      tone: 'error',
      headline: criticalAttention.title,
      detail: criticalAttention.detail,
      actionLabel: criticalAttention.actionLabel,
      action: criticalAttention.action,
    };
  }

  if (nextChecklistItem) {
    if (completedSteps === 0) {
      return {
        state: 'new',
        label: 'NEW WORKSPACE',
        tone: 'inactive',
        headline: 'The workspace is still at day-zero setup.',
        detail: nextChecklistItem.detail,
        actionLabel: nextChecklistItem.actionLabel,
        action: nextChecklistItem.action,
      };
    }

    return {
      state: 'setup_in_progress',
      label: 'SETUP IN PROGRESS',
      tone: 'warning',
      headline: `${completedSteps} of ${input.checklist.length} setup steps are complete.`,
      detail: nextChecklistItem.detail,
      actionLabel: nextChecklistItem.actionLabel,
      action: nextChecklistItem.action,
    };
  }

  if (warningAttention) {
    return {
      state: 'attention_required',
      label: 'ATTENTION REQUIRED',
      tone: 'warning',
      headline: warningAttention.title,
      detail: warningAttention.detail,
      actionLabel: warningAttention.actionLabel,
      action: warningAttention.action,
    };
  }

  if (input.servingVerifiedCount > 0) {
    return {
      state: 'serving_verified',
      label: 'SERVING VERIFIED',
      tone: '',
      headline: `${input.servingVerifiedCount} model${input.servingVerifiedCount === 1 ? '' : 's'} passed live inference verification.`,
      detail: 'The workspace has moved past setup and has at least one deployment that answered a real chat-completions request successfully.',
      actionLabel: 'OPEN MODELS',
      action: 'open_models',
    };
  }

  if (input.servingUnverifiedCount > 0 || input.pendingDeploymentCount > 0 || input.activeInstanceCount > 0) {
    return {
      state: 'serving_unverified',
      label: 'SERVING UNVERIFIED',
      tone: 'warning',
      headline: 'Runtime looks active, but the workspace still needs a clean live verification result.',
      detail: input.servingUnverifiedCount > 0
        ? `${input.servingUnverifiedCount} model${input.servingUnverifiedCount === 1 ? '' : 's'} still need inference verification.`
        : `${input.pendingDeploymentCount} deployment${input.pendingDeploymentCount === 1 ? '' : 's'} are still moving through provisioning or model loading.`,
      actionLabel: input.servingUnverifiedCount > 0 ? 'VERIFY NOW' : 'OPEN CLUSTERS',
      action: input.servingUnverifiedCount > 0 ? 'verify_now' : 'open_clusters',
    };
  }

  return {
    state: 'setup_in_progress',
    label: 'SETUP IN PROGRESS',
    tone: 'inactive',
    headline: 'The workspace has partial infrastructure state, but the next step is still operational setup.',
    detail: 'Review models, clusters, and workspace configuration to finish turning the workspace into a serving environment.',
    actionLabel: 'OPEN MODELS',
    action: 'open_models',
  };
}
