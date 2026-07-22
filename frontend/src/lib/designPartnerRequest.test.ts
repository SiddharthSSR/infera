/// <reference types="vitest/globals" />
import { describe, expect, it, vi } from 'vitest';
import { getDesignPartnerRequestEndpoint, submitDesignPartnerRequest } from './designPartnerRequest';

const request = {
  workEmail: 'operator@example.com',
  company: 'Example Systems',
  role: 'Infrastructure lead',
  currentInferenceStack: 'OpenAI-compatible client with self-hosted workers',
  evaluationGoal: 'Evaluate a controlled migration of one inference route.',
};

describe('design-partner request configuration', () => {
  it('accepts same-origin paths and HTTPS destinations only', () => {
    expect(getDesignPartnerRequestEndpoint({ VITE_DESIGN_PARTNER_REQUEST_ENDPOINT: '/api/design-partner-requests' })).toBe('/api/design-partner-requests');
    expect(getDesignPartnerRequestEndpoint({ VITE_DESIGN_PARTNER_REQUEST_ENDPOINT: 'https://intake.example.com/requests' })).toBe('https://intake.example.com/requests');
    expect(getDesignPartnerRequestEndpoint({ VITE_DESIGN_PARTNER_REQUEST_ENDPOINT: 'http://intake.example.com/requests' })).toBeUndefined();
    expect(getDesignPartnerRequestEndpoint({ VITE_DESIGN_PARTNER_REQUEST_ENDPOINT: '//intake.example.com/requests' })).toBeUndefined();
    expect(getDesignPartnerRequestEndpoint({})).toBeUndefined();
  });

  it('posts only the approved fields without cookies or referrer data', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: true, status: 202 });

    await submitDesignPartnerRequest(request, {
      endpoint: '/api/design-partner-requests',
      fetcher: fetcher as typeof fetch,
    });

    expect(fetcher).toHaveBeenCalledWith('/api/design-partner-requests', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
      credentials: 'omit',
      referrerPolicy: 'no-referrer',
    });
  });

  it('surfaces non-success responses for recoverable page feedback', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: false, status: 503 });

    await expect(submitDesignPartnerRequest(request, {
      endpoint: '/api/design-partner-requests',
      fetcher: fetcher as typeof fetch,
    })).rejects.toThrow('Design-partner request endpoint returned 503');
  });
});
