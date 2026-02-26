/// <reference types="vitest/globals" />
import { describe, it, expect } from 'vitest'
import type { 
  Worker, 
  Instance, 
  GPUOffering, 
  CostSummary,
  Stats,
  ProvisionRequest 
} from './index'

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
  })
})