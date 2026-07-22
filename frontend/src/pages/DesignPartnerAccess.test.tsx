/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DesignPartnerAccess } from './DesignPartnerAccess';

const analyticsMocks = vi.hoisted(() => ({ track: vi.fn(), trackFirst: vi.fn() }));
vi.mock('../lib/publicAnalytics', () => ({ publicAnalytics: analyticsMocks }));

function renderPage(endpoint = '/api/design-partner-requests') {
  return render(<MemoryRouter><DesignPartnerAccess endpoint={endpoint} /></MemoryRouter>);
}

function completeForm() {
  fireEvent.change(screen.getByLabelText('Work email'), { target: { value: 'operator@example.com' } });
  fireEvent.change(screen.getByLabelText('Company or organization'), { target: { value: 'Example Systems' } });
  fireEvent.change(screen.getByLabelText('Role'), { target: { value: 'Infrastructure lead' } });
  fireEvent.change(screen.getByLabelText('Current inference stack'), { target: { value: 'OpenAI-compatible client with self-hosted workers' } });
  fireEvent.change(screen.getByLabelText('Evaluation goal'), { target: { value: 'Evaluate migration of one inference route.' } });
  fireEvent.click(screen.getByRole('checkbox'));
}

describe('DesignPartnerAccess', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(fetch).mockReset();
  });

  it('collects only the approved details and explains the privacy boundary', () => {
    renderPage();

    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByLabelText('Work email')).toHaveAttribute('type', 'email');
    expect(screen.getByLabelText('Company or organization')).toBeInTheDocument();
    expect(screen.getByLabelText('Role')).toBeInTheDocument();
    expect(screen.getByLabelText('Current inference stack')).toBeInTheDocument();
    expect(screen.getByLabelText('Evaluation goal')).toBeInTheDocument();
    expect(screen.getByText(/Do not include/i).closest('p')).toHaveTextContent(/API keys, credentials, prompts, model output, customer data/i);
    expect(screen.queryByLabelText(/phone/i)).not.toBeInTheDocument();
  });

  it('announces validation errors, preserves entries, and records a bounded outcome', async () => {
    renderPage();
    fireEvent.click(screen.getByRole('button', { name: 'Request design-partner access' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Check the highlighted fields.');
    expect(screen.getByLabelText('Work email')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByText('Enter a valid work email address.')).toBeInTheDocument();
    expect(analyticsMocks.track).toHaveBeenCalledWith('design_partner_request_submitted', { outcome: 'validation_failed' });
  });

  it('submits without credentials and shows a success state', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: true, status: 202 } as Response);
    renderPage();
    completeForm();
    fireEvent.click(screen.getByRole('button', { name: 'Request design-partner access' }));

    expect(await screen.findByRole('status')).toHaveTextContent('Your evaluation context was delivered.');
    const payload = JSON.parse(String(vi.mocked(fetch).mock.calls[0]?.[1]?.body));
    expect(payload).toEqual({
      workEmail: 'operator@example.com',
      company: 'Example Systems',
      role: 'Infrastructure lead',
      currentInferenceStack: 'OpenAI-compatible client with self-hosted workers',
      evaluationGoal: 'Evaluate migration of one inference route.',
    });
    expect(analyticsMocks.track).toHaveBeenCalledWith('design_partner_request_submitted', { outcome: 'succeeded' });
  });

  it('keeps entered values available after a delivery failure', async () => {
    vi.mocked(fetch).mockResolvedValue({ ok: false, status: 503 } as Response);
    renderPage();
    completeForm();
    fireEvent.click(screen.getByRole('button', { name: 'Request design-partner access' }));

    expect(await screen.findByText(/We could not deliver this request/)).toBeInTheDocument();
    expect(screen.getByLabelText('Work email')).toHaveValue('operator@example.com');
    expect(analyticsMocks.track).toHaveBeenCalledWith('design_partner_request_submitted', { outcome: 'delivery_failed' });
  });

  it('fails closed and names the administrator action when delivery is unconfigured', async () => {
    renderPage('');
    expect(screen.getByRole('status')).toHaveTextContent('administrator must connect an approved secure intake endpoint');
    completeForm();
    fireEvent.click(screen.getByRole('button', { name: 'Request design-partner access' }));
    await waitFor(() => expect(analyticsMocks.track).toHaveBeenCalledWith('design_partner_request_submitted', { outcome: 'configuration_missing' }));
    expect(fetch).not.toHaveBeenCalled();
  });
});
