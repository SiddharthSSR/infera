/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { useModelsViewState } from './useModelsViewState';

describe('useModelsViewState', () => {
  it('derives filtered catalog counts, recommendations, and provider signal', () => {
    const displayModels = [
      {
        id: 'Qwen/Qwen3-4B-Thinking-2507',
        loaded: false,
        vault_status: 'available',
        family: 'qwen',
        parameters: '4B',
        quantization: 'AWQ',
        max_context: 32768,
        vram_required: 8192,
        owned_by: 'Qwen',
        tags: ['reasoning', 'chat'],
      },
      {
        id: 'org/model-b',
        loaded: true,
        vault_status: 'available',
        family: 'llama',
        parameters: '8B',
        quantization: 'FP16',
        max_context: 8192,
        owned_by: 'org',
        tags: ['chat'],
      },
    ] as any;

    const { result } = renderHook(() => useModelsViewState({
      displayModels,
      offerings: [{ provider: 'runpod', gpu_type: 'RTX_4090', cost_per_hour: 0.4 }] as any,
      providers: [{ provider: 'runpod', connected: true }] as any,
      liveInstances: [{ id: 'instance-1', models: ['org/model-b'], status: 'running' }] as any,
      workers: [{ worker_id: 'worker-1', models: ['org/model-b'] }] as any,
      deploymentAttempts: [],
      deferredQuery: 'qwen',
      activeTagFilter: null,
    }));

    expect(result.current.allTags).toEqual(['chat', 'reasoning']);
    expect(result.current.filtered).toHaveLength(1);
    expect(result.current.filtered[0].id).toBe('Qwen/Qwen3-4B-Thinking-2507');
    expect(result.current.readyCount).toBe(1);
    expect(result.current.activeCount).toBe(0);
    expect(result.current.servingVerifiedCount).toBe(0);
    expect(result.current.recommendedModels.map((model) => model.id)).toEqual(['Qwen/Qwen3-4B-Thinking-2507']);
    expect(result.current.connectedProviderCount).toBe(1);
  });

  it('applies tag filters and preserves active loaded models in counts', () => {
    const displayModels = [
      {
        id: 'org/model-a',
        loaded: true,
        vault_status: 'available',
        family: 'llama',
        parameters: '8B',
        quantization: 'FP16',
        max_context: 8192,
        owned_by: 'org',
        tags: ['chat'],
      },
      {
        id: 'org/model-b',
        loaded: false,
        vault_status: 'available',
        family: 'mistral',
        parameters: '7B',
        quantization: 'AWQ',
        max_context: 8192,
        owned_by: 'org',
        tags: ['embedding'],
      },
    ] as any;

    const { result } = renderHook(() => useModelsViewState({
      displayModels,
      offerings: [{ provider: 'runpod', gpu_type: 'RTX_4090', cost_per_hour: 0.4 }] as any,
      providers: [{ provider: 'runpod', connected: true }] as any,
      liveInstances: [{ id: 'instance-1', models: ['org/model-a'], status: 'running' }] as any,
      workers: [{ worker_id: 'worker-1', models: ['org/model-a'] }] as any,
      deploymentAttempts: [],
      deferredQuery: '',
      activeTagFilter: 'chat',
    }));

    expect(result.current.filtered.map((model) => model.id)).toEqual(['org/model-a']);
    expect(result.current.activeCount).toBe(1);
    expect(result.current.readyCount).toBe(0);
  });
});
