/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type {
  ApiKeyRecord,
  WorkspaceQuotaRecord,
  WorkspaceInvitationRecord,
} from '../types';
import type { AuditUsageRow } from '../lib/apiCore';
import type { DeploymentAttemptRecord } from '../lib/deploymentHistory';
import type { Instance, Model, ProviderStatus, Stats, Worker } from '../types';
import { useDashboardViewState } from './useDashboardViewState';

const stats: Stats = {
  workers: { total: 1, healthy: 1 },
  models: { available: 1 },
  requests: { per_second: 12, queue_depth: 2 },
  latency: { avg_ms: 140 },
  gpu: { avg_utilization: 61 },
  memory: { used_bytes: 6 * 1024 ** 3, total_bytes: 12 * 1024 ** 3 },
  uptime_seconds: 7200,
};

const worker: Worker = {
  worker_id: 'worker-1',
  address: 'http://worker-1',
  status: 'healthy',
  models: ['org/model-a'],
  gpu_utilization: 61,
  memory_used: 6,
  memory_total: 12,
  queue_depth: 0,
  requests_per_sec: 12,
  avg_latency_ms: 120,
  p50_latency_ms: 100,
  p99_latency_ms: 180,
  error_rate: 0,
  last_heartbeat: '2100-01-01T00:00:00Z',
};

const instance: Instance = {
  id: 'inst-1',
  provider_id: 'prov-1',
  provider: 'runpod',
  name: 'node-1',
  status: 'running',
  gpu_type: 'A100_40GB',
  gpu_count: 1,
  vcpu: 16,
  memory_gb: 64,
  storage_gb: 200,
  worker_id: 'worker-1',
  models: ['org/model-a'],
  cost_per_hour: 2.5,
  spot_instance: false,
  created_at: '2026-04-01T00:00:00Z',
};

const model: Model = {
  id: 'org/model-a',
  object: 'model',
  created: 0,
  owned_by: 'infera',
  loaded: true,
};

const provider: ProviderStatus = {
  provider: 'runpod',
  connected: true,
  active_instances: 1,
};

const verifiedAttempt: DeploymentAttemptRecord = {
  id: 'attempt-verified',
  workspace_id: 'ws_test',
  created_at: '2026-04-01T00:00:00Z',
  updated_at: '2026-04-01T00:05:00Z',
  outcome: 'provisioned',
  request: { gpu_type: 'A100_40GB', models: ['org/model-a'] },
  selected_model_name: 'Model A',
  instance_id: 'inst-1',
  inference_verification: {
    status: 'passed',
    verified_at: '2026-04-01T00:05:00Z',
    latency_ms: 180,
    model: 'org/model-a',
    response_preview: 'ready',
  },
};

describe('useDashboardViewState', () => {
  it('derives a verified workspace with live operations', () => {
    const serviceAccount: ApiKeyRecord = {
      id: 'key-1',
      workspace_id: 'ws_test',
      workspace_slug: 'test',
      workspace_name: 'Test Workspace',
      key_prefix: 'sk_test',
      name: 'Automation',
      role: 'admin',
      principal_type: 'service_account',
      created_at: '2026-04-01T00:00:00Z',
      status: 'active',
    };

    const { result } = renderHook(() => useDashboardViewState({
      isLoading: false,
      errorWorkers: false,
      workers: [worker],
      errorStats: false,
      stats,
      instances: [instance],
      costs: { current_hourly: 0.2, today_total: 2, by_provider: { runpod: 2 } },
      models: [model],
      providers: [provider],
      deploymentAttempts: [verifiedAttempt],
      quota: null,
      usageRows: [],
      workspaceInvites: [],
      workspaceServiceAccounts: [serviceAccount],
    }));

    expect(result.current.gatewayDown).toBe(false);
    expect(result.current.servingVerifiedCount).toBe(1);
    expect(result.current.workspaceMaturity.state).toBe('serving_verified');
    expect(result.current.liveWorkspaceOperations.show).toBe(true);
    expect(result.current.requestMetricSamples).toEqual([12]);
    expect(result.current.nodeOverviewRows[0]).toEqual({
      label: 'Active Instances',
      value: '1',
      secondary: '1 total',
    });
  });

  it('surfaces billing attention when quota is exceeded', () => {
    const quota: WorkspaceQuotaRecord = {
      workspace_id: 'ws_test',
      monthly_request_limit: 100,
      monthly_token_limit: 1000,
      enforce_hard_limits: true,
      updated_at: '2026-04-01T00:00:00Z',
    };
    const usageRows: AuditUsageRow[] = [
      {
        bucket_start: '2026-04-01T00:00:00Z',
        bucket_end: '2026-04-02T00:00:00Z',
        requests: 150,
        tokens: 1200,
      },
    ];

    const { result } = renderHook(() => useDashboardViewState({
      isLoading: false,
      errorWorkers: false,
      workers: [],
      errorStats: false,
      stats,
      instances: [],
      costs: { current_hourly: 6, today_total: 20, by_provider: { runpod: 19 } },
      models: [],
      providers: [],
      deploymentAttempts: [],
      quota,
      usageRows,
      workspaceInvites: [],
      workspaceServiceAccounts: [],
    }));

    expect(result.current.billingAttention.some((item) => item.id === 'quota-exceeded')).toBe(true);
    expect(result.current.attentionQueue.some((item) => item.id === 'provider-cost-concentration')).toBe(true);
    expect(result.current.quickConfigFields.find((field) => field.key === 'monthly_request_limit')?.value).toBe('100');
  });

  it('keeps a new workspace on the setup checklist path', () => {
    const invites: WorkspaceInvitationRecord[] = [];
    const serviceAccounts: ApiKeyRecord[] = [];

    const { result } = renderHook(() => useDashboardViewState({
      isLoading: false,
      errorWorkers: false,
      workers: [],
      errorStats: false,
      stats: undefined,
      instances: [],
      costs: undefined,
      models: [],
      providers: [],
      deploymentAttempts: [],
      quota: null,
      usageRows: [],
      workspaceInvites: invites,
      workspaceServiceAccounts: serviceAccounts,
    }));

    expect(result.current.isNewWorkspace).toBe(true);
    expect(result.current.workspaceMaturity.state).toBe('new');
    expect(result.current.firstWorkspaceChecklist[0].done).toBe(false);
    expect(result.current.liveWorkspaceOperations.show).toBe(false);
    expect(result.current.dashboardGuideCopy).toContain('attention queue');
  });
});
