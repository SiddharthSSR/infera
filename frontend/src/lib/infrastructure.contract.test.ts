/// <reference types="vitest/globals" />
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { fetchCosts, fetchInstances, fetchOfferings, fetchProviders } from './api';
import {
  parseCostSummary,
  parseInstancesResponse,
  parseOfferingsResponse,
  parseProvidersResponse,
} from './infrastructure';

const __dirname = dirname(fileURLToPath(import.meta.url));
const FIXTURE_DIR = resolve(__dirname, '../../../contracts/infrastructure_dashboard');

const mockFetch = vi.fn();
(globalThis as { fetch?: typeof fetch }).fetch = mockFetch;

function loadJSONFixture(name: string) {
  return JSON.parse(readFileSync(resolve(FIXTURE_DIR, name), 'utf8'));
}

describe('infrastructure dashboard contract fixtures', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('parses the shared instances fixture with workspace and engine fields', () => {
    const payload = loadJSONFixture('instances_list_response.json');

    const parsed = parseInstancesResponse(payload);

    expect(parsed.total).toBe(1);
    expect(parsed.instances[0]?.workspace_id).toBe('ws_alpha');
    expect(parsed.instances[0]?.engine).toBe('sglang');
    expect(parsed.instances[0]?.worker_id).toBe('worker-fixture-1');
  });

  it('parses the shared offerings fixture', () => {
    const payload = loadJSONFixture('offerings_list_response.json');

    const parsed = parseOfferingsResponse(payload);

    expect(parsed.total).toBe(1);
    expect(parsed.offerings[0]?.provider_gpu_type_id).toBe('h100-sxm');
    expect(parsed.offerings[0]?.spot_price).toBe(2.75);
  });

  it('parses the shared providers fixture', () => {
    const payload = loadJSONFixture('providers_list_response.json');

    const parsed = parseProvidersResponse(payload);

    expect(parsed.providers[0]?.provider).toBe('runpod');
    expect(parsed.providers[0]?.capabilities?.supports_custom_images).toBe(true);
    expect(parsed.providers[0]?.capabilities?.known_regions).toEqual(['us-east-1']);
  });

  it('parses the shared cost summary fixture', () => {
    const payload = loadJSONFixture('cost_summary_response.json');

    const parsed = parseCostSummary(payload);

    expect(parsed.current_hourly).toBe(0);
    expect(parsed.by_provider).toEqual({});
    expect(parsed.by_gpu).toEqual({});
  });

  it('fetchInstances/fetchOfferings/fetchProviders/fetchCosts use the shared fixture shapes', async () => {
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => loadJSONFixture('instances_list_response.json') })
      .mockResolvedValueOnce({ ok: true, json: async () => loadJSONFixture('offerings_list_response.json') })
      .mockResolvedValueOnce({ ok: true, json: async () => loadJSONFixture('providers_list_response.json') })
      .mockResolvedValueOnce({ ok: true, json: async () => loadJSONFixture('cost_summary_response.json') });

    await expect(fetchInstances()).resolves.toEqual(
      expect.arrayContaining([expect.objectContaining({ id: 'inst_fixture_1', engine: 'sglang' })]),
    );
    await expect(fetchOfferings()).resolves.toEqual(
      expect.arrayContaining([expect.objectContaining({ gpu_type: 'H100', provider_gpu_type_id: 'h100-sxm' })]),
    );
    await expect(fetchProviders()).resolves.toEqual(
      expect.arrayContaining([expect.objectContaining({ provider: 'runpod', active_instances: 1 })]),
    );
    await expect(fetchCosts()).resolves.toEqual(
      expect.objectContaining({ current_hourly: 0, by_provider: {}, by_gpu: {} }),
    );
  });
});
