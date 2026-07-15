/// <reference types="vitest/globals" />
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import {
  fetchWorkspaceProviderConfigs,
  fetchWorkspaceQuota,
  updateWorkspaceQuota,
  upsertWorkspaceProviderConfig,
} from './api';
import {
  parseWorkspaceProviderConfigResponse,
  parseWorkspaceProviderConfigsResponse,
  parseWorkspaceQuotaResponse,
} from './workspaceAdmin';

const __dirname = dirname(fileURLToPath(import.meta.url));
const FIXTURE_DIR = resolve(__dirname, '../../../contracts/workspace_admin');

const mockFetch = vi.fn();
(globalThis as { fetch?: typeof fetch }).fetch = mockFetch;

function loadJSONFixture(name: string) {
  return JSON.parse(readFileSync(resolve(FIXTURE_DIR, name), 'utf8'));
}

describe('workspace admin contract fixtures', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('parses the shared workspace quota fixture', () => {
    const payload = loadJSONFixture('workspace_quota_response.json');

    const parsed = parseWorkspaceQuotaResponse(payload);

    expect(parsed.quota.workspace_id).toBe('ws_alpha');
    expect(parsed.quota.monthly_request_limit).toBe(250);
    expect(parsed.quota.enforce_hard_limits).toBe(false);
  });

  it('parses the shared workspace provider config fixtures', () => {
    const listPayload = loadJSONFixture('workspace_provider_configs_list_response.json');
    const providerPayload = loadJSONFixture('workspace_provider_config_response.json');

    const list = parseWorkspaceProviderConfigsResponse(listPayload);
    const single = parseWorkspaceProviderConfigResponse(providerPayload);

    expect(list.total).toBe(1);
    expect(list.providers[0]?.options).toEqual({ location: 'us-east-1', note: 'primary' });
    expect(single.provider.provider).toBe('runpod');
  });

  it('workspace admin API functions surface the shared auth error fixtures', async () => {
    const providerRequest = loadJSONFixture('workspace_provider_config_upsert_request.json');

    mockFetch
      .mockResolvedValueOnce({
        ok: false,
        status: 403,
        statusText: 'Forbidden',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_usage_access_required.json'),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 400,
        statusText: 'Bad Request',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_unknown_provider.json'),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 405,
        statusText: 'Method Not Allowed',
        headers: { get: () => 'application/json' },
        json: async () => loadJSONFixture('auth_error_method_not_allowed.json'),
      });

    await expect(fetchWorkspaceQuota('ws_alpha')).rejects.toThrow(
      'Failed to fetch workspace quota (403 Forbidden): Usage access required.',
    );
    await expect(upsertWorkspaceProviderConfig('ws_alpha', 'not-a-provider', providerRequest)).rejects.toThrow(
      'Failed to save workspace provider config (400 Bad Request): Unknown provider',
    );
    await expect(updateWorkspaceQuota('ws_alpha', loadJSONFixture('workspace_quota_update_request.json'))).rejects.toThrow(
      'Failed to update workspace quota (405 Method Not Allowed): Method not allowed',
    );
  });

  it('workspace admin API functions use the shared request and response fixtures', async () => {
    const quotaResponse = loadJSONFixture('workspace_quota_response.json');
    const quotaRequest = loadJSONFixture('workspace_quota_update_request.json');
    const providerListResponse = loadJSONFixture('workspace_provider_configs_list_response.json');
    const providerRequest = loadJSONFixture('workspace_provider_config_upsert_request.json');
    const providerResponse = loadJSONFixture('workspace_provider_config_response.json');

    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => quotaResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => quotaResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => providerListResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => providerResponse });

    await expect(fetchWorkspaceQuota('ws_alpha')).resolves.toEqual(
      expect.objectContaining({ workspace_id: 'ws_alpha', monthly_request_limit: 250 }),
    );
    await expect(updateWorkspaceQuota('ws_alpha', quotaRequest)).resolves.toEqual(
      expect.objectContaining({ monthly_token_limit: 5000, enforce_hard_limits: false }),
    );
    await expect(fetchWorkspaceProviderConfigs('ws_alpha')).resolves.toEqual([
      expect.objectContaining({ workspace_id: 'ws_alpha', provider: 'runpod' }),
    ]);
    await expect(upsertWorkspaceProviderConfig('ws_alpha', 'runpod', providerRequest)).resolves.toEqual(
      expect.objectContaining({ configured: true, endpoint: 'https://api.runpod.io/graphql' }),
    );

    expect(JSON.parse(String(mockFetch.mock.calls[1]?.[1]?.body))).toEqual(quotaRequest);
    expect(JSON.parse(String(mockFetch.mock.calls[3]?.[1]?.body))).toEqual(providerRequest);
  });
});
