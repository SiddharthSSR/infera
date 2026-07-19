/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { PublicLanding } from './PublicLanding';

function renderLanding() {
  return render(
    <MemoryRouter>
      <PublicLanding />
    </MemoryRouter>,
  );
}

describe('PublicLanding', () => {
  const writeText = vi.fn();

  beforeEach(() => {
    writeText.mockReset();
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    });
  });

  it('separates product evaluation from sign in with one dominant migration path', () => {
    renderLanding();

    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByRole('heading', { name: 'Run open models behind the client you already ship.' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Run the migration quickstart' })).toHaveAttribute('href', '/getting-started');
    expect(screen.getByRole('link', { name: 'SIGN IN' })).toHaveAttribute('href', '/sign-in');
    expect(screen.getByRole('link', { name: /GITHUB/ })).toHaveAttribute('href', 'https://github.com/SiddharthSSR/infera');
  });

  it('exposes the complete migration sequence and factual API boundary', () => {
    renderLanding();

    const migrationSection = screen.getByRole('heading', { name: 'First response before first surprise.' }).closest('section');
    expect(migrationSection).not.toBeNull();
    expect(within(migrationSection as HTMLElement).getAllByRole('listitem')).toHaveLength(4);
    expect(screen.getByText('Confirm auth')).toBeInTheDocument();
    expect(screen.getByText('List live models')).toBeInTheDocument();
    expect(screen.getByText('Send one chat')).toBeInTheDocument();
    expect(screen.getByText('Promote to stream')).toBeInTheDocument();
    expect(screen.getByText('Error types are Infera-specific.')).toBeInTheDocument();
    expect(screen.getByText(/legacy completions and embeddings are not currently exposed/i)).toBeInTheDocument();
    expect(screen.getByText('Base URL, workspace credential, and model ID from live discovery')).toBeInTheDocument();
  });

  it('keeps the migration-first reading order before technical proof and operator workflow', () => {
    const { container } = renderLanding();
    const sectionIDs = Array.from(container.querySelectorAll('main > section[id]')).map((section) => section.id);

    expect(sectionIDs).toEqual([
      'migration',
      'architecture',
      'operator-loop',
      'product',
      'proof',
      'trust',
    ]);
  });

  it('keeps the migration CTA primary while exposing the bounded trust record', () => {
    renderLanding();

    expect(screen.getByRole('heading', { name: 'Public evidence, with the gaps left visible.' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open the trust record →' })).toHaveAttribute('href', '/trust');
    expect(screen.getByText('The repository has no license file or SECURITY file. The site does not infer rights or assurances from README copy.')).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: /quickstart/i }).length).toBeGreaterThanOrEqual(3);
  });

  it('copies the migration example and announces success', async () => {
    renderLanding();

    fireEvent.click(screen.getByRole('button', { name: 'Copy' }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(expect.stringContaining('from openai import OpenAI'));
      expect(screen.getByRole('status')).toHaveTextContent('Copied to clipboard.');
    });
  });

  it('provides a keyboard-operable mobile navigation disclosure', () => {
    renderLanding();

    const menuButton = screen.getByRole('button', { name: 'MENU' });
    expect(menuButton).toHaveAttribute('aria-expanded', 'false');

    fireEvent.click(menuButton);

    expect(screen.getByRole('button', { name: 'CLOSE' })).toHaveAttribute('aria-expanded', 'true');
    expect(document.getElementById('public-navigation')).toHaveClass('is-open');
  });
});
