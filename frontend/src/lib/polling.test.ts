/// <reference types="vitest/globals" />
import { describe, expect, it } from 'vitest';

import {
  createVisibilityAwarePollingOptions,
  resolvePollingInterval,
} from './polling';

describe('polling helpers', () => {
  it('returns the polling interval when the document is visible', () => {
    expect(resolvePollingInterval(5000, 'visible')).toBe(5000);
  });

  it('stops polling when the document is hidden', () => {
    expect(resolvePollingInterval(5000, 'hidden')).toBe(false);
  });

  it('stops polling when the document is prerendering', () => {
    expect(resolvePollingInterval(5000, 'prerender')).toBe(false);
  });

  it('stops polling for non-positive intervals', () => {
    expect(resolvePollingInterval(0, 'visible')).toBe(false);
    expect(resolvePollingInterval(-1, 'visible')).toBe(false);
  });

  it('creates query options that avoid background polling', () => {
    const options = createVisibilityAwarePollingOptions(4000);

    expect(options.refetchIntervalInBackground).toBe(false);
    expect(options.refetchInterval()).toBe(4000);
  });
});
