import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';

import { sendChatCompletion } from '../lib/chatClient';
import { publicAnalytics } from '../lib/publicAnalytics';
import {
  summarizeDeploymentAttempt,
  type DeploymentAttemptRecord,
  type DeploymentAttemptSummary,
  type DeploymentRemediation,
} from '../lib/deploymentHistory';
import { formatLatency } from '../lib/formatting';
import { deriveNodeIncident, type NodeIncident } from '../lib/instanceIncidents';
import { useMarkDeploymentAutoVerificationRequested, useUpdateDeploymentVerification } from './useDeploymentApi';
import type { Instance, Worker } from '../types';

const AUTO_VERIFY_DELAY_MS = 1500;

export type IncidentRow = {
  instance: Instance;
  summary: DeploymentAttemptSummary | null;
  incident: NodeIncident;
};

export function useInstanceDeploymentFlow({
  workspaceID,
  deploymentAttempts,
  instances,
  workers,
  filteredInstances,
  onOpenWorkspace,
  onRetry,
  onFocusInstance,
}: {
  workspaceID: string | undefined;
  deploymentAttempts: DeploymentAttemptRecord[];
  instances: Instance[] | undefined;
  workers: Worker[] | undefined;
  filteredInstances: Instance[];
  onOpenWorkspace: () => void;
  onRetry: (attempt: DeploymentAttemptRecord) => void;
  onFocusInstance: (instanceID: string) => void;
}) {
  const [verifyingAttemptID, setVerifyingAttemptID] = useState<string | null>(null);
  const autoVerifyTimerRef = useRef<number | null>(null);
  const updateDeploymentVerification = useUpdateDeploymentVerification(workspaceID);
  const markAutoVerificationRequested = useMarkDeploymentAutoVerificationRequested(workspaceID);

  const deploymentHistory = useMemo(
    () => deploymentAttempts.map((attempt) => summarizeDeploymentAttempt(attempt, instances, workers)),
    [deploymentAttempts, instances, workers],
  );
  const latestDeployment = deploymentHistory[0] || null;
  const deploymentSummaryByInstanceID = useMemo(
    () => new Map(
      deploymentHistory
        .filter((summary): summary is DeploymentAttemptSummary & { instance: Instance } => Boolean(summary.instance?.id))
        .map((summary) => [summary.instance.id, summary]),
    ),
    [deploymentHistory],
  );
  const incidentRows = useMemo(
    () => filteredInstances.flatMap((instance) => {
      const summary = deploymentSummaryByInstanceID.get(instance.id) || null;
      const incident = deriveNodeIncident(instance, workers, summary);
      return incident ? [{ instance, summary, incident }] : [];
    }),
    [deploymentSummaryByInstanceID, filteredInstances, workers],
  );

  const runInferenceVerification = useCallback(async (summary: DeploymentAttemptSummary) => {
    const model = summary.instance?.models?.[0] || summary.attempt.request.models?.[0];
    if (!model) {
      toast.error('No deployed model is available to verify');
      return;
    }

    setVerifyingAttemptID(summary.attempt.id);
    const startedAt = Date.now();

    try {
      const response = await sendChatCompletion({
        model,
        messages: [
          { role: 'system', content: 'Reply with a short readiness confirmation.' },
          { role: 'user', content: 'Return a short response confirming that inference is working.' },
        ],
        temperature: 0,
        max_tokens: 16,
      });

      publicAnalytics.trackFirst('activation_first_unary_inference_succeeded', { surface: 'onboarding' });

      const latencyMs = Date.now() - startedAt;
      const content = response.choices?.[0]?.message?.content?.trim() || '';
      await updateDeploymentVerification.mutateAsync({
        attemptId: summary.attempt.id,
        verification: {
          status: 'passed',
          verified_at: new Date().toISOString(),
          latency_ms: latencyMs,
          model,
          response_preview: content.slice(0, 120),
        },
      });
      toast.success(`Inference verified in ${formatLatency(latencyMs) || `${latencyMs}ms`}`);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Verification request failed';
      await updateDeploymentVerification.mutateAsync({
        attemptId: summary.attempt.id,
        verification: {
          status: 'failed',
          verified_at: new Date().toISOString(),
          model,
          error: message,
        },
      });
      toast.error(message);
    } finally {
      setVerifyingAttemptID(null);
    }
  }, [updateDeploymentVerification]);

  const handleRemediation = useCallback((summary: DeploymentAttemptSummary, remediation: DeploymentRemediation | null) => {
    if (!remediation) return;

    switch (remediation.action) {
      case 'open_workspace':
        onOpenWorkspace();
        return;
      case 'view_capacity':
      case 'retry_config':
        onRetry(summary.attempt);
        return;
      case 'focus_instance':
        if (summary.instance?.id) onFocusInstance(summary.instance.id);
        return;
      case 'verify_inference':
        void runInferenceVerification(summary);
        return;
    }
  }, [onFocusInstance, onOpenWorkspace, onRetry, runInferenceVerification]);

  useEffect(() => {
    if (autoVerifyTimerRef.current) {
      window.clearTimeout(autoVerifyTimerRef.current);
      autoVerifyTimerRef.current = null;
    }

    if (
      !latestDeployment
      || latestDeployment.readiness.label !== 'SERVING VERIFIED'
      || latestDeployment.inferenceVerified
      || latestDeployment.autoVerificationRequested
      || latestDeployment.attempt.inference_verification?.status === 'failed'
      || verifyingAttemptID
    ) {
      return;
    }

    void markAutoVerificationRequested.mutateAsync({
      attemptId: latestDeployment.attempt.id,
      requestedAt: new Date().toISOString(),
    });
    autoVerifyTimerRef.current = window.setTimeout(() => {
      void runInferenceVerification({
        ...latestDeployment,
        autoVerificationRequested: true,
        attempt: {
          ...latestDeployment.attempt,
          auto_verification_requested_at: new Date().toISOString(),
        },
      });
    }, AUTO_VERIFY_DELAY_MS);

    return () => {
      if (autoVerifyTimerRef.current) {
        window.clearTimeout(autoVerifyTimerRef.current);
        autoVerifyTimerRef.current = null;
      }
    };
  }, [latestDeployment, markAutoVerificationRequested, runInferenceVerification, verifyingAttemptID]);

  return {
    deploymentHistory,
    latestDeployment,
    deploymentSummaryByInstanceID,
    incidentRows,
    verifyingAttemptID,
    runInferenceVerification,
    handleRemediation,
  };
}
