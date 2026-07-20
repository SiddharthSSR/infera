/// <reference types="vitest/globals" />
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AcceptInvitation } from './AcceptInvitation';

const mocks = vi.hoisted(() => ({
  acceptWorkspaceInvitation: vi.fn(),
  createSession: vi.fn(),
  fetchInvitationPreview: vi.fn(),
  navigate: vi.fn(),
  track: vi.fn(),
  trackFirst: vi.fn(),
}));

vi.mock('../lib/authAccessClient', () => ({
  acceptWorkspaceInvitation: mocks.acceptWorkspaceInvitation,
  createSession: mocks.createSession,
  fetchInvitationPreview: mocks.fetchInvitationPreview,
}));

vi.mock('../lib/publicAnalytics', () => ({
  publicAnalytics: {
    track: mocks.track,
    trackFirst: mocks.trackFirst,
  },
}));

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => mocks.navigate };
});

const invitation = {
  workspace_id: 'ws_1',
  workspace_slug: 'research',
  workspace_name: 'Research',
  email: 'person@example.com',
  display_name: 'Ada',
  role: 'admin',
  expires_at: '2026-08-01T12:00:00Z',
  status: 'pending',
};

const accepted = {
  membership: {
    id: 'member_1',
    workspace_id: 'ws_1',
    email: 'person@example.com',
    display_name: 'Ada',
    role: 'admin',
    status: 'active',
    created_at: '2026-07-19T12:00:00Z',
  },
  key: 'inf_human_once',
  record: {
    id: 'key_1',
    workspace_id: 'ws_1',
    workspace_slug: 'research',
    workspace_name: 'Research',
    key_prefix: 'inf_hum',
    name: 'Ada',
    role: 'admin',
    principal_type: 'human',
    created_at: '2026-07-19T12:00:00Z',
    status: 'active',
  },
};

const session = {
  session: { id: 'session_1', expires_at: '2026-07-20T12:00:00Z' },
  key: accepted.record,
  workspace: { id: 'ws_1', slug: 'research', name: 'Research' },
  member: { id: 'member_1', email: 'person@example.com', display_name: 'Ada' },
};

function renderInvitation(initialEntry = '/accept-invite') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <AcceptInvitation onAccepted={vi.fn()} />
    </MemoryRouter>,
  );
}

describe('AcceptInvitation', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  it('labels the token input and gives recovery guidance for an empty submission', () => {
    renderInvitation();

    expect(screen.getByRole('link', { name: 'LOGIN' })).toHaveAttribute('href', '/sign-in');
    expect(screen.getByLabelText('INVITATION TOKEN')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'LOAD INVITATION' }));

    expect(screen.getByRole('alert')).toHaveTextContent('Invitation token is required.');
    expect(screen.getByRole('alert')).toHaveTextContent('Paste the complete token');
    expect(screen.getByLabelText('INVITATION TOKEN')).toHaveAttribute('aria-invalid', 'true');
  });

  it('keeps the token available and explains recovery when preview fails', async () => {
    mocks.fetchInvitationPreview.mockRejectedValueOnce(new Error('Invitation expired'));
    renderInvitation();

    const input = screen.getByLabelText('INVITATION TOKEN');
    fireEvent.change(input, { target: { value: 'invite_expired' } });
    fireEvent.click(screen.getByRole('button', { name: 'LOAD INVITATION' }));

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Invitation expired'));
    expect(screen.getByRole('alert')).toHaveTextContent('Ask the workspace admin for a new invitation');
    expect(input).toHaveValue('invite_expired');
  });

  it('loads a manually entered token once when preserving it in the URL', async () => {
    mocks.fetchInvitationPreview.mockResolvedValue(invitation);
    renderInvitation();

    fireEvent.change(screen.getByLabelText('INVITATION TOKEN'), { target: { value: 'invite_valid' } });
    fireEvent.click(screen.getByRole('button', { name: 'LOAD INVITATION' }));

    await screen.findByText('Research');
    await waitFor(() => expect(mocks.fetchInvitationPreview).toHaveBeenCalledTimes(1));
  });

  it('accepts a human invitation and continues to the first authenticated action', async () => {
    const onAccepted = vi.fn();
    mocks.fetchInvitationPreview.mockResolvedValue(invitation);
    mocks.acceptWorkspaceInvitation.mockResolvedValue(accepted);
    mocks.createSession.mockResolvedValue(session);

    render(
      <MemoryRouter initialEntries={['/accept-invite?token=invite_valid']}>
        <AcceptInvitation onAccepted={onAccepted} />
      </MemoryRouter>,
    );

    await screen.findByText('Research');
    expect(screen.getByLabelText('DISPLAY NAME')).toHaveValue('Ada');
    fireEvent.click(screen.getByRole('button', { name: 'ACCEPT INVITATION' }));

    expect(await screen.findByText('ONE-TIME HUMAN KEY — COPY NOW')).toBeInTheDocument();
    expect(screen.getByText(/create a separate service-account key/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'CONTINUE TO WORKSPACE SETUP' }));

    await waitFor(() => expect(onAccepted).toHaveBeenCalledWith(session));
    expect(mocks.track).toHaveBeenCalledWith('public_sign_in_intent', { source: 'invitation' });
    expect(mocks.navigate).toHaveBeenCalledWith('/workspace', { replace: true });
    expect(mocks.fetchInvitationPreview).toHaveBeenCalledTimes(1);
  });

  it('keeps the accepted key visible when the browser session cannot start', async () => {
    mocks.fetchInvitationPreview.mockResolvedValue(invitation);
    mocks.acceptWorkspaceInvitation.mockResolvedValue(accepted);
    mocks.createSession.mockRejectedValue(new Error('Session unavailable'));
    renderInvitation('/accept-invite?token=invite_valid');

    await screen.findByText('Research');
    fireEvent.click(screen.getByRole('button', { name: 'ACCEPT INVITATION' }));
    await screen.findByText('inf_human_once');
    fireEvent.click(screen.getByRole('button', { name: 'CONTINUE TO WORKSPACE SETUP' }));

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Session unavailable'));
    expect(mocks.track).toHaveBeenCalledWith('public_sign_in_intent', { source: 'invitation' });
    expect(screen.getByRole('alert')).toHaveTextContent('Your human key is still shown below');
    expect(screen.getByText('inf_human_once')).toBeInTheDocument();
  });
});
