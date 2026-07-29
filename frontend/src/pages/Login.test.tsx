/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
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

const mockCreateSession = createSession as ReturnType<typeof vi.fn>

function renderLogin(onAuthenticated = vi.fn()) {
  return render(
    <MemoryRouter>
      <Login onAuthenticated={onAuthenticated} />
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
    mockCreateSession.mockRejectedValueOnce(new Error('Invalid API key'))
    renderLogin(mockOnAuthenticated)
    const input = screen.getByLabelText('Human dashboard key')
    fireEvent.change(input, { target: { value: 'inf_badkey123' } })

    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(
        'Invalid human dashboard key. Check your key and try again.',
      )
    })
    expect(input).toHaveFocus()
  })

  it.each(['Dashboard access required', 'Admin access required'])(
    'explains dashboard access requirements without preserving legacy admin-only copy for %s',
    async (gatewayMessage) => {
      mockCreateSession.mockRejectedValueOnce(new Error(gatewayMessage))
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
    mockCreateSession.mockRejectedValueOnce(new Error('Service accounts cannot create dashboard sessions.'))
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
