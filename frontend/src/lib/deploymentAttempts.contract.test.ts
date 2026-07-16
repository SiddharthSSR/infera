/// <reference types="vitest/globals" />
import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import {
  fetchDeploymentAttempts,
  markDeploymentAutoVerificationRequested,
  updateDeploymentVerification,
} from './api';
import {
  parseDeploymentAttemptResponse,
  parseDeploymentAttemptsResponse,
} from './deploymentAttempts';

const __dirname = dirname(fileURLToPath(import.meta.url));
const FIXTURE_DIR = resolve(__dirname, '../../../contracts/deployment_history');

const mockFetch = vi.fn();
(globalThis as { fetch?: typeof fetch }).fetch = mockFetch;

function loadJSONFixture(name: string) {
  return JSON.parse(readFileSync(resolve(FIXTURE_DIR, name), 'utf8'));
}

describe('deployment history contract fixtures', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('parses the shared deployment attempts list fixture', () => {
    const payload = loadJSONFixture('deployment_attempts_list_response.json');

    const parsed = parseDeploymentAttemptsResponse(payload);

    expect(parsed.total).toBe(1);
    expect(parsed.attempts[0]?.workspace_id).toBe('ws_alpha');
    expect(parsed.attempts[0]?.created_by_key_id).toBe('key_fixture');
    expect(parsed.attempts[0]?.request.engine).toBe('sglang');
    expect(parsed.attempts[0]?.request.options).toEqual({
      INFERA_SGLANG_CHUNKED_PREFILL_SIZE: '2048',
      INFERA_SGLANG_MAX_RUNNING_REQUESTS: '32',
      INFERA_SGLANG_MEM_FRACTION_STATIC: '0.90',
    });
    expect(parsed.attempts[0]?.inference_verification?.response_preview).toBe('READY');
  });

  it('parses the shared deployment update response fixtures', () => {
    const verificationPayload = loadJSONFixture('deployment_attempt_verification_response.json');
    const autoVerificationPayload = loadJSONFixture('deployment_attempt_auto_verification_response.json');

    const verification = parseDeploymentAttemptResponse(verificationPayload);
    const autoVerification = parseDeploymentAttemptResponse(autoVerificationPayload);

    expect(verification.attempt.inference_verification?.status).toBe('passed');
    expect(verification.attempt.auto_verification_requested_at).toBeUndefined();
    expect(autoVerification.attempt.auto_verification_requested_at).toBe('2026-04-10T00:00:30Z');
  });

  it('deployment API functions use the shared request and response fixtures', async () => {
    const attemptsList = loadJSONFixture('deployment_attempts_list_response.json');
    const verificationRequest = loadJSONFixture('deployment_attempt_verification_request.json');
    const verificationResponse = loadJSONFixture('deployment_attempt_verification_response.json');
    const autoVerificationRequest = loadJSONFixture('deployment_attempt_auto_verification_request.json');
    const autoVerificationResponse = loadJSONFixture('deployment_attempt_auto_verification_response.json');

    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => attemptsList })
      .mockResolvedValueOnce({ ok: true, json: async () => verificationResponse })
      .mockResolvedValueOnce({ ok: true, json: async () => autoVerificationResponse });

    await expect(fetchDeploymentAttempts()).resolves.toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          id: 'attempt_fixture_1',
          created_by_key_id: 'key_fixture',
          workspace_id: 'ws_alpha',
        }),
      ]),
    );
    await expect(updateDeploymentVerification('attempt_fixture_1', verificationRequest)).resolves.toEqual(
      expect.objectContaining({
        id: 'attempt_fixture_1',
        inference_verification: expect.objectContaining({ status: 'passed' }),
      }),
    );
    await expect(
      markDeploymentAutoVerificationRequested('attempt_fixture_1', autoVerificationRequest.requested_at),
    ).resolves.toEqual(
      expect.objectContaining({
        id: 'attempt_fixture_1',
        auto_verification_requested_at: '2026-04-10T00:00:30Z',
      }),
    );

    expect(JSON.parse(String(mockFetch.mock.calls[1]?.[1]?.body))).toEqual(verificationRequest);
    expect(JSON.parse(String(mockFetch.mock.calls[2]?.[1]?.body))).toEqual(autoVerificationRequest);
  });
});
