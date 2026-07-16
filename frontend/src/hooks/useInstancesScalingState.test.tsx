/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { useInstancesScalingState } from './useInstancesScalingState';

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
  },
}));

import { toast } from 'sonner';

const mockToastSuccess = vi.mocked(toast.success);

describe('useInstancesScalingState', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('starts with clean defaults', () => {
    const { result } = renderHook(() => useInstancesScalingState());

    expect(result.current.scaling.minNodes).toBe(1);
    expect(result.current.scaling.maxNodes).toBe(5);
    expect(result.current.scaling.trigger).toBe(80);
    expect(result.current.scaling.dirty).toBe(false);
    expect(result.current.scaling.hasErrors).toBe(false);
    expect(result.current.scaling.errors).toEqual({
      minNodes: '',
      maxNodes: '',
      trigger: '',
    });
  });

  it('surfaces validation errors when the scaling bounds are invalid', () => {
    const { result } = renderHook(() => useInstancesScalingState());

    act(() => {
      result.current.scaling.onMinNodesChange(6);
      result.current.scaling.onMaxNodesChange(4);
      result.current.scaling.onTriggerChange(120);
    });

    expect(result.current.scaling.dirty).toBe(true);
    expect(result.current.scaling.hasErrors).toBe(true);
    expect(result.current.scaling.errors).toEqual({
      minNodes: 'Min must be less than max nodes',
      maxNodes: 'Max must be greater than min nodes',
      trigger: 'Must be between 0 and 100',
    });

    act(() => {
      result.current.scaling.onApply();
    });

    expect(mockToastSuccess).not.toHaveBeenCalled();
  });

  it('persists valid scaling changes and clears dirty state', () => {
    const { result } = renderHook(() => useInstancesScalingState());

    act(() => {
      result.current.scaling.onMinNodesChange(2);
      result.current.scaling.onMaxNodesChange(8);
      result.current.scaling.onTriggerChange(70);
    });

    expect(result.current.scaling.dirty).toBe(true);
    expect(result.current.scaling.hasErrors).toBe(false);

    act(() => {
      result.current.scaling.onApply();
    });

    expect(mockToastSuccess).toHaveBeenCalledWith('Scaling configuration updated');
    expect(result.current.scaling.dirty).toBe(false);
    expect(result.current.scaling.minNodes).toBe(2);
    expect(result.current.scaling.maxNodes).toBe(8);
    expect(result.current.scaling.trigger).toBe(70);
  });
});
