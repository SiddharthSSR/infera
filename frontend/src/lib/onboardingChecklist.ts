export type OnboardingAction = 'open_workspace' | 'open_models' | 'open_clusters' | 'open_docs' | 'open_api_keys';

export type OnboardingItem = {
  id: string;
  label: string;
  detail: string;
  done: boolean;
  actionLabel: string;
  action: OnboardingAction;
};

export function buildFirstWorkspaceChecklist(input: {
  providerReady: boolean;
  providerConnected: boolean;
  modelReady: boolean;
  nodeReady: boolean;
  inferenceVerified: boolean;
  collaborationReady: boolean;
}): OnboardingItem[] {
  return [
    {
      id: 'provider',
      label: 'Add provider access',
      detail: input.providerConnected
        ? 'A live inventory source is connected and returning current capacity.'
        : input.providerReady
          ? 'A provider path exists, but the workspace still needs a healthy live connection.'
          : 'Connect RunPod, Vast.ai, or a local inventory source before trying to deploy.',
      done: input.providerConnected,
      actionLabel: 'OPEN WORKSPACE',
      action: 'open_workspace',
    },
    {
      id: 'model',
      label: 'Register or confirm a model',
      detail: input.modelReady
        ? 'At least one model is available in the active workspace.'
        : 'Add a model to the registry so the workspace has something to deploy.',
      done: input.modelReady,
      actionLabel: 'OPEN MODELS',
      action: 'open_models',
    },
    {
      id: 'node',
      label: 'Provision first node',
      detail: input.nodeReady
        ? 'The workspace has at least one node in inventory.'
        : 'Provision a node from Nodes after provider access and a model are ready.',
      done: input.nodeReady,
      actionLabel: 'OPEN NODES',
      action: 'open_clusters',
    },
    {
      id: 'verify',
      label: 'Verify first inference',
      detail: input.inferenceVerified
        ? 'A real chat-completions request already passed for this workspace.'
        : 'Run or wait for the first inference verification after a deployment becomes ready.',
      done: input.inferenceVerified,
      actionLabel: 'OPEN NODES',
      action: 'open_clusters',
    },
    {
      id: 'collaboration',
      label: 'Add a teammate or automation identity',
      detail: input.collaborationReady
        ? 'This workspace already has an invite or service-account path set up.'
        : 'Create a service account for automation or send the first invite for another human teammate.',
      done: input.collaborationReady,
      actionLabel: 'OPEN WORKSPACE',
      action: 'open_workspace',
    },
  ];
}
