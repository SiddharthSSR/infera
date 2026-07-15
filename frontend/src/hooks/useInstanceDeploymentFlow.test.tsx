/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { act, renderHook } from '@testing-library/react';
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';

import type { DeploymentAttemptRecord, DeploymentAttemptSummary, Instance, Worker } from '../types';
import type { DeploymentRemediation } from '../lib/deploymentHistory';
import { useInstanceDeploymentFlow } from './useInstanceDeploymentFlow';

vi.mock('../lib/chatClient', () => ({
  sendChatCompletion: vi.fn(),
}));

vi.mock('../lib/deploymentHistory', () => ({
  summarizeDeploymentAttempt: vi.fn(),
}));

vi.mock('../lib/instanceIncidents', () => ({
  deriveNodeIncident: vi.fn(),
}));

vi.mock('./useDeploymentApi', () => ({
  useMarkDeploymentAutoVerificationRequested: vi.fn(),
  useUpdateDeploymentVerification: vi.fn(),
}));

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { sendChatCompletion } from '../lib/chatClient';
import { summarizeDeploymentAttempt } from '../lib/deploymentHistory';
import { deriveNodeIncident } from '../lib/instanceIncidents';
import { toast } from 'sonner';
import { useMarkDeploymentAutoVerificationRequested, useUpdateDeploymentVerification } from './useDeploymentApi';

const instance: Instance = {
  id: 'inst_1',
  provider_id: 'prov_1',
  provider: 'runpod',
  name: 'worker-1',
  status: 'running',
  gpu_type: 'RTX_4090',
  gpu_count: 1,
  vcpu: 16,
  memory_gb: 24,
  storage_gb: 200,
  cost_per_hour: 1.2,
  created_at: '2026-04-01T00:00:00Z',
  models: ['Qwen/Qwen3-4B-Instruct'],
};

const worker: Worker = {
  worker_id: 'worker-1',
  address: 'http://worker-1',
  status: 'healthy',
  models: ['Qwen/Qwen3-4B-Instruct'],
  gpu_utilization: 10,
  memory_used: 100,
  memory_total: 1000,
  queue_depth: 0,
  requests_per_sec: 0,
  avg_latency_ms: 12,
  p50_latency_ms: 10,
  p99_latency_ms: 20,
  error_rate: 0,
  last_heartbeat: '2026-04-01T00:00:00Z',
};

const attempt: DeploymentAttemptRecord = {
  id: 'attempt_1',
  workspace_id: 'ws_1',
  created_at: '2026-04-01T00:00:00Z',
  updated_at: '2026-04-01T00:00:00Z',
  outcome: 'provisioned',
  request: {
    gpu_type: 'RTX_4090',
    provider: 'runpod',
    models: ['Qwen/Qwen3-4B-Instruct'],
  },
  instance_id: 'inst_1',
  instance_name: 'worker-1',
};

const servingSummary: DeploymentAttemptSummary = {
  attempt,
  readiness: {
    label: 'SERVING VERIFIED',
    detail: 'Model is ready to serve.',
    tone: '',
    serving: true,
    verified: true,
  },
  instance,
  retryable: false,
  inferenceVerified: false,
  autoVerificationRequested: false,
};

const pendingSummary: DeploymentAttemptSummary = {
  ...servingSummary,
  readiness: {
    label: 'MODEL LOADING',
    detail: 'Still loading.',
    tone: 'warning',
    serving: false,
    verified: false,
  },
};

describe('useInstanceDeploymentFlow', () => {
  const summaryByAttemptID = new Map<string, DeploymentAttemptSummary>();
  const markAutoVerificationRequestedMutateAsync = vi.fn();
  const updateDeploymentVerificationMutateAsync = vi.fn();
  const onOpenWorkspace = vi.fn();
  const onRetry = vi.fn();
  const onFocusInstance = vi.fn();

  const mockSendChatCompletion = vi.mocked(sendChatCompletion);
  const mockSummarizeDeploymentAttempt = vi.mocked(summarizeDeploymentAttempt);
  const mockDeriveNodeIncident = vi.mocked(deriveNodeIncident);
  const mockUseMarkDeploymentAutoVerificationRequested = vi.mocked(useMarkDeploymentAutoVerificationRequested);
  const mockUseUpdateDeploymentVerification = vi.mocked(useUpdateDeploymentVerification);
  const mockToastSuccess = vi.mocked(toast.success);
  const mockToastError = vi.mocked(toast.error);

  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    summaryByAttemptID.clear();
    markAutoVerificationRequestedMutateAsync.mockResolvedValue(undefined);
    updateDeploymentVerificationMutateAsync.mockResolvedValue(undefined);
    mockUseMarkDeploymentAutoVerificationRequested.mockReturnValue({
      mutateAsync: markAutoVerificationRequestedMutateAsync,
    } as ReturnType<typeof useMarkDeploymentAutoVerificationRequested>);
    mockUseUpdateDeploymentVerification.mockReturnValue({
      mutateAsync: updateDeploymentVerificationMutateAsync,
    } as ReturnType<typeof useUpdateDeploymentVerification>);
    mockSummarizeDeploymentAttempt.mockImplementation((targetAttempt) => summaryByAttemptID.get(targetAttempt.id) ?? pendingSummary);
    mockDeriveNodeIncident.mockImplementation((targetInstance) => (
      targetInstance.id === 'inst_1'
        ? { title: 'VERIFICATION STALE', detail: 'Needs a probe.', tone: 'warning' }
        : null
    ));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('auto-verifies the latest serving deployment after the delay', async () => {
    summaryByAttemptID.set(attempt.id, servingSummary);
    mockSendChatCompletion.mockResolvedValue({
      id: 'chatcmpl_1',
      object: 'chat.completion',
      created: 1711929600,
      model: 'Qwen/Qwen3-4B-Instruct',
      choices: [
        {
          index: 0,
          message: { role: 'assistant', content: 'ready' },
          finish_reason: 'stop',
        },
      ],
    });

    const { result } = renderHook(() => useInstanceDeploymentFlow({
      workspaceID: 'ws_1',
      deploymentAttempts: [attempt],
      instances: [instance],
      workers: [worker],
      filteredInstances: [instance],
      onOpenWorkspace,
      onRetry,
      onFocusInstance,
    }));

    expect(markAutoVerificationRequestedMutateAsync).toHaveBeenCalledWith({
      attemptId: 'attempt_1',
      requestedAt: expect.any(String),
    });

    expect(result.current.latestDeployment).toEqual(servingSummary);
    expect(result.current.deploymentSummaryByInstanceID.get('inst_1')).toEqual(servingSummary);
    expect(result.current.incidentRows).toEqual([
      {
        instance,
        summary: servingSummary,
        incident: { title: 'VERIFICATION STALE', detail: 'Needs a probe.', tone: 'warning' },
      },
    ]);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });

    expect(mockSendChatCompletion).toHaveBeenCalledWith(expect.objectContaining({
      model: 'Qwen/Qwen3-4B-Instruct',
      temperature: 0,
      max_tokens: 16,
    }));

    expect(updateDeploymentVerificationMutateAsync).toHaveBeenCalledWith({
      attemptId: 'attempt_1',
      verification: expect.objectContaining({
        status: 'passed',
        model: 'Qwen/Qwen3-4B-Instruct',
        response_preview: 'ready',
        verified_at: expect.any(String),
        latency_ms: expect.any(Number),
      }),
    });
    expect(mockToastSuccess).toHaveBeenCalled();
  });

  it('routes remediation actions to the expected callbacks', () => {
    summaryByAttemptID.set(attempt.id, pendingSummary);

    const { result } = renderHook(() => useInstanceDeploymentFlow({
      workspaceID: 'ws_1',
      deploymentAttempts: [attempt],
      instances: [instance],
      workers: [worker],
      filteredInstances: [instance],
      onOpenWorkspace,
      onRetry,
      onFocusInstance,
    }));

    const remediations: DeploymentRemediation[] = [
      { label: 'Open workspace', detail: 'Check provider setup.', action: 'open_workspace' },
      { label: 'Retry', detail: 'Retry the config.', action: 'retry_config' },
      { label: 'Focus', detail: 'Inspect the node.', action: 'focus_instance' },
    ];

    act(() => {
      result.current.handleRemediation(pendingSummary, remediations[0]);
      result.current.handleRemediation(pendingSummary, remediations[1]);
      result.current.handleRemediation(pendingSummary, remediations[2]);
      result.current.handleRemediation(pendingSummary, null);
    });

    expect(onOpenWorkspace).toHaveBeenCalledTimes(1);
    expect(onRetry).toHaveBeenCalledWith(attempt);
    expect(onFocusInstance).toHaveBeenCalledWith('inst_1');
  });

  it('records failed verification attempts when chat inference fails', async () => {
    summaryByAttemptID.set(attempt.id, pendingSummary);
    mockSendChatCompletion.mockRejectedValue(new Error('Gateway unavailable'));

    const { result } = renderHook(() => useInstanceDeploymentFlow({
      workspaceID: 'ws_1',
      deploymentAttempts: [attempt],
      instances: [instance],
      workers: [worker],
      filteredInstances: [instance],
      onOpenWorkspace,
      onRetry,
      onFocusInstance,
    }));

    await act(async () => {
      await result.current.runInferenceVerification(pendingSummary);
    });

    expect(updateDeploymentVerificationMutateAsync).toHaveBeenCalledWith({
      attemptId: 'attempt_1',
      verification: expect.objectContaining({
        status: 'failed',
        model: 'Qwen/Qwen3-4B-Instruct',
        error: 'Gateway unavailable',
        verified_at: expect.any(String),
      }),
    });
    expect(mockToastError).toHaveBeenCalledWith('Gateway unavailable');
    expect(result.current.verifyingAttemptID).toBeNull();
  });
});
