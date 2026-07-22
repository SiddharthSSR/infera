/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { Company } from './Company';
import { Security } from './Security';
import { Trust } from './Trust';

function renderPage(page: ReactNode) {
  return render(<MemoryRouter>{page}</MemoryRouter>);
}

describe('public trust surfaces', () => {
  it('publishes authoritative evidence and labels every missing trust source', () => {
    renderPage(<Trust />);

    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByRole('link', { name: 'TRUST' })).toHaveAttribute('href', '/trust');
    expect(screen.getByRole('link', { name: /Inspect repository/ })).toHaveAttribute(
      'href',
      'https://github.com/SiddharthSSR/infera',
    );
    expect(screen.getByText(/Python worker package declares Apache-2.0/i)).toBeInTheDocument();
    for (const link of screen.getAllByRole('link', { name: /Read decision record/ })) {
      expect(link).toHaveAttribute(
        'href',
        'https://github.com/SiddharthSSR/infera/blob/main/docs/trust/publication-readiness.md',
      );
    }
    expect(screen.getByText(/No authoritative public status page/i)).toBeInTheDocument();
    expect(screen.getByText(/No SECURITY file or dedicated private vulnerability-reporting channel/i)).toBeInTheDocument();
    expect(screen.getByText(/No approved legal company profile/i)).toBeInTheDocument();
    expect(screen.queryByText(/SOC 2 certified/i)).not.toBeInTheDocument();
  });

  it('keeps unknown company details unavailable and bounds the public issue channel', () => {
    renderPage(<Company />);

    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByText('Legal or trading identity')).toBeInTheDocument();
    expect(screen.getAllByText('Owner decision required')).toHaveLength(5);
    expect(screen.getByRole('link', { name: /Read the exact decision record/ })).toHaveAttribute(
      'href',
      'https://github.com/SiddharthSSR/infera/blob/main/docs/trust/publication-readiness.md',
    );
    expect(screen.getByText(/They are not a private contact channel/i)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Open GitHub issues/ })).toHaveAttribute(
      'href',
      'https://github.com/SiddharthSSR/infera/issues',
    );
  });

  it('distinguishes implementation evidence from unavailable security assurances', () => {
    renderPage(<Security />);

    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByRole('heading', { name: 'Inspect the controls. Keep the claims bounded.' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Review compatibility documentation/ })).toHaveAttribute(
      'href',
      'https://github.com/SiddharthSSR/infera/blob/main/docs/openai/COMPATIBILITY.md',
    );
    expect(screen.getByText(/authoritative evidence before publishing audit or certification claims/i)).toBeInTheDocument();
    expect(screen.getAllByText('Decision required')).toHaveLength(6);
    expect(screen.getByText(/Do not place vulnerability details/i)).toBeInTheDocument();
    expect(screen.queryByText(/guaranteed/i)).not.toBeInTheDocument();
  });

  it('keeps the migration quickstart reachable as the primary CTA on every trust surface', () => {
    for (const page of [<Trust key="trust" />, <Company key="company" />, <Security key="security" />]) {
      const view = renderPage(page);
      expect(screen.getAllByRole('link', { name: /quickstart/i }).some((link) => link.getAttribute('href') === '/getting-started')).toBe(true);
      view.unmount();
    }
  });
});
