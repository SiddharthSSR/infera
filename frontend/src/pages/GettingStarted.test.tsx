/// <reference types="vitest/globals" />
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { GettingStarted } from './GettingStarted';

describe('GettingStarted', () => {
  it('provides one named main landmark with a skip target and page heading', () => {
    render(
      <MemoryRouter>
        <GettingStarted />
      </MemoryRouter>,
    );

    expect(screen.getByRole('link', { name: 'Skip to main content' })).toHaveAttribute('href', '#main-content');
    expect(screen.getByRole('main')).toHaveAttribute('id', 'main-content');
    expect(screen.getByRole('heading', { level: 1, name: 'From API key to first model response.' })).toBeInTheDocument();
    expect(screen.getByRole('navigation', { name: 'On this page' })).toBeInTheDocument();
  });

  it('distinguishes machine credentials from human dashboard sessions', () => {
    render(
      <MemoryRouter>
        <GettingStarted />
      </MemoryRouter>,
    );

    expect(screen.getByText('Machine request')).toBeInTheDocument();
    expect(screen.getByText('Human dashboard session')).toBeInTheDocument();
    expect(screen.getByText(/Do not put a human dashboard key into unattended production code/i)).toBeInTheDocument();
  });

  it('provides recovery and first authenticated actions as keyboard-accessible links', () => {
    render(
      <MemoryRouter>
        <GettingStarted />
      </MemoryRouter>,
    );

    const recoveryCard = screen.getByText('NO KEY OR WRONG WORKSPACE?').closest('.docs-card');

    expect(recoveryCard).not.toBeNull();
    expect(screen.getByRole('link', { name: 'SIGN IN TO DASHBOARD' })).toHaveAttribute('href', '/sign-in');
    expect(within(recoveryCard!).getByRole('link', { name: 'SIGN IN' })).toHaveAttribute('href', '/sign-in');
    expect(within(recoveryCard!).getByRole('link', { name: 'ACCEPT AN INVITATION' })).toHaveAttribute('href', '/accept-invite');
    expect(screen.getByText(/first setup action is to connect provider access in Workspace/i)).toBeInTheDocument();
  });
});
