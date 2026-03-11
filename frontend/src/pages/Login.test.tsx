/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { Login } from './Login'

// Mock the api module
vi.mock('../lib/api', () => ({
  createSession: vi.fn(),
}))

import { createSession } from '../lib/api'

const mockFetch = globalThis.fetch as ReturnType<typeof vi.fn>
const mockCreateSession = createSession as ReturnType<typeof vi.fn>

describe('Login', () => {
  const mockOnAuthenticated = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    mockFetch.mockReset()
    // Default: health check succeeds (prevents noisy rejections)
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'healthy', workers: 1 }),
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders branding and form elements', async () => {
    render(<Login onAuthenticated={mockOnAuthenticated} />)

    // Wait for health check to settle
    await waitFor(() => {
      expect(screen.getByText(/Gateway online/)).toBeInTheDocument()
    })

    expect(screen.getByText('INFERA')).toBeInTheDocument()
    expect(screen.getByText('API KEY')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('inf_...')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /connect/i })).toBeInTheDocument()
  })

  it('shows gateway online with worker count', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({ status: 'healthy', workers: 2 }),
    })

    render(<Login onAuthenticated={mockOnAuthenticated} />)

    await waitFor(() => {
      expect(screen.getByText(/Gateway online/)).toBeInTheDocument()
    })
    expect(screen.getByText(/2 workers connected/)).toBeInTheDocument()
  })

  it('shows gateway unreachable on health check failure', async () => {
    mockFetch.mockRejectedValue(new Error('network error'))

    render(<Login onAuthenticated={mockOnAuthenticated} />)

    await waitFor(() => {
      expect(screen.getByText('Gateway unreachable')).toBeInTheDocument()
    })
  })

  it('shows error on empty submit', async () => {
    render(<Login onAuthenticated={mockOnAuthenticated} />)

    // Wait for health check to settle
    await waitFor(() => {
      expect(screen.getByText(/Gateway online/)).toBeInTheDocument()
    })

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /connect/i }))
    })

    expect(screen.getByText('Please enter your API key')).toBeInTheDocument()
    expect(mockCreateSession).not.toHaveBeenCalled()
  })

  it('shows error for invalid key', async () => {
    mockCreateSession.mockRejectedValueOnce(new Error('Invalid API key'))

    render(<Login onAuthenticated={mockOnAuthenticated} />)

    await waitFor(() => {
      expect(screen.getByText(/Gateway online/)).toBeInTheDocument()
    })

    const input = screen.getByPlaceholderText('inf_...')
    fireEvent.change(input, { target: { value: 'inf_badkey123' } })

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /connect/i }))
    })

    await waitFor(() => {
      expect(screen.getByText('Invalid API key. Check your key and try again.')).toBeInTheDocument()
    })
  })

  it('shows admin access required when createSession returns 403', async () => {
    mockCreateSession.mockRejectedValueOnce(new Error('Admin access required'))

    render(<Login onAuthenticated={mockOnAuthenticated} />)

    await waitFor(() => {
      expect(screen.getByText(/Gateway online/)).toBeInTheDocument()
    })

    const input = screen.getByPlaceholderText('inf_...')
    fireEvent.change(input, { target: { value: 'inf_userkey123' } })

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /connect/i }))
    })

    await waitFor(() => {
      expect(screen.getByText('Admin access required. Only admin keys can access the dashboard.')).toBeInTheDocument()
    })
  })

  it('authenticates with valid key', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    mockCreateSession.mockResolvedValueOnce({
      session: { id: 'sess-1', expires_at: '2099-01-01T00:00:00Z' },
      key: { id: 'k1', key_prefix: 'inf_abcd', name: 'admin', role: 'admin' },
    })

    render(<Login onAuthenticated={mockOnAuthenticated} />)

    const input = screen.getByPlaceholderText('inf_...')
    fireEvent.change(input, { target: { value: 'inf_validkey123' } })

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /connect/i }))
    })

    await waitFor(() => {
      expect(mockCreateSession).toHaveBeenCalledWith('inf_validkey123')
    })

    // onAuthenticated fires after 500ms timeout
    await act(async () => {
      vi.advanceTimersByTime(500)
    })

    expect(mockOnAuthenticated).toHaveBeenCalled()
  })

  it('shows connection error when createSession throws unknown error', async () => {
    mockCreateSession.mockRejectedValueOnce(new Error('Network error'))

    render(<Login onAuthenticated={mockOnAuthenticated} />)

    await waitFor(() => {
      expect(screen.getByText(/Gateway online/)).toBeInTheDocument()
    })

    const input = screen.getByPlaceholderText('inf_...')
    fireEvent.change(input, { target: { value: 'inf_somekey123' } })

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /connect/i }))
    })

    await waitFor(() => {
      expect(screen.getByText('Could not connect to gateway. Is it running?')).toBeInTheDocument()
    })
  })

  it('clears error when typing', async () => {
    render(<Login onAuthenticated={mockOnAuthenticated} />)

    await waitFor(() => {
      expect(screen.getByText(/Gateway online/)).toBeInTheDocument()
    })

    // Trigger empty submit error
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /connect/i }))
    })

    expect(screen.getByText('Please enter your API key')).toBeInTheDocument()

    // Type in input — error should clear
    const input = screen.getByPlaceholderText('inf_...')
    fireEvent.change(input, { target: { value: 'a' } })

    expect(screen.queryByText('Please enter your API key')).not.toBeInTheDocument()
  })
})
