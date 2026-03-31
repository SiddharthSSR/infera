/// <reference types="vitest/globals" />
import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  createSession,
  getSession,
  destroySession,
  fetchWorkspaces,
  fetchWorkers,
  fetchModels,
  fetchAgents,
  fetchStats,
  createAgentRun,
  fetchAgentRunDetail,
  cancelAgentRun,
  fetchInstances,
  fetchOfferings,
  fetchProviders,
  fetchCosts,
  fetchDeploymentAttempts,
  fetchApiKeys,
  createApiKey,
  revokeApiKey,
  fetchWorkspaceQuota,
  updateWorkspaceQuota,
  fetchWorkspaceMembers,
  updateWorkspaceMember,
  removeWorkspaceMember,
  fetchWorkspaceInvites,
  fetchInvitationPreview,
  acceptWorkspaceInvitation,
  fetchWorkspaceProviderConfigs,
  createWorkspaceInvite,
  upsertWorkspaceProviderConfig,
  deleteWorkspaceProviderConfig,
  revokeWorkspaceInvite,
  fetchAuditUsage,
  provisionInstance,
  terminateInstance,
  startInstance,
  stopInstance,
  updateDeploymentVerification,
  markDeploymentAutoVerificationRequested,
  switchSessionWorkspace,
} from './api'

const mockFetch = vi.fn()
;(globalThis as any).fetch = mockFetch

describe('API Functions', () => {
  beforeEach(() => {
    mockFetch.mockClear()
  })

  describe('session auth', () => {
    it('createSession should create server-side session', async () => {
      const payload = {
        session: { id: 'sess-1', expires_at: '2099-01-01T00:00:00Z' },
        key: { id: 'k1', key_prefix: 'inf_abcd', name: 'admin', role: 'admin' },
      }

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => payload,
      })

      const result = await createSession('inf_valid_key')

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/auth/session',
        expect.objectContaining({
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ api_key: 'inf_valid_key' }),
        })
      )
      expect(result).toEqual(payload)
    })

    it('createSession returns invalid-key message for 401', async () => {
      mockFetch.mockResolvedValueOnce({ ok: false, status: 401 })
      await expect(createSession('inf_bad')).rejects.toThrow('Invalid API key')
    })

    it('createSession returns admin-required message for 403', async () => {
      mockFetch.mockResolvedValueOnce({ ok: false, status: 403 })
      await expect(createSession('inf_user')).rejects.toThrow('Admin access required')
    })

    it('getSession returns null when unauthenticated', async () => {
      mockFetch.mockResolvedValueOnce({ ok: false, status: 401 })
      await expect(getSession()).resolves.toBeNull()
    })

    it('destroySession should not throw on network errors', async () => {
      mockFetch.mockRejectedValueOnce(new Error('network down'))
      await expect(destroySession()).resolves.toBeUndefined()
    })

    it('switchSessionWorkspace should update session workspace', async () => {
      const payload = {
        session: { id: 'sess-1', expires_at: '2099-01-01T00:00:00Z' },
        key: { id: 'k2', key_prefix: 'inf_qwer', name: 'member', role: 'operator', workspace_id: 'ws_2' },
        workspace: { id: 'ws_2', slug: 'beta-team', name: 'Beta Team' },
      }

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => payload,
      })

      const result = await switchSessionWorkspace('ws_2')

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/auth/session/workspace',
        expect.objectContaining({
          method: 'PUT',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ workspace_id: 'ws_2' }),
        }),
      )
      expect(result).toEqual(payload)
    })
  })

  describe('core endpoints', () => {
    it('fetchWorkers should call endpoint with cookie credentials', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ workers: [{ worker_id: 'w1', status: 'healthy' }] }),
      })

      const workers = await fetchWorkers()

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/workers',
        expect.objectContaining({ credentials: 'include' })
      )
      expect(workers).toHaveLength(1)
    })

    it('dispatches auth-expired event on 401', async () => {
      const handler = vi.fn()
      window.addEventListener('auth-expired', handler)

      mockFetch.mockResolvedValueOnce({ ok: false, status: 401 })

      await expect(fetchWorkers()).rejects.toThrow('Failed to fetch workers')
      expect(handler).toHaveBeenCalledTimes(1)

      window.removeEventListener('auth-expired', handler)
    })

    it('fetchModels/fetchStats/fetchInstances/fetchOfferings/fetchProviders/fetchCosts should parse payloads', async () => {
      mockFetch
        .mockResolvedValueOnce({ ok: true, json: async () => ({ data: [{ id: 'llama-3-8b', object: 'model' }] }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ workers: { total: 1, healthy: 1 } }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ instances: [{ id: 'i1', status: 'running' }] }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ offerings: [{ gpu_type: 'RTX_4090' }] }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ providers: [{ provider: 'runpod', connected: true }] }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ current_hourly: 1.5 }) })

      await expect(fetchModels()).resolves.toHaveLength(1)
      await expect(fetchStats()).resolves.toEqual(expect.objectContaining({ workers: { total: 1, healthy: 1 } }))
      await expect(fetchInstances()).resolves.toHaveLength(1)
      await expect(fetchOfferings()).resolves.toHaveLength(1)
      await expect(fetchProviders()).resolves.toHaveLength(1)
      await expect(fetchCosts()).resolves.toEqual(expect.objectContaining({ current_hourly: 1.5 }))
    })

    it('fetchAgents/createAgentRun/fetchAgentRunDetail/cancelAgentRun should parse agent payloads', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            default_agent_id: 'hermes',
            agents: [{ id: 'hermes', name: 'Hermes', description: 'ops assistant', default_max_steps: 8, tools: [] }],
          }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            run: {
              id: 'run_1',
              workspace_id: 'ws_alpha',
              agent_id: 'hermes',
              model: 'Qwen/Qwen2.5-7B-Instruct',
              input: 'inspect workers',
              status: 'queued',
              max_steps: 8,
              current_step: 0,
              created_at: '2026-03-31T12:00:00Z',
              updated_at: '2026-03-31T12:00:00Z',
            },
          }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            run: {
              id: 'run_1',
              workspace_id: 'ws_alpha',
              agent_id: 'hermes',
              model: 'Qwen/Qwen2.5-7B-Instruct',
              input: 'inspect workers',
              status: 'succeeded',
              max_steps: 8,
              current_step: 2,
              final_output: 'done',
              created_at: '2026-03-31T12:00:00Z',
              updated_at: '2026-03-31T12:00:02Z',
            },
            steps: [
              {
                id: 1,
                run_id: 'run_1',
                index: 0,
                type: 'tool_call',
                tool_name: 'list_workers',
                payload: {},
                created_at: '2026-03-31T12:00:01Z',
              },
            ],
          }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            run: {
              id: 'run_1',
              workspace_id: 'ws_alpha',
              agent_id: 'hermes',
              model: 'Qwen/Qwen2.5-7B-Instruct',
              input: 'inspect workers',
              status: 'canceled',
              max_steps: 8,
              current_step: 1,
              created_at: '2026-03-31T12:00:00Z',
              updated_at: '2026-03-31T12:00:01Z',
            },
          }),
        })

      await expect(fetchAgents()).resolves.toEqual(expect.objectContaining({
        default_agent_id: 'hermes',
        agents: [expect.objectContaining({ id: 'hermes' })],
      }))
      await expect(createAgentRun({
        agent_id: 'hermes',
        model: 'Qwen/Qwen2.5-7B-Instruct',
        input: 'inspect workers',
        max_steps: 8,
      })).resolves.toEqual(expect.objectContaining({ id: 'run_1', status: 'queued' }))
      await expect(fetchAgentRunDetail('run_1')).resolves.toEqual(expect.objectContaining({
        run: expect.objectContaining({ id: 'run_1', status: 'succeeded' }),
        steps: [expect.objectContaining({ type: 'tool_call', tool_name: 'list_workers' })],
      }))
      await expect(cancelAgentRun('run_1')).resolves.toEqual(expect.objectContaining({ id: 'run_1', status: 'canceled' }))
    })

    it('provision/start/stop/terminate should hit expected methods', async () => {
      mockFetch
        .mockResolvedValueOnce({ ok: true, json: async () => ({ instance: { id: 'new-inst', name: 'worker-1' } }) })
        .mockResolvedValueOnce({ ok: true })
        .mockResolvedValueOnce({ ok: true })
        .mockResolvedValueOnce({ ok: true })

      await provisionInstance({ name: 'worker-1', provider: 'mock', gpu_type: 'RTX_4090', gpu_count: 1 })
      await startInstance('new-inst')
      await stopInstance('new-inst')
      await terminateInstance('new-inst')

      expect(mockFetch).toHaveBeenNthCalledWith(
        1,
        '/api/instances/provision',
        expect.objectContaining({ method: 'POST', credentials: 'include' })
      )
      expect(mockFetch).toHaveBeenNthCalledWith(
        2,
        '/api/instances/new-inst/start',
        expect.objectContaining({ method: 'POST', credentials: 'include' })
      )
      expect(mockFetch).toHaveBeenNthCalledWith(
        3,
        '/api/instances/new-inst/stop',
        expect.objectContaining({ method: 'POST', credentials: 'include' })
      )
      expect(mockFetch).toHaveBeenNthCalledWith(
        4,
        '/api/instances/new-inst',
        expect.objectContaining({ method: 'DELETE', credentials: 'include' })
      )
    })

    it('fetchDeploymentAttempts/updateDeploymentVerification/markDeploymentAutoVerificationRequested should parse payloads', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            attempts: [
              {
                id: 'attempt-1',
                outcome: 'provisioned',
                request: { gpu_type: 'RTX_4090' },
                created_at: '2026-03-16T00:00:00Z',
                updated_at: '2026-03-16T00:00:00Z',
              },
            ],
          }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            attempt: {
              id: 'attempt-1',
              outcome: 'provisioned',
              request: { gpu_type: 'RTX_4090' },
              created_at: '2026-03-16T00:00:00Z',
              updated_at: '2026-03-16T00:01:00Z',
              inference_verification: {
                status: 'passed',
                verified_at: '2026-03-16T00:01:00Z',
                latency_ms: 420,
              },
            },
          }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            attempt: {
              id: 'attempt-1',
              outcome: 'provisioned',
              request: { gpu_type: 'RTX_4090' },
              created_at: '2026-03-16T00:00:00Z',
              updated_at: '2026-03-16T00:01:00Z',
              auto_verification_requested_at: '2026-03-16T00:00:30Z',
            },
          }),
        })

      await expect(fetchDeploymentAttempts()).resolves.toHaveLength(1)
      await expect(updateDeploymentVerification('attempt-1', {
        status: 'passed',
        verified_at: '2026-03-16T00:01:00Z',
        latency_ms: 420,
      })).resolves.toEqual(expect.objectContaining({
        id: 'attempt-1',
        inference_verification: expect.objectContaining({ status: 'passed' }),
      }))
      await expect(markDeploymentAutoVerificationRequested('attempt-1', '2026-03-16T00:00:30Z')).resolves.toEqual(
        expect.objectContaining({
          id: 'attempt-1',
          auto_verification_requested_at: '2026-03-16T00:00:30Z',
        }),
      )

      expect(mockFetch).toHaveBeenNthCalledWith(
        1,
        '/api/deployments',
        expect.objectContaining({ credentials: 'include' }),
      )
      expect(mockFetch).toHaveBeenNthCalledWith(
        2,
        '/api/deployments/attempt-1/verification',
        expect.objectContaining({ method: 'PUT', credentials: 'include' }),
      )
      expect(mockFetch).toHaveBeenNthCalledWith(
        3,
        '/api/deployments/attempt-1/auto-verification',
        expect.objectContaining({ method: 'PUT', credentials: 'include' }),
      )
    })
  })

  describe('error parsing', () => {
    it('createApiKey handles non-JSON error responses', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 502,
        statusText: 'Bad Gateway',
        headers: { get: () => 'text/html' },
        text: async () => '<html>upstream failure</html>',
      })

      await expect(createApiKey('test-key')).rejects.toThrow('Failed to create key (502 Bad Gateway)')
    })

    it('revokeApiKey handles empty error bodies', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        headers: { get: () => '' },
        text: async () => '',
      })

      await expect(revokeApiKey('key-1')).rejects.toThrow('Failed to revoke key (500 Internal Server Error)')
    })

    it('fetchApiKeys preserves status on JSON errors', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 503,
        statusText: 'Service Unavailable',
        headers: { get: () => 'application/json' },
        json: async () => ({ error: { message: 'auth backend unavailable' } }),
      })

      await expect(fetchApiKeys()).rejects.toThrow('503 Service Unavailable')
    })
  })

  describe('workspace admin endpoints', () => {
    it('fetchWorkspaces parses workspace list', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          workspaces: [
            { id: 'ws_1', slug: 'alpha', name: 'Alpha', created_at: '2026-03-15T00:00:00Z', status: 'active' },
            { id: 'ws_2', slug: 'beta', name: 'Beta', created_at: '2026-03-15T00:00:00Z', status: 'active' },
          ],
        }),
      })

      const workspaces = await fetchWorkspaces()

      expect(workspaces).toHaveLength(2)
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/auth/workspaces',
        expect.objectContaining({ credentials: 'include' }),
      )
    })

    it('fetchWorkspaceQuota/fetchWorkspaceMembers/fetchWorkspaceInvites parse payloads', async () => {
      mockFetch
        .mockResolvedValueOnce({ ok: true, json: async () => ({ quota: { workspace_id: 'ws_1', enforce_hard_limits: true } }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ members: [{ id: 'm1', email: 'member@example.com' }] }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ invitations: [{ id: 'inv_1', email: 'invite@example.com' }] }) })

      await expect(fetchWorkspaceQuota('ws_1')).resolves.toEqual(expect.objectContaining({ workspace_id: 'ws_1' }))
      await expect(fetchWorkspaceMembers('ws_1')).resolves.toHaveLength(1)
      await expect(fetchWorkspaceInvites('ws_1')).resolves.toHaveLength(1)
    })

    it('fetchInvitationPreview/acceptWorkspaceInvitation hit expected methods', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            invitation: {
              workspace_id: 'ws_1',
              workspace_slug: 'workspace',
              workspace_name: 'Workspace',
              role: 'developer',
              email: 'invite@example.com',
              display_name: 'Invite User',
              expires_at: '2026-03-20T00:00:00Z',
              status: 'pending',
            },
          }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({
            membership: { id: 'm1', workspace_id: 'ws_1', email: 'invite@example.com', display_name: 'Invite User', role: 'developer', status: 'active', created_at: '2026-03-14T00:00:00Z' },
            key: 'inf_secret',
            record: { id: 'k1', key_prefix: 'inf_abcd...', name: 'Invite User', role: 'developer', created_at: '2026-03-14T00:00:00Z', status: 'active' },
          }),
        })

      await expect(fetchInvitationPreview('invite_token')).resolves.toEqual(
        expect.objectContaining({ workspace_id: 'ws_1', role: 'developer' })
      )
      await expect(acceptWorkspaceInvitation('invite_token', 'Invite User')).resolves.toEqual(
        expect.objectContaining({ key: 'inf_secret' })
      )

      expect(mockFetch).toHaveBeenNthCalledWith(1, '/api/auth/invitations/preview?token=invite_token')
      expect(mockFetch).toHaveBeenNthCalledWith(
        2,
        '/api/auth/invitations/accept',
        expect.objectContaining({ method: 'POST' })
      )
    })

    it('updateWorkspaceMember/removeWorkspaceMember hit expected methods', async () => {
      mockFetch
        .mockResolvedValueOnce({ ok: true, json: async () => ({ member: { id: 'm1', role: 'operator' } }) })
        .mockResolvedValueOnce({ ok: true })

      await updateWorkspaceMember('ws_1', 'm1', { role: 'operator' })
      await removeWorkspaceMember('ws_1', 'm1')

      expect(mockFetch).toHaveBeenNthCalledWith(
        1,
        '/api/auth/workspaces/ws_1/members/m1',
        expect.objectContaining({ method: 'PUT', credentials: 'include' })
      )
      expect(mockFetch).toHaveBeenNthCalledWith(
        2,
        '/api/auth/workspaces/ws_1/members/m1',
        expect.objectContaining({ method: 'DELETE', credentials: 'include' })
      )
    })

    it('fetchWorkspaceProviderConfigs parses configured providers', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          providers: [{ workspace_id: 'ws_1', provider: 'runpod', configured: true, endpoint: 'https://api.runpod.io/graphql', options: { location: 'us-east-1' } }],
        }),
      })

      await expect(fetchWorkspaceProviderConfigs('ws_1')).resolves.toEqual([
        expect.objectContaining({ workspace_id: 'ws_1', provider: 'runpod', configured: true, options: { location: 'us-east-1' } }),
      ])
    })

    it('updateWorkspaceQuota/createWorkspaceInvite/revokeWorkspaceInvite hit expected methods', async () => {
      mockFetch
        .mockResolvedValueOnce({ ok: true, json: async () => ({ quota: { workspace_id: 'ws_1', enforce_hard_limits: false } }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ invitation_token: 'invite_token', invitation: { id: 'inv_1' } }) })
        .mockResolvedValueOnce({ ok: true })

      await updateWorkspaceQuota('ws_1', { monthly_request_limit: 100, monthly_token_limit: 1000, enforce_hard_limits: false })
      await createWorkspaceInvite('ws_1', { email: 'new@example.com', role: 'developer' })
      await revokeWorkspaceInvite('ws_1', 'inv_1')

      expect(mockFetch).toHaveBeenNthCalledWith(
        1,
        '/api/auth/workspaces/ws_1/quota',
        expect.objectContaining({ method: 'PUT', credentials: 'include' })
      )
      expect(mockFetch).toHaveBeenNthCalledWith(
        2,
        '/api/auth/workspaces/ws_1/invites',
        expect.objectContaining({ method: 'POST', credentials: 'include' })
      )
      expect(mockFetch).toHaveBeenNthCalledWith(
        3,
        '/api/auth/workspaces/ws_1/invites/inv_1',
        expect.objectContaining({ method: 'DELETE', credentials: 'include' })
      )
    })

    it('upsertWorkspaceProviderConfig/deleteWorkspaceProviderConfig hit expected methods', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: async () => ({ provider: { workspace_id: 'ws_1', provider: 'runpod', configured: true } }),
        })
        .mockResolvedValueOnce({ ok: true })

      await upsertWorkspaceProviderConfig('ws_1', 'runpod', {
        api_key: 'rp_key',
        api_secret: 'rp_secret',
        endpoint: 'https://api.runpod.io/graphql',
        options: { location: 'us-east-1' },
      })
      await deleteWorkspaceProviderConfig('ws_1', 'runpod')

      expect(mockFetch).toHaveBeenNthCalledWith(
        1,
        '/api/auth/workspaces/ws_1/providers/runpod',
        expect.objectContaining({ method: 'PUT', credentials: 'include' })
      )
      expect(mockFetch).toHaveBeenNthCalledWith(
        2,
        '/api/auth/workspaces/ws_1/providers/runpod',
        expect.objectContaining({ method: 'DELETE', credentials: 'include' })
      )
    })

    it('fetchAuditUsage builds query parameters and parses rows', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          bucket: 'day',
          start: '2026-03-01T00:00:00Z',
          end: '2026-03-31T23:59:59Z',
          rows: [{ bucket_start: '2026-03-10T00:00:00Z', workspace_id: 'ws_1', key_id: 'key_1', requests: 12, tokens: 3456, successes: 11, errors: 1 }],
        }),
      })

      const result = await fetchAuditUsage({
        start: '2026-03-01T00:00:00Z',
        end: '2026-03-31T23:59:59Z',
        bucket: 'day',
        workspace_id: 'ws_1',
      })

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/audit/usage?start=2026-03-01T00%3A00%3A00Z&end=2026-03-31T23%3A59%3A59Z&bucket=day&workspace_id=ws_1',
        expect.objectContaining({ credentials: 'include' })
      )
      expect(result.rows).toHaveLength(1)
      expect(result.rows[0]).toEqual(expect.objectContaining({ requests: 12, tokens: 3456 }))
    })
  })
})
