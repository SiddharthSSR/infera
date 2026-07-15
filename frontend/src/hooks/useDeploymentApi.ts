import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { fetchDeploymentAttempts, markDeploymentAutoVerificationRequested, updateDeploymentVerification } from '../lib/deploymentsClient';
import type { DeploymentAttemptRecord } from '../lib/deploymentHistory';
import { createVisibilityAwarePollingOptions, POLLING_INTERVALS_MS } from '../lib/polling';

export function useDeploymentAttempts(workspaceID?: string) {
  return useQuery({
    queryKey: ['deployment-attempts', workspaceID],
    queryFn: fetchDeploymentAttempts,
    enabled: Boolean(workspaceID),
    ...createVisibilityAwarePollingOptions(POLLING_INTERVALS_MS.deployments),
  });
}

function updateAttemptInCache(
  queryClient: ReturnType<typeof useQueryClient>,
  workspaceID: string | undefined,
  attempt: DeploymentAttemptRecord,
) {
  if (!workspaceID) return;
  queryClient.setQueryData<DeploymentAttemptRecord[] | undefined>(
    ['deployment-attempts', workspaceID],
    (current) => {
      if (!current) return [attempt];
      const next = [attempt, ...current.filter((item) => item.id !== attempt.id)];
      return next.sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at));
    },
  );
}

export function useUpdateDeploymentVerification(workspaceID?: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      attemptId,
      verification,
    }: {
      attemptId: string;
      verification: NonNullable<DeploymentAttemptRecord['inference_verification']>;
    }) => updateDeploymentVerification(attemptId, verification),
    onSuccess: (attempt) => {
      updateAttemptInCache(queryClient, workspaceID, attempt);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['deployment-attempts', workspaceID] });
    },
  });
}

export function useMarkDeploymentAutoVerificationRequested(workspaceID?: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ attemptId, requestedAt }: { attemptId: string; requestedAt: string }) =>
      markDeploymentAutoVerificationRequested(attemptId, requestedAt),
    onSuccess: (attempt) => {
      updateAttemptInCache(queryClient, workspaceID, attempt);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['deployment-attempts', workspaceID] });
    },
  });
}
