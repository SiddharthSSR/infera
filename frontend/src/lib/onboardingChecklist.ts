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
          ? 'Start here: the provider path exists, but this workspace still needs a healthy live connection.'
          : 'Start here after sign-in: connect RunPod, Vast.ai, or a local inventory source before trying to deploy.',
      done: input.providerConnected,
      actionLabel: 'CONNECT PROVIDER',
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
        : 'After a deployment is ready, verify one non-streaming request before testing streaming.',
      done: input.inferenceVerified,
      actionLabel: 'OPEN NODES',
      action: 'open_clusters',
    },
    {
      id: 'collaboration',
      label: 'Add a teammate or automation identity',
      detail: input.collaborationReady
        ? 'This workspace already has a human invitation or machine service-account path set up.'
        : 'Invite a human for dashboard access, or create a service account for unattended automation. Keep those credentials separate.',
      done: input.collaborationReady,
      actionLabel: 'OPEN WORKSPACE',
      action: 'open_workspace',
    },
  ];
}
