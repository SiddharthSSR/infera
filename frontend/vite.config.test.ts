/// <reference types="vitest/globals" />
import { describe, expect, it } from 'vitest';
import config from './vite.config';

describe('vite proxy routing', () => {
  it('only proxies real API paths so frontend routes like /api-keys stay local', () => {
    expect(Object.keys(config.server?.proxy || {})).toEqual([
      '^/api(/|$)',
      '^/v1(/|$)',
      '^/health$',
    ]);
  });
});
