/// <reference types="vitest/globals" />
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { GettingStarted } from './GettingStarted';

describe('GettingStarted', () => {
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

    expect(screen.getByRole('link', { name: 'SIGN IN' })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: 'ACCEPT AN INVITATION' })).toHaveAttribute('href', '/accept-invite');
    expect(screen.getByText(/first setup action is to connect provider access in Workspace/i)).toBeInTheDocument();
  });
});
