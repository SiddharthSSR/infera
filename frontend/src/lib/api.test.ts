/// <reference types="vitest/globals" />
import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  fetchWorkers,
  fetchModels,
  fetchStats,
  fetchInstances,
  fetchOfferings,
  fetchCosts,
  provisionInstance,
  terminateInstance,
  startInstance,
  stopInstance,
} from './api'

// Mock fetch
const mockFetch = vi.fn()
;(globalThis as any).fetch = mockFetch

describe('API Functions', () => {
  beforeEach(() => {
    mockFetch.mockClear()
  })

  describe('fetchWorkers', () => {
    it('should fetch workers successfully', async () => {
      const mockWorkers = [
        { worker_id: 'worker-1', status: 'healthy' },
        { worker_id: 'worker-2', status: 'degraded' },
      ]

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ workers: mockWorkers }),
      })

      const workers = await fetchWorkers()

      expect(mockFetch).toHaveBeenCalledWith('/api/workers')
      expect(workers).toHaveLength(2)
      expect(workers[0].worker_id).toBe('worker-1')
    })

    it('should throw error on failure', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
      })

      await expect(fetchWorkers()).rejects.toThrow('Failed to fetch workers')
    })
  })

  describe('fetchModels', () => {
    it('should fetch models successfully', async () => {
      const mockModels = [
        { id: 'llama-3-8b', object: 'model' },
        { id: 'gpt-4', object: 'model' },
      ]

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ data: mockModels }),
      })

      const models = await fetchModels()

      expect(mockFetch).toHaveBeenCalledWith('/v1/models')
      expect(models).toHaveLength(2)
    })
  })

  describe('fetchStats', () => {
    it('should fetch stats successfully', async () => {
      const mockStats = {
        workers: { total: 5, healthy: 4 },
        models: { available: 3 },
        requests: { per_second: 100, queue_depth: 10 },
        latency: { avg_ms: 150 },
        memory: { used_bytes: 1000, total_bytes: 2000 },
        uptime_seconds: 3600,
      }

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => mockStats,
      })

      const stats = await fetchStats()

      expect(mockFetch).toHaveBeenCalledWith('/api/stats')
      expect(stats.workers.total).toBe(5)
    })
  })

  describe('fetchInstances', () => {
    it('should fetch instances successfully', async () => {
      const mockInstances = [
        { id: 'inst-1', name: 'test-1', status: 'running' },
        { id: 'inst-2', name: 'test-2', status: 'stopped' },
      ]

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ instances: mockInstances }),
      })

      const instances = await fetchInstances()

      expect(mockFetch).toHaveBeenCalledWith('/api/instances')
      expect(instances).toHaveLength(2)
    })
  })

  describe('fetchOfferings', () => {
    it('should fetch offerings successfully', async () => {
      const mockOfferings = [
        { provider: 'mock', gpu_type: 'RTX_4090', cost_per_hour: 0.50 },
      ]

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ offerings: mockOfferings }),
      })

      const offerings = await fetchOfferings()

      expect(mockFetch).toHaveBeenCalledWith('/api/offerings')
      expect(offerings[0].gpu_type).toBe('RTX_4090')
    })
  })

  describe('fetchCosts', () => {
    it('should fetch costs successfully', async () => {
      const mockCosts = {
        current_hourly: 5.50,
        today_total: 45.00,
        month_total: 350.00,
        projected_month: 500.00,
        by_provider: {},
        by_gpu: {},
      }

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => mockCosts,
      })

      const costs = await fetchCosts()

      expect(mockFetch).toHaveBeenCalledWith('/api/costs')
      expect(costs.current_hourly).toBe(5.50)
    })
  })

  describe('provisionInstance', () => {
    it('should provision instance successfully', async () => {
      const mockInstance = {
        id: 'new-inst',
        name: 'my-worker',
        status: 'provisioning',
      }

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ instance: mockInstance }),
      })

      const request = {
        name: 'my-worker',
        provider: 'mock' as const,
        gpu_type: 'RTX_4090' as const,
        gpu_count: 1,
      }

      const instance = await provisionInstance(request)

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/instances/provision',
        expect.objectContaining({
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(request),
        })
      )
      expect(instance.name).toBe('my-worker')
    })

    it('should throw error on provision failure', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        json: async () => ({ error: { message: 'Quota exceeded' } }),
      })

      await expect(
        provisionInstance({ gpu_type: 'H100' })
      ).rejects.toThrow('Quota exceeded')
    })
  })

  describe('terminateInstance', () => {
    it('should terminate instance successfully', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ success: true }),
      })

      await terminateInstance('inst-123')

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/instances/inst-123',
        expect.objectContaining({ method: 'DELETE' })
      )
    })

    it('should throw error on termination failure', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        json: async () => ({ error: { message: 'Instance not found' } }),
      })

      await expect(terminateInstance('invalid')).rejects.toThrow('Instance not found')
    })
  })

  describe('startInstance', () => {
    it('should start instance successfully', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ success: true }),
      })

      await startInstance('inst-123')

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/instances/inst-123/start',
        expect.objectContaining({ method: 'POST' })
      )
    })
  })

  describe('stopInstance', () => {
    it('should stop instance successfully', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ success: true }),
      })

      await stopInstance('inst-123')

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/instances/inst-123/stop',
        expect.objectContaining({ method: 'POST' })
      )
    })
  })
})