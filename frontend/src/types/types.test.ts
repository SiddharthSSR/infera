import { describe, it, expect } from 'vitest'
import type { 
  Worker, 
  Instance, 
  GPUOffering, 
  CostSummary,
  Stats,
  ProvisionRequest 
} from '../types/index'

describe('Types', () => {
  describe('Worker', () => {
    it('should have required fields', () => {
      const worker: Worker = {
        worker_id: 'worker-123',
        address: 'localhost:8081',
        status: 'healthy',
        models: ['llama-3-8b'],
        gpu_utilization: 0.75,
        memory_used: 8000000000,
        memory_total: 16000000000,
        queue_depth: 5,
        requests_per_sec: 10.5,
        avg_latency_ms: 150,
        p50_latency_ms: 120,
        p99_latency_ms: 500,
        error_rate: 0.01,
        last_heartbeat: '2024-01-15T10:30:00Z',
      }

      expect(worker.worker_id).toBe('worker-123')
      expect(worker.status).toBe('healthy')
      expect(worker.models).toContain('llama-3-8b')
    })

    it('should support all status values', () => {
      const statuses: Worker['status'][] = [
        'healthy',
        'degraded',
        'unhealthy',
        'draining',
        'offline',
      ]

      statuses.forEach(status => {
        const worker: Worker = {
          worker_id: 'test',
          address: 'test',
          status,
          models: [],
          gpu_utilization: 0,
          memory_used: 0,
          memory_total: 0,
          queue_depth: 0,
          requests_per_sec: 0,
          avg_latency_ms: 0,
          p50_latency_ms: 0,
          p99_latency_ms: 0,
          error_rate: 0,
          last_heartbeat: '',
        }
        expect(worker.status).toBe(status)
      })
    })
  })

  describe('Instance', () => {
    it('should have required fields', () => {
      const instance: Instance = {
        id: 'inst-123',
        provider_id: 'mock-inst-123',
        provider: 'mock',
        name: 'test-instance',
        status: 'running',
        gpu_type: 'RTX_4090',
        gpu_count: 2,
        vcpu: 16,
        memory_gb: 64,
        storage_gb: 200,
        cost_per_hour: 0.80,
        spot_instance: false,
        created_at: '2024-01-15T10:00:00Z',
      }

      expect(instance.id).toBe('inst-123')
      expect(instance.provider).toBe('mock')
      expect(instance.gpu_count).toBe(2)
    })

    it('should support all status values', () => {
      const statuses: Instance['status'][] = [
        'pending',
        'provisioning',
        'running',
        'starting',
        'stopping',
        'stopped',
        'terminating',
        'terminated',
        'error',
      ]

      statuses.forEach(status => {
        const instance: Instance = {
          id: 'test',
          provider_id: 'test',
          provider: 'mock',
          name: 'test',
          status,
          gpu_type: 'RTX_4090',
          gpu_count: 1,
          vcpu: 8,
          memory_gb: 32,
          storage_gb: 100,
          cost_per_hour: 0.50,
          spot_instance: false,
          created_at: '',
        }
        expect(instance.status).toBe(status)
      })
    })

    it('should support optional fields', () => {
      const instance: Instance = {
        id: 'inst-456',
        provider_id: 'mock-inst-456',
        provider: 'runpod',
        name: 'full-instance',
        status: 'running',
        gpu_type: 'A100_80GB',
        gpu_count: 4,
        vcpu: 32,
        memory_gb: 256,
        storage_gb: 500,
        public_ip: '192.168.1.100',
        http_port: 8080,
        ssh_port: 22,
        worker_id: 'worker-789',
        models: ['llama-3-70b'],
        cost_per_hour: 8.00,
        spot_instance: true,
        created_at: '2024-01-15T10:00:00Z',
        started_at: '2024-01-15T10:05:00Z',
        error: undefined,
      }

      expect(instance.public_ip).toBe('192.168.1.100')
      expect(instance.worker_id).toBe('worker-789')
      expect(instance.spot_instance).toBe(true)
    })
  })

  describe('GPUOffering', () => {
    it('should have required fields', () => {
      const offering: GPUOffering = {
        provider: 'runpod',
        gpu_type: 'H100',
        gpu_count: 1,
        vcpu: 24,
        memory_gb: 256,
        storage_gb: 500,
        cost_per_hour: 3.50,
        region: 'us-east-1',
        available: 10,
      }

      expect(offering.provider).toBe('runpod')
      expect(offering.gpu_type).toBe('H100')
      expect(offering.cost_per_hour).toBe(3.50)
    })

    it('should support spot pricing', () => {
      const offering: GPUOffering = {
        provider: 'mock',
        gpu_type: 'RTX_4090',
        gpu_count: 1,
        vcpu: 8,
        memory_gb: 32,
        storage_gb: 100,
        cost_per_hour: 0.50,
        spot_price: 0.25,
        region: 'mock',
        available: 100,
      }

      expect(offering.spot_price).toBe(0.25)
      expect(offering.spot_price).toBeLessThan(offering.cost_per_hour)
    })
  })

  describe('CostSummary', () => {
    it('should have required fields', () => {
      const costs: CostSummary = {
        current_hourly: 5.50,
        today_total: 45.00,
        month_total: 350.00,
        projected_month: 500.00,
        by_provider: {
          runpod: 3.50,
          vastai: 2.00,
        },
        by_gpu: {
          RTX_4090: 1.50,
          A100_80GB: 4.00,
        },
      }

      expect(costs.current_hourly).toBe(5.50)
      expect(costs.by_provider.runpod).toBe(3.50)
    })

    it('should have consistent breakdown', () => {
      const costs: CostSummary = {
        current_hourly: 5.50,
        today_total: 45.00,
        month_total: 350.00,
        projected_month: 500.00,
        by_provider: {
          runpod: 3.50,
          vastai: 2.00,
        },
        by_gpu: {
          RTX_4090: 1.50,
          A100_80GB: 4.00,
        },
      }

      const providerTotal = Object.values(costs.by_provider).reduce((a, b) => a + b, 0)
      const gpuTotal = Object.values(costs.by_gpu).reduce((a, b) => a + b, 0)

      expect(providerTotal).toBe(costs.current_hourly)
      expect(gpuTotal).toBe(costs.current_hourly)
    })
  })

  describe('Stats', () => {
    it('should have required fields', () => {
      const stats: Stats = {
        workers: {
          total: 5,
          healthy: 4,
        },
        models: {
          available: 3,
        },
        requests: {
          per_second: 100,
          queue_depth: 10,
        },
        latency: {
          avg_ms: 150,
        },
        memory: {
          used_bytes: 32000000000,
          total_bytes: 80000000000,
        },
        uptime_seconds: 86400,
      }

      expect(stats.workers.total).toBe(5)
      expect(stats.workers.healthy).toBeLessThanOrEqual(stats.workers.total)
      expect(stats.requests.per_second).toBe(100)
    })
  })

  describe('ProvisionRequest', () => {
    it('should have required fields', () => {
      const request: ProvisionRequest = {
        gpu_type: 'RTX_4090',
      }

      expect(request.gpu_type).toBe('RTX_4090')
    })

    it('should support all optional fields', () => {
      const request: ProvisionRequest = {
        name: 'my-worker',
        provider: 'runpod',
        gpu_type: 'H100',
        gpu_count: 4,
        region: 'us-west-2',
        spot_instance: true,
        max_cost_hour: 15.00,
        models: ['llama-3-70b', 'mixtral-8x7b'],
      }

      expect(request.name).toBe('my-worker')
      expect(request.gpu_count).toBe(4)
      expect(request.spot_instance).toBe(true)
      expect(request.models).toHaveLength(2)
    })
  })
})
