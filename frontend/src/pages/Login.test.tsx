/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Login } from './Login'

const analyticsMocks = vi.hoisted(() => ({
  track: vi.fn(),
  trackFirst: vi.fn(),
}))

vi.mock('../lib/authAccessClient', () => ({
  createSession: vi.fn(),
}))

vi.mock('../lib/publicAnalytics', () => ({
  publicAnalytics: analyticsMocks,
}))

import { createSession } from '../lib/authAccessClient'
import { SessionCreateError } from '../lib/authAccess'

const mockCreateSession = createSession as ReturnType<typeof vi.fn>

function renderLogin(onAuthenticated = vi.fn(), intakeEndpoint = '') {
  return render(
    <MemoryRouter>
      <Login onAuthenticated={onAuthenticated} intakeEndpoint={intakeEndpoint} />
    </MemoryRouter>,
  )
}

describe('Login', () => {
  const mockOnAuthenticated = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders the public-shell sign-in experience without runtime dashboard content', () => {
    renderLogin(mockOnAuthenticated)

    expect(screen.getByText('INFERA.AI')).toBeInTheDocument()
    expect(screen.getByText('OPEN INFERENCE CONTROL PLANE')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Sign in with a human dashboard key' })).toBeInTheDocument()
    expect(screen.getByLabelText('Human dashboard key')).toHaveAttribute('type', 'password')
    expect(screen.getByText('Human dashboard access')).toBeInTheDocument()
    expect(screen.getByText('Stored server-side')).toBeInTheDocument()
    expect(screen.getByText('Bound to the key workspace')).toBeInTheDocument()
    expect(screen.queryByText(/workers connected/i)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Connect' })).toBeInTheDocument()
  })

  it('fails closed to evaluation while preserving the invitation path when access intake is unconfigured', () => {
    renderLogin(mockOnAuthenticated)

    const guidance = screen.getByRole('region', { name: 'Don’t have a dashboard key?' })
    expect(within(guidance).getByText(/Access is approved before sign-in/)).toHaveTextContent(
      'After approval, a workspace admin issues an active human dashboard key.',
    )
    expect(within(guidance).getByText(/Service-account and inference keys cannot start/)).toHaveTextContent(
      'New access requests are not open right now.',
    )
    expect(within(guidance).queryByRole('link', { name: /Request access/ })).not.toBeInTheDocument()
    expect(within(guidance).getByRole('link', { name: /Evaluate deployment fit/ })).toHaveAttribute('href', '/evaluation')
    expect(within(guidance).getByRole('link', { name: /Accept a workspace invitation/ })).toHaveAttribute('href', '/accept-invite')
  })

  it('offers request access while preserving the invitation path when access intake is configured', () => {
    renderLogin(mockOnAuthenticated, '/api/design-partner-request')

    const guidance = screen.getByRole('region', { name: 'Don’t have a dashboard key?' })
    expect(within(guidance).queryByText(/New access requests are not open/)).not.toBeInTheDocument()
    expect(within(guidance).getByRole('link', { name: /Request access/ })).toHaveAttribute('href', '/request-access')
    expect(within(guidance).queryByRole('link', { name: /Evaluate deployment fit/ })).not.toBeInTheDocument()
    expect(within(guidance).getByRole('link', { name: /Accept a workspace invitation/ })).toHaveAttribute('href', '/accept-invite')
  })

  it('shows an accessible error and focuses the field on empty submit', async () => {
    renderLogin(mockOnAuthenticated)
    const input = screen.getByLabelText('Human dashboard key')

    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    expect(screen.getByRole('alert')).toHaveTextContent('Enter a human dashboard key to continue.')
    expect(input).toHaveFocus()
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveAttribute('aria-describedby', 'login-key-help login-key-error')
    expect(mockCreateSession).not.toHaveBeenCalled()
    expect(analyticsMocks.track).not.toHaveBeenCalled()
  })

  it('reveals and hides the key while preserving input focus', () => {
    renderLogin(mockOnAuthenticated)
    const input = screen.getByLabelText('Human dashboard key')

    fireEvent.click(screen.getByRole('button', { name: 'Show human dashboard key' }))
    expect(input).toHaveAttribute('type', 'text')
    expect(input).toHaveFocus()

    fireEvent.click(screen.getByRole('button', { name: 'Hide human dashboard key' }))
    expect(input).toHaveAttribute('type', 'password')
    expect(input).toHaveFocus()
  })

  it('shows the human-dashboard invalid-key message and returns focus to the field', async () => {
    mockCreateSession.mockRejectedValueOnce(new SessionCreateError('invalid_credentials', 401, 'authentication_error'))
    renderLogin(mockOnAuthenticated)
    const input = screen.getByLabelText('Human dashboard key')
    fireEvent.change(input, { target: { value: 'inf_badkey123' } })

    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Invalid or revoked human dashboard key. Check your key and try again.',
      )
    })
    expect(input).toHaveFocus()
  })

  it.each(['Dashboard access required', 'Admin access required'])(
    'explains dashboard access requirements without preserving legacy admin-only copy for %s',
    async () => {
      mockCreateSession.mockRejectedValueOnce(
        new SessionCreateError('dashboard_access_forbidden', 403, 'authorization_error'),
      )
      renderLogin(mockOnAuthenticated)
      fireEvent.change(screen.getByLabelText('Human dashboard key'), {
        target: { value: 'inf_userkey123' },
      })

      fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

      await waitFor(() => {
        expect(screen.getByRole('alert')).toHaveTextContent(
          'Dashboard access required. Use an active human key with dashboard access; inference-only keys cannot sign in.',
        )
      })
    },
  )

  it('keeps service-account keys on the machine API path', async () => {
    mockCreateSession.mockRejectedValueOnce(
      new SessionCreateError('service_account_forbidden', 403, 'authorization_error'),
    )
    renderLogin(mockOnAuthenticated)
    fireEvent.change(screen.getByLabelText('Human dashboard key'), {
      target: { value: 'inf_servicekey123' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Dashboard access requires a human key. Service-account keys are for API and automation use.',
      )
    })
  })

  it('authenticates with a trimmed valid key and reports the public intent', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const session = {
      session: { id: 'sess-1', expires_at: '2099-01-01T00:00:00Z' },
      key: { id: 'k1', key_prefix: 'inf_abcd', name: 'admin', role: 'admin' as const },
    }
    mockCreateSession.mockResolvedValueOnce(session)
    renderLogin(mockOnAuthenticated)
    fireEvent.change(screen.getByLabelText('Human dashboard key'), {
      target: { value: '  inf_validkey123  ' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(() => {
      expect(mockCreateSession).toHaveBeenCalledWith('inf_validkey123')
      expect(screen.getByText('Connected')).toBeInTheDocument()
    })
    expect(analyticsMocks.track).toHaveBeenCalledWith('public_sign_in_intent', {
      source: 'sign_in_form',
    })

    await act(async () => {
      vi.advanceTimersByTime(500)
    })
    expect(mockOnAuthenticated).toHaveBeenCalledWith(session)
  })

  it('shows a useful gateway error for an unknown failure', async () => {
    mockCreateSession.mockRejectedValueOnce(new Error('Network error'))
    renderLogin(mockOnAuthenticated)
    fireEvent.change(screen.getByLabelText('Human dashboard key'), {
      target: { value: 'inf_somekey123' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Could not connect to the gateway. Check its availability and try again.',
      )
    })
  })

  it('does not describe a gateway HTTP failure as a connectivity failure or expose server detail', async () => {
    mockCreateSession.mockRejectedValueOnce(
      new SessionCreateError('gateway_response_error', 500, 'internal_error'),
    )
    renderLogin(mockOnAuthenticated)
    fireEvent.change(screen.getByLabelText('Human dashboard key'), {
      target: { value: 'inf_somekey123' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Sign-in is temporarily unavailable. Try again in a moment.',
      )
    })
    expect(screen.getByRole('alert')).not.toHaveTextContent('Could not connect')
  })

  it('clears validation state as the user edits the key', () => {
    renderLogin(mockOnAuthenticated)
    const input = screen.getByLabelText('Human dashboard key')
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    expect(screen.getByRole('alert')).toBeInTheDocument()

    fireEvent.change(input, { target: { value: 'a' } })

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(input).toHaveAttribute('aria-invalid', 'false')
    expect(input).toHaveAttribute('aria-describedby', 'login-key-help')
  })
})
