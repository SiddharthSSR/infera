/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Dashboard } from './Dashboard'

const mockNavigate = vi.fn()

type QueryResult<T> = { data: T; isLoading?: boolean; isError?: boolean }

const mocks = vi.hoisted(() => ({
  workers: { data: [], isLoading: false, isError: false } as QueryResult<any[]>,
  stats: { data: undefined, isLoading: false, isError: false } as QueryResult<any>,
  instances: { data: [], isLoading: false } as QueryResult<any[]>,
  costs: { data: undefined, isLoading: false } as QueryResult<any>,
  models: { data: [], isLoading: false } as QueryResult<any[]>,
  providers: { data: [], isLoading: false } as QueryResult<any[]>,
  fetchApiKeys: vi.fn(),
  fetchWorkspaceQuota: vi.fn(),
  fetchAuditUsage: vi.fn(),
  fetchWorkspaceInvites: vi.fn(),
}))

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

vi.mock('../hooks/useApi', () => ({
  useWorkers: () => mocks.workers,
  useStats: () => mocks.stats,
  useInstances: () => mocks.instances,
  useCosts: () => mocks.costs,
  useModels: () => mocks.models,
  useProviders: () => mocks.providers,
}))

vi.mock('../lib/auth-context', () => ({
  useAuthSession: () => ({ session: { workspace: { id: 'ws_test' }, key: { role: 'admin' } } }),
}))

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api')
  return {
    ...actual,
    fetchApiKeys: mocks.fetchApiKeys,
    fetchWorkspaceQuota: mocks.fetchWorkspaceQuota,
    fetchAuditUsage: mocks.fetchAuditUsage,
    fetchWorkspaceInvites: mocks.fetchWorkspaceInvites,
  }
})

describe('Dashboard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
    mocks.workers = { data: [], isLoading: false, isError: false }
    mocks.stats = { data: undefined, isLoading: false, isError: false }
    mocks.instances = { data: [], isLoading: false }
    mocks.costs = {
      data: {
        current_hourly: 0,
        today_total: 0,
        month_total: 0,
        projected_month: 0,
        by_provider: {},
        by_gpu: {},
      },
      isLoading: false,
    }
    mocks.models = { data: [], isLoading: false }
    mocks.providers = { data: [], isLoading: false }
    mocks.fetchApiKeys.mockImplementation(() => new Promise(() => {}))
    mocks.fetchWorkspaceQuota.mockImplementation(() => new Promise(() => {}))
    mocks.fetchAuditUsage.mockImplementation(() => new Promise(() => {}))
    mocks.fetchWorkspaceInvites.mockImplementation(() => new Promise(() => {}))
  })

  it('renders loading skeleton when both workers and stats are loading', () => {
    mocks.workers = { data: [], isLoading: true, isError: false }
    mocks.stats = { data: undefined, isLoading: true, isError: false }

    const { container } = render(<Dashboard />)
    expect(container.querySelectorAll('[style*="skeleton-pulse"]').length).toBeGreaterThan(0)
    expect(screen.queryByText('TOTAL REQUESTS')).not.toBeInTheDocument()
  })

  it('renders core pipeline metrics from API data', () => {
    mocks.workers = {
      data: [
        { worker_id: 'w1', status: 'healthy', gpu_utilization: 70, memory_used: 4, memory_total: 8, models: ['meta/model-a'] },
        { worker_id: 'w2', status: 'healthy', gpu_utilization: 60, memory_used: 3, memory_total: 8, models: ['meta/model-b'] },
        { worker_id: 'w3', status: 'degraded', gpu_utilization: 20, memory_used: 1, memory_total: 8, models: [] },
      ],
      isLoading: false,
      isError: false,
    }
    mocks.stats = {
      data: {
        workers: { total: 3, healthy: 2 },
        models: { available: 4 },
        requests: { per_second: 100, queue_depth: 7 },
        latency: { avg_ms: 123.6 },
        gpu: { avg_utilization: 65 },
        memory: { used_bytes: 10 * 1024 ** 3, total_bytes: 20 * 1024 ** 3 },
        uptime_seconds: 7265,
      },
      isLoading: false,
      isError: false,
    }
    mocks.instances = {
      data: [
        { id: 'i1', status: 'running', cost_per_hour: 1.2, models: ['org/model-a'] },
        { id: 'i2', status: 'stopped', cost_per_hour: 0.5, models: [] },
      ],
      isLoading: false,
    }
    mocks.costs = {
      data: {
        current_hourly: 3.21,
        today_total: 12.34,
        month_total: 44.56,
        projected_month: 77.89,
        by_provider: {},
        by_gpu: {},
      },
      isLoading: false,
    }
    mocks.models = {
      data: [
        { id: 'org/model-a', loaded: true, family: 'llama', quantization: 'AWQ', max_context: 8192, tags: ['chat'] },
      ],
      isLoading: false,
    }

    render(<Dashboard />)

    expect(screen.getByText('8640.0K')).toBeInTheDocument()
    expect(screen.getByText('124ms')).toBeInTheDocument()
    expect(screen.getByText('100.0 r/s')).toBeInTheDocument()
    expect(screen.getByText('2 / 3')).toBeInTheDocument()
    expect(screen.getByText('$3.21')).toBeInTheDocument()
    expect(screen.getByText('$12.34 today')).toBeInTheDocument()
    expect(screen.getByText('7')).toBeInTheDocument()
  })

  it('renders dashboard serving summary from deployment history', async () => {
    window.localStorage.setItem('infera:deployment-attempts:ws_test', JSON.stringify([
      {
        id: 'attempt_verified',
        created_at: '2026-03-15T10:00:00.000Z',
        updated_at: '2026-03-15T10:05:00.000Z',
        outcome: 'provisioned',
        request: { gpu_type: 'A100_40GB', models: ['org/model-a'] },
        selected_model_name: 'Model A',
        instance_id: 'i1',
        inference_verification: {
          status: 'passed',
          verified_at: '2026-03-15T10:05:00.000Z',
          latency_ms: 182,
          model: 'org/model-a',
          response_preview: 'ready',
        },
      },
      {
        id: 'attempt_failed',
        created_at: '2026-03-15T09:00:00.000Z',
        updated_at: '2026-03-15T09:01:00.000Z',
        outcome: 'request_failed',
        request: { gpu_type: 'H100', models: ['org/model-b'] },
        failure_reason: 'provider auth failed',
      },
      {
        id: 'attempt_pending',
        created_at: '2026-03-15T08:00:00.000Z',
        updated_at: '2026-03-15T08:01:00.000Z',
        outcome: 'provisioned',
        request: { gpu_type: 'RTX_4090', models: ['org/model-c'] },
        instance_id: 'i2',
      },
    ]))

    mocks.providers = {
      data: [{ provider: 'runpod', connected: true }],
      isLoading: false,
    }
    mocks.workers = {
      data: [
        { worker_id: 'worker-1', status: 'healthy', last_heartbeat: new Date().toISOString(), gpu_utilization: 52, memory_used: 4, memory_total: 8, models: ['org/model-a'] },
      ],
      isLoading: false,
      isError: false,
    }
    mocks.stats = {
      data: {
        workers: { total: 1, healthy: 1 },
        models: { available: 3 },
        requests: { per_second: 0, queue_depth: 0 },
        latency: { avg_ms: 50 },
        gpu: { avg_utilization: 52 },
        memory: { used_bytes: 4 * 1024 ** 3, total_bytes: 8 * 1024 ** 3 },
        uptime_seconds: 300,
      },
      isLoading: false,
      isError: false,
    }
    mocks.instances = {
      data: [
        {
          id: 'i1',
          provider_id: 'p1',
          provider: 'runpod',
          name: 'node-a',
          status: 'running',
          gpu_type: 'A100_40GB',
          gpu_count: 1,
          vcpu: 16,
          memory_gb: 64,
          storage_gb: 100,
          worker_id: 'worker-1',
          models: ['org/model-a'],
          cost_per_hour: 1.2,
          spot_instance: false,
          created_at: '2026-03-15T10:00:00.000Z',
        },
        {
          id: 'i2',
          provider_id: 'p2',
          provider: 'runpod',
          name: 'node-b',
          status: 'provisioning',
          gpu_type: 'RTX_4090',
          gpu_count: 1,
          vcpu: 8,
          memory_gb: 32,
          storage_gb: 100,
          models: ['org/model-c'],
          cost_per_hour: 0.4,
          spot_instance: false,
          created_at: '2026-03-15T08:00:00.000Z',
        },
      ],
      isLoading: false,
    }
    mocks.models = {
      data: [
        { id: 'org/model-a', loaded: true },
        { id: 'org/model-b', loaded: false },
        { id: 'org/model-c', loaded: false },
      ],
      isLoading: false,
    }

    render(<Dashboard />)

    expect(screen.getByText('WORKSPACE STATE')).toBeInTheDocument()
    expect(screen.getAllByText('ATTENTION REQUIRED').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Latest deployment request failed').length).toBeGreaterThan(0)
    expect(screen.getAllByText('SERVING VERIFIED').length).toBeGreaterThan(0)
    expect(screen.getByText('VERIFY PENDING')).toBeInTheDocument()
    expect(screen.getByText('DEGRADED DEPLOYMENTS')).toBeInTheDocument()
    expect(screen.getByText('PENDING DEPLOYMENTS')).toBeInTheDocument()
    expect(screen.getByText('ATTENTION QUEUE')).toBeInTheDocument()
    expect(screen.getAllByText('Latest deployment request failed').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Model A').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/provider auth failed/i).length).toBeGreaterThan(0)
  })

  it('renders first workspace checklist for incomplete setup', async () => {
    mocks.providers = {
      data: [],
      isLoading: false,
    }
    mocks.models = {
      data: [],
      isLoading: false,
    }
    mocks.instances = {
      data: [],
      isLoading: false,
    }
    mocks.fetchApiKeys.mockResolvedValue([])
    mocks.fetchWorkspaceInvites.mockResolvedValue([])

    render(<Dashboard />)

    expect(await screen.findByText('NEW WORKSPACE')).toBeInTheDocument()
    expect(await screen.findByText('FIRST WORKSPACE CHECKLIST')).toBeInTheDocument()
    expect(screen.getByText('Add provider access')).toBeInTheDocument()
    expect(screen.getByText('Register or confirm a model')).toBeInTheDocument()
    expect(screen.getByText('Provision first node')).toBeInTheDocument()
    expect(screen.getByText('Verify first inference')).toBeInTheDocument()
    expect(screen.getByText('Add a teammate or automation identity')).toBeInTheDocument()
  })

  it('surfaces provider and worker issues in the attention queue', () => {
    mocks.providers = {
      data: [{ provider: 'runpod', connected: false }],
      isLoading: false,
    }
    mocks.instances = {
      data: [
        { id: 'i1', provider_id: 'p1', provider: 'runpod', name: 'node-a', status: 'running', gpu_type: 'A100_40GB', gpu_count: 1, vcpu: 16, memory_gb: 64, storage_gb: 100, models: ['org/model-a'], cost_per_hour: 1.2, spot_instance: false, created_at: '2026-03-15T10:00:00.000Z' },
      ],
      isLoading: false,
    }
    mocks.models = { data: [{ id: 'org/model-a', loaded: true }], isLoading: false }

    render(<Dashboard />)

    expect(screen.getAllByText('ATTENTION REQUIRED').length).toBeGreaterThan(0)
    expect(screen.getAllByText('No live provider connection').length).toBeGreaterThan(0)
    expect(screen.getByText('Workers are not connected')).toBeInTheDocument()
    expect(screen.getAllByText('OPEN WORKSPACE').length).toBeGreaterThan(0)
  })

  it('surfaces quota and spend alerts when billing pressure is high', async () => {
    mocks.costs = {
      data: {
        current_hourly: 2.5,
        today_total: 12,
        month_total: 40,
        projected_month: 120,
        by_provider: { runpod: 11, vastai: 1 },
        by_gpu: {},
      },
      isLoading: false,
    }
    mocks.fetchWorkspaceQuota.mockResolvedValue({
      workspace_id: 'ws_test',
      monthly_request_limit: 1000,
      monthly_token_limit: 2000,
      enforce_hard_limits: true,
      updated_at: '2026-03-15T00:00:00.000Z',
    })
    mocks.fetchAuditUsage.mockResolvedValue({
      bucket: 'day',
      start: '2026-03-01T00:00:00.000Z',
      end: '2026-03-15T00:00:00.000Z',
      rows: [
        { bucket_start: '2026-03-15T00:00:00.000Z', workspace_id: 'ws_test', key_id: 'key_1', requests: 920, tokens: 1500, successes: 900, errors: 20 },
      ],
    })

    render(<Dashboard />)

    expect(await screen.findByText('Workspace quota nearing limit')).toBeInTheDocument()
    expect(screen.getAllByText('Current cost burn is elevated').length).toBeGreaterThan(0)
    expect(screen.getByText('Spend is concentrated on one provider')).toBeInTheDocument()
  })

  it('renders dashboard trends and history from deployment and usage data', async () => {
    window.localStorage.setItem('infera:deployment-attempts:ws_test', JSON.stringify([
      {
        id: 'attempt_verified',
        created_at: '2026-03-14T10:00:00.000Z',
        updated_at: '2026-03-14T10:05:00.000Z',
        outcome: 'provisioned',
        request: { gpu_type: 'A100_40GB', models: ['org/model-a'] },
        selected_model_name: 'Model A',
        instance_id: 'i1',
        inference_verification: {
          status: 'passed',
          verified_at: '2026-03-14T10:05:00.000Z',
          latency_ms: 182,
          model: 'org/model-a',
          response_preview: 'ready',
        },
      },
      {
        id: 'attempt_pending',
        created_at: '2026-03-15T08:00:00.000Z',
        updated_at: '2026-03-15T08:01:00.000Z',
        outcome: 'provisioned',
        request: { gpu_type: 'RTX_4090', models: ['org/model-b'] },
        selected_model_name: 'Model B',
        instance_id: 'i2',
      },
    ]))

    mocks.providers = {
      data: [{ provider: 'runpod', connected: true }],
      isLoading: false,
    }
    mocks.workers = {
      data: [
        { worker_id: 'worker-1', status: 'healthy', last_heartbeat: new Date().toISOString(), gpu_utilization: 52, memory_used: 4, memory_total: 8, models: ['org/model-a'] },
      ],
      isLoading: false,
      isError: false,
    }
    mocks.instances = {
      data: [
        {
          id: 'i1',
          provider_id: 'p1',
          provider: 'runpod',
          name: 'node-a',
          status: 'running',
          gpu_type: 'A100_40GB',
          gpu_count: 1,
          vcpu: 16,
          memory_gb: 64,
          storage_gb: 100,
          worker_id: 'worker-1',
          models: ['org/model-a'],
          cost_per_hour: 1.2,
          spot_instance: false,
          created_at: '2026-03-14T10:00:00.000Z',
        },
        {
          id: 'i2',
          provider_id: 'p2',
          provider: 'runpod',
          name: 'node-b',
          status: 'provisioning',
          gpu_type: 'RTX_4090',
          gpu_count: 1,
          vcpu: 8,
          memory_gb: 32,
          storage_gb: 100,
          models: ['org/model-b'],
          cost_per_hour: 0.4,
          spot_instance: false,
          created_at: '2026-03-15T08:00:00.000Z',
        },
      ],
      isLoading: false,
    }
    mocks.models = {
      data: [
        { id: 'org/model-a', loaded: true },
        { id: 'org/model-b', loaded: false },
      ],
      isLoading: false,
    }
    mocks.fetchWorkspaceQuota.mockResolvedValue({
      workspace_id: 'ws_test',
      monthly_request_limit: 5000,
      monthly_token_limit: 10000,
      enforce_hard_limits: true,
      updated_at: '2026-03-15T00:00:00.000Z',
    })
    mocks.fetchAuditUsage.mockResolvedValue({
      bucket: 'day',
      start: '2026-03-01T00:00:00.000Z',
      end: '2026-03-15T00:00:00.000Z',
      rows: [
        { bucket_start: '2026-03-13T00:00:00.000Z', workspace_id: 'ws_test', key_id: 'key_1', requests: 200, tokens: 800, successes: 190, errors: 10 },
        { bucket_start: '2026-03-14T00:00:00.000Z', workspace_id: 'ws_test', key_id: 'key_1', requests: 320, tokens: 1100, successes: 315, errors: 5 },
        { bucket_start: '2026-03-15T00:00:00.000Z', workspace_id: 'ws_test', key_id: 'key_1', requests: 450, tokens: 1500, successes: 446, errors: 4 },
      ],
    })

    render(<Dashboard />)

    expect(screen.getByText('DEPLOYMENT HISTORY')).toBeInTheDocument()
    expect(screen.getByText('VERIFICATION HISTORY')).toBeInTheDocument()
    expect(screen.getByText('USAGE TRAJECTORY')).toBeInTheDocument()
    expect(screen.getAllByText('Model B').length).toBeGreaterThan(0)
    expect(await screen.findByText('450 req')).toBeInTheDocument()
    expect(screen.getByText('Latency 182ms')).toBeInTheDocument()
  })

  it('navigates to provision flow from deploy button', () => {
    render(<Dashboard />)
    screen.getByRole('button', { name: 'DEPLOY NEW MODEL' }).click()
    expect(mockNavigate).toHaveBeenCalledWith('/models')
  })
})
