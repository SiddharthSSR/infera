/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { usePlaygroundLayoutState } from './usePlaygroundLayoutState';

const originalWidth = window.innerWidth;

function setViewportWidth(width: number, dispatch = true) {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    writable: true,
    value: width,
  });
  if (dispatch) {
    window.dispatchEvent(new Event('resize'));
  }
}

describe('usePlaygroundLayoutState', () => {
  afterEach(() => {
    setViewportWidth(originalWidth, false);
  });

  it('derives the desktop rail layout when there is history', () => {
    setViewportWidth(1400, false);

    const { result } = renderHook(() => usePlaygroundLayoutState(3));

    expect(result.current.isMobile).toBe(false);
    expect(result.current.isTablet).toBe(false);
    expect(result.current.isCompactDesktop).toBe(false);
    expect(result.current.isExtraSmall).toBe(false);
    expect(result.current.promptHeight).toBe(132);
    expect(result.current.showDesktopSettingsRail).toBe(true);
    expect(result.current.showDesktopHistoryRail).toBe(true);
    expect(result.current.playgroundGridTemplateColumns).toBe('252px minmax(0, 1fr) 236px');
  });

  it('switches to the single-column mobile layout on resize', () => {
    setViewportWidth(1400, false);

    const { result } = renderHook(() => usePlaygroundLayoutState(0));

    act(() => {
      setViewportWidth(420);
    });

    expect(result.current.isExtraSmall).toBe(true);
    expect(result.current.isMobile).toBe(true);
    expect(result.current.isTablet).toBe(false);
    expect(result.current.isCompactDesktop).toBe(true);
    expect(result.current.promptHeight).toBe(100);
    expect(result.current.showDesktopSettingsRail).toBe(false);
    expect(result.current.showDesktopHistoryRail).toBe(false);
    expect(result.current.playgroundGridTemplateColumns).toBe('1fr');
  });

  it('exits focus mode when escape is pressed', () => {
    setViewportWidth(1200, false);

    const { result } = renderHook(() => usePlaygroundLayoutState(2));

    act(() => {
      result.current.setFocusMode(true);
    });

    expect(result.current.focusMode).toBe(true);
    expect(result.current.showDesktopSettingsRail).toBe(false);

    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });

    expect(result.current.focusMode).toBe(false);
    expect(result.current.showDesktopSettingsRail).toBe(true);
  });
});
