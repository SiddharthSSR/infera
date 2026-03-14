/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Dashboard } from './Dashboard'

const mockNavigate = vi.fn()

type QueryResult<T> = { data: T; isLoading?: boolean }

const mocks = vi.hoisted(() => ({
  workers: { data: [], isLoading: false } as QueryResult<any[]>,
  stats: { data: undefined, isLoading: false } as QueryResult<any>,
  instances: { data: [], isLoading: false } as QueryResult<any[]>,
  costs: { data: undefined, isLoading: false } as QueryResult<any>,
  models: { data: [], isLoading: false } as QueryResult<any[]>,
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
}))

describe('Dashboard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.workers = { data: [], isLoading: false }
    mocks.stats = { data: undefined, isLoading: false }
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
  })

  it('renders loading skeleton when both workers and stats are loading', () => {
    mocks.workers = { data: [], isLoading: true }
    mocks.stats = { data: undefined, isLoading: true }

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
    }
    mocks.instances = {
      data: [
        { id: 'i1', status: 'running', cost_per_hour: 1.2 },
        { id: 'i2', status: 'stopped', cost_per_hour: 0.5 },
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

  it('navigates to provision flow from deploy button', () => {
    render(<Dashboard />)
    screen.getByRole('button', { name: 'DEPLOY NEW MODEL' }).click()
    expect(mockNavigate).toHaveBeenCalledWith('/models')
  })
})
