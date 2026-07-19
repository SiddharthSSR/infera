/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import App from './App';

vi.mock('./pages/Dashboard', () => ({ Dashboard: () => <div>DASHBOARD PAGE</div> }));
vi.mock('./pages/Playground', () => ({ Playground: () => <div>PLAYGROUND PAGE</div> }));
vi.mock('./pages/Instances', () => ({ Instances: () => <div>INSTANCES PAGE</div> }));
vi.mock('./pages/Logs', () => ({ Logs: () => <div>LOGS PAGE</div> }));
vi.mock('./pages/Models', () => ({ Models: () => <div>MODELS PAGE</div> }));
vi.mock('./pages/ApiKeys', () => ({ ApiKeys: () => <div>API KEYS PAGE</div> }));
vi.mock('./pages/WorkspaceAdmin', () => ({ WorkspaceAdmin: () => <div>WORKSPACE PAGE</div> }));
vi.mock('./pages/PublicApiDocs', () => ({ PublicApiDocs: () => <div className="top-nav">PUBLIC DOCS PAGE</div> }));
vi.mock('./pages/GettingStarted', () => ({ GettingStarted: () => <div className="top-nav">GETTING STARTED PAGE</div> }));
vi.mock('./pages/PublicLanding', () => ({ PublicLanding: () => <div>PUBLIC LANDING PAGE</div> }));
vi.mock('./pages/Login', () => ({ Login: () => <div>SIGN IN PAGE</div> }));
vi.mock('./pages/AcceptInvitation', () => ({ AcceptInvitation: () => <div>ACCEPT INVITATION PAGE</div> }));
vi.mock('./hooks/useIsMobile', () => ({ useIsMobile: vi.fn(() => false) }));

vi.mock('./lib/authAccessClient', () => ({
  getSession: vi.fn(),
  destroySession: vi.fn(),
  fetchWorkspaces: vi.fn(),
  switchSessionWorkspace: vi.fn(),
}));

import { fetchWorkspaces, getSession, switchSessionWorkspace } from './lib/authAccessClient';
import { useIsMobile } from './hooks/useIsMobile';

const mockGetSession = vi.mocked(getSession);
const mockFetchWorkspaces = vi.mocked(fetchWorkspaces);
const mockSwitchSessionWorkspace = vi.mocked(switchSessionWorkspace);
const mockUseIsMobile = vi.mocked(useIsMobile);

const baseSession = {
  session: { id: 'sess_1', expires_at: '2099-01-01T00:00:00Z' },
  key: {
    id: 'key_1',
    key_prefix: 'inf_alpha...',
    name: 'Joined Member',
    role: 'operator',
    principal_type: 'human',
    workspace_id: 'ws_alpha',
    workspace_slug: 'alpha-team',
    workspace_name: 'Alpha Team',
  },
  workspace: { id: 'ws_alpha', slug: 'alpha-team', name: 'Alpha Team' },
  member: { id: 'm_1', email: 'member@example.com', display_name: 'Joined Member' },
};

function renderApp() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <App />
    </MemoryRouter>,
  );
}

describe('App public routing', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetSession.mockResolvedValue(null);
  });

  it('uses distinct landing and sign-in routes when signed out', async () => {
    const { unmount } = renderApp();
    expect(await screen.findByText('PUBLIC LANDING PAGE')).toBeInTheDocument();
    unmount();

    render(
      <MemoryRouter initialEntries={['/sign-in']}>
        <App />
      </MemoryRouter>,
    );
    expect(await screen.findByText('SIGN IN PAGE')).toBeInTheDocument();
  });

  it('preserves public docs, quickstart, and invitation routes', async () => {
    const routes = [
      ['/docs', 'PUBLIC DOCS PAGE'],
      ['/getting-started', 'GETTING STARTED PAGE'],
      ['/accept-invite', 'ACCEPT INVITATION PAGE'],
    ] as const;

    for (const [path, expected] of routes) {
      const view = render(
        <MemoryRouter initialEntries={[path]}>
          <App />
        </MemoryRouter>,
      );
      expect(await screen.findByText(expected)).toBeInTheDocument();
      view.unmount();
    }
  });
});

describe('App workspace switcher', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.localStorage.clear();
    mockGetSession.mockResolvedValue(baseSession);
    mockUseIsMobile.mockReturnValue(false);
  });

  it('shows current workspace chip when only one workspace is available', async () => {
    mockFetchWorkspaces.mockResolvedValue([
      { id: 'ws_alpha', slug: 'alpha-team', name: 'Alpha Team', created_at: '2026-03-15T00:00:00Z', status: 'active' },
    ]);

    renderApp();

    await waitFor(() => {
      expect(screen.getByLabelText('Current workspace: Alpha Team')).toBeInTheDocument();
    });

    expect(screen.queryByLabelText('Switch workspace')).not.toBeInTheDocument();
  });

  it('switches workspace from the top nav when multiple workspaces are available', async () => {
    mockFetchWorkspaces.mockResolvedValue([
      { id: 'ws_alpha', slug: 'alpha-team', name: 'Alpha Team', created_at: '2026-03-15T00:00:00Z', status: 'active' },
      { id: 'ws_beta', slug: 'beta-team', name: 'Beta Team', created_at: '2026-03-15T00:00:00Z', status: 'active' },
    ]);
    mockSwitchSessionWorkspace.mockResolvedValue({
      ...baseSession,
      key: {
        ...baseSession.key,
        id: 'key_2',
        workspace_id: 'ws_beta',
        workspace_slug: 'beta-team',
        workspace_name: 'Beta Team',
      },
      workspace: { id: 'ws_beta', slug: 'beta-team', name: 'Beta Team' },
    });

    renderApp();

    const select = await screen.findByLabelText('Switch workspace');
    fireEvent.change(select, { target: { value: 'ws_beta' } });

    await waitFor(() => {
      expect(mockSwitchSessionWorkspace).toHaveBeenCalledWith('ws_beta');
    });
    await waitFor(() => {
      expect((screen.getByLabelText('Switch workspace') as HTMLSelectElement).value).toBe('ws_beta');
    });
    expect(window.localStorage.getItem('infera:last-workspace:member@example.com')).toBe('ws_beta');
  });

  it('keeps the active route stable while rehydrating the new workspace session', async () => {
    mockFetchWorkspaces.mockResolvedValue([
      { id: 'ws_alpha', slug: 'alpha-team', name: 'Alpha Team', created_at: '2026-03-15T00:00:00Z', status: 'active' },
      { id: 'ws_beta', slug: 'beta-team', name: 'Beta Team', created_at: '2026-03-15T00:00:00Z', status: 'active' },
    ]);
    mockSwitchSessionWorkspace.mockResolvedValue({
      ...baseSession,
      key: {
        ...baseSession.key,
        id: 'key_2',
        workspace_id: 'ws_beta',
        workspace_slug: 'beta-team',
        workspace_name: 'Beta Team',
      },
      workspace: { id: 'ws_beta', slug: 'beta-team', name: 'Beta Team' },
    });

    render(
      <MemoryRouter initialEntries={['/models']}>
        <App />
      </MemoryRouter>,
    );

    expect(await screen.findByText('MODELS PAGE')).toBeInTheDocument();

    const select = await screen.findByLabelText('Switch workspace');
    fireEvent.change(select, { target: { value: 'ws_beta' } });

    await waitFor(() => {
      expect(mockSwitchSessionWorkspace).toHaveBeenCalledWith('ws_beta');
    });
    await waitFor(() => {
      expect(screen.getByText('MODELS PAGE')).toBeInTheDocument();
    });
    expect((screen.getByLabelText('Switch workspace') as HTMLSelectElement).value).toBe('ws_beta');
  });

  it('opens and closes the mobile nav drawer', async () => {
    mockUseIsMobile.mockReturnValue(true);
    mockFetchWorkspaces.mockResolvedValue([
      { id: 'ws_alpha', slug: 'alpha-team', name: 'Alpha Team', created_at: '2026-03-15T00:00:00Z', status: 'active' },
    ]);

    renderApp();

    const openButton = await screen.findByLabelText('Open navigation');
    fireEvent.click(openButton);

    expect(screen.getByRole('link', { name: 'Models' })).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText('Close navigation'));

    await waitFor(() => {
      expect(screen.queryByRole('link', { name: 'Models' })).not.toBeInTheDocument();
    });
  });

  it('uses a single docs header when an authenticated user opens /docs', async () => {
    mockFetchWorkspaces.mockResolvedValue([
      { id: 'ws_alpha', slug: 'alpha-team', name: 'Alpha Team', created_at: '2026-03-15T00:00:00Z', status: 'active' },
    ]);

    render(
      <MemoryRouter initialEntries={['/docs']}>
        <App />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByText('PUBLIC DOCS PAGE')).toBeInTheDocument();
    });

    expect(screen.queryByText('INFERA')).not.toBeInTheDocument();
    expect(document.querySelectorAll('.top-nav').length).toBe(1);
  });
});
