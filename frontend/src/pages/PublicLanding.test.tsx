/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { PublicLanding } from './PublicLanding';

const analyticsMocks = vi.hoisted(() => ({
  track: vi.fn(),
  trackFirst: vi.fn(),
}));

vi.mock('../lib/publicAnalytics', () => ({
  publicAnalytics: analyticsMocks,
}));

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
    vi.clearAllMocks();
    writeText.mockReset();
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    });
  });

  it('records the landing view, acquisition CTAs, and quickstart with bounded properties', () => {
    renderLanding();

    expect(analyticsMocks.track).toHaveBeenCalledWith('public_landing_view', {
      surface: 'migration_landing',
    });

    fireEvent.click(screen.getByRole('link', { name: 'Run the quickstart' }));
    fireEvent.click(screen.getAllByRole('link', { name: 'Request design-partner access' })[0]);
    fireEvent.click(screen.getAllByRole('link', { name: 'Request design-partner access' })[1]);

    expect(analyticsMocks.track).toHaveBeenCalledWith('public_primary_cta_clicked', {
      action: 'start_building',
      placement: 'hero',
    });
    expect(analyticsMocks.track).toHaveBeenCalledWith('public_primary_cta_clicked', {
      action: 'request_design_partner_access',
      placement: 'closing',
    });
    expect(analyticsMocks.track).toHaveBeenCalledWith('public_resource_opened', {
      resource: 'quickstart',
      source: 'landing',
    });
  });

  it('leads with a concise compatible-client promise and one dominant access path', () => {
    renderLanding();

    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByRole('heading', { name: 'Run open models. Keep your OpenAI client.' })).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: 'Request design-partner access' })[0]).toHaveAttribute('href', '/request-access');
    expect(screen.getByRole('link', { name: 'Run the quickstart' })).toHaveAttribute('href', '/getting-started');
    expect(screen.getByRole('link', { name: 'Explore registry models' })).toHaveAttribute('href', '#models');
    expect(screen.getByRole('link', { name: 'OPENAI MIGRATION' })).toHaveAttribute('href', '/#migration');
    expect(screen.getByRole('link', { name: 'SIGN IN' })).toHaveAttribute('href', '/sign-in');
    expect(screen.getByRole('link', { name: /GITHUB/ })).toHaveAttribute('href', 'https://github.com/SiddharthSSR/infera');
  });

  it('presents source-backed registry examples without implying live serving', () => {
    renderLanding();

    const modelSection = screen.getByRole('heading', { name: 'Put open models behind one endpoint.' }).closest('section');
    expect(modelSection).not.toBeNull();
    expect(within(modelSection as HTMLElement).getAllByRole('article')).toHaveLength(6);
    expect(screen.getByRole('heading', { name: 'Mistral 7B Instruct v0.3' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Llama 3.1 8B Instruct' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Qwen2.5 7B Instruct' })).toBeInTheDocument();
    expect(screen.getByText('A registry entry does not mean serving.')).toBeInTheDocument();
    expect(screen.getByText(/live serving still requires a healthy worker/i)).toBeInTheDocument();
  });

  it('shows the source-backed operator walkthrough and factual API boundary', () => {
    renderLanding();

    const walkthrough = screen.getByRole('heading', { name: 'Follow one request from model to evidence.' }).closest('section');
    expect(walkthrough).not.toBeNull();
    expect(within(walkthrough as HTMLElement).getAllByRole('listitem')).toHaveLength(4);
    expect(within(walkthrough as HTMLElement).getByText('Models / discovery')).toBeInTheDocument();
    expect(within(walkthrough as HTMLElement).getByText('Nodes / readiness')).toBeInTheDocument();
    expect(within(walkthrough as HTMLElement).getByText('Playground / request')).toBeInTheDocument();
    expect(within(walkthrough as HTMLElement).getByText('Logs / inspection')).toBeInTheDocument();
    expect(within(walkthrough as HTMLElement).getByRole('heading', { name: 'OpenAI migration' })).toBeInTheDocument();
    expect(within(walkthrough as HTMLElement).getByRole('heading', { name: 'Self-hosted operations' })).toBeInTheDocument();
    expect(within(walkthrough as HTMLElement).getByRole('heading', { name: 'Failure diagnosis' })).toBeInTheDocument();
    expect(screen.getByText('Error types are Infera-specific.')).toBeInTheDocument();
    expect(screen.getByText('No legacy completions or embeddings endpoint.')).toBeInTheDocument();
  });

  it('keeps model proof before the product walkthrough', () => {
    const { container } = renderLanding();
    const sectionIDs = Array.from(container.querySelectorAll('main > section[id]')).map((section) => section.id);

    expect(sectionIDs).toEqual([
      'models',
      'product',
      'proof',
    ]);
  });

  it('links every walkthrough step to a focused quickstart or docs surface', () => {
    renderLanding();

    expect(screen.getByRole('link', { name: 'Run model discovery →' })).toHaveAttribute('href', '/getting-started#copy-run');
    expect(screen.getByRole('link', { name: 'Review readiness checks →' })).toHaveAttribute('href', '/getting-started#failures');
    expect(screen.getByRole('link', { name: 'Read the request contract →' })).toHaveAttribute('href', '/docs#quickstart');
    expect(screen.getByRole('link', { name: 'Open the failure runbook →' })).toHaveAttribute('href', '/getting-started#failures');
  });

  it('keeps the quickstart available while linking to the API and trust records', () => {
    renderLanding();

    expect(screen.getByRole('heading', { name: 'Small surface. Clear limits.' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Inspect the trust record →' })).toHaveAttribute('href', '/trust');
    expect(screen.getByRole('link', { name: 'Read the API contract →' })).toHaveAttribute('href', '/docs');
    expect(screen.getAllByRole('link', { name: /quickstart/i }).length).toBeGreaterThanOrEqual(2);
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
