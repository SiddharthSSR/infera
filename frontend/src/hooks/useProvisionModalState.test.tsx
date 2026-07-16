/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { NavigateFunction, SetURLSearchParams } from 'react-router-dom';

import type { DeploymentAttemptRecord } from '../types';
import type { ProvisionDraft } from '../lib/instanceProvisioning';
import { useProvisionModalState } from './useProvisionModalState';

const retryAttempt: DeploymentAttemptRecord = {
  id: 'attempt_1',
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

describe('useProvisionModalState', () => {
  let navigate: ReturnType<typeof vi.fn>;
  let setSearchParams: SetURLSearchParams;
  let onProvisionedSuccess: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    navigate = vi.fn();
    setSearchParams = vi.fn() as unknown as SetURLSearchParams;
    onProvisionedSuccess = vi.fn();
  });

  it('opens from provision search params and returns to the source route on close', async () => {
    const searchParams = new URLSearchParams('provision=true&model=Qwen%2FQwen3-4B-Instruct&from=models');

    const { result } = renderHook(() => useProvisionModalState({
      searchParams,
      setSearchParams,
      navigate: navigate as unknown as NavigateFunction,
      onProvisionedSuccess,
    }));

    await waitFor(() => {
      expect(result.current.showProvisionModal).toBe(true);
    });

    expect(result.current.preselectedModel).toBe('Qwen/Qwen3-4B-Instruct');
    expect(result.current.provisionDraft).toBeNull();
    expect(setSearchParams).toHaveBeenCalledWith({}, { replace: true });

    act(() => {
      result.current.closeProvisionModal();
    });

    expect(navigate).toHaveBeenCalledWith('/models');
    expect(result.current.showProvisionModal).toBe(false);
    expect(result.current.preselectedModel).toBeNull();
  });

  it('uses retry attempts as the modal draft and can redirect straight to workspace', () => {
    const { result } = renderHook(() => useProvisionModalState({
      searchParams: new URLSearchParams(),
      setSearchParams,
      navigate: navigate as unknown as NavigateFunction,
      onProvisionedSuccess,
    }));

    act(() => {
      result.current.openRetryModal(retryAttempt);
    });

    expect(result.current.showProvisionModal).toBe(true);
    expect(result.current.preselectedModel).toBe('Qwen/Qwen3-4B-Instruct');
    expect(result.current.provisionDraft).toEqual(retryAttempt.request);

    const failedDraft: ProvisionDraft = {
      gpu_type: 'A100_80GB',
      provider: 'mock',
      models: ['meta-llama/Llama-3.1-8B-Instruct'],
    };

    act(() => {
      result.current.handleProvisionFailed(failedDraft);
    });

    expect(result.current.provisionDraft).toEqual(failedDraft);

    act(() => {
      result.current.openWorkspaceFromModal();
    });

    expect(result.current.showProvisionModal).toBe(false);
    expect(navigate).toHaveBeenCalledWith('/workspace');
  });

  it('clears the return route after a successful provision', async () => {
    const searchParams = new URLSearchParams('provision=true&from=models');

    const { result } = renderHook(() => useProvisionModalState({
      searchParams,
      setSearchParams,
      navigate: navigate as unknown as NavigateFunction,
      onProvisionedSuccess,
    }));

    await waitFor(() => {
      expect(result.current.showProvisionModal).toBe(true);
    });

    act(() => {
      result.current.handleProvisioned();
    });

    expect(onProvisionedSuccess).toHaveBeenCalled();

    act(() => {
      result.current.closeProvisionModal();
    });

    expect(navigate).not.toHaveBeenCalledWith('/models');
  });
});
