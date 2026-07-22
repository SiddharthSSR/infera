/// <reference types="vitest/globals" />
import { fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { Evaluation } from './Evaluation';

function renderEvaluation() {
  return render(
    <MemoryRouter>
      <Evaluation />
    </MemoryRouter>,
  );
}

describe('Evaluation', () => {
  it('provides a skip target, one page heading, and coherent evaluation navigation', () => {
    renderEvaluation();

    expect(screen.getByRole('link', { name: 'Skip to main content' })).toHaveAttribute('href', '#main-content');
    expect(screen.getByRole('main')).toHaveAttribute('id', 'main-content');
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByRole('heading', { name: 'Decide with the repository open.' })).toBeInTheDocument();
    expect(screen.getByRole('navigation', { name: 'On this page' })).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: 'OPEN QUICKSTART' })[0]).toHaveAttribute('href', '/getting-started');
  });

  it('uses a semantic comparison table and distinguishes external verification from Infera evidence', () => {
    renderEvaluation();

    const table = screen.getByRole('table', { name: /evaluation responsibilities/i });
    expect(within(table).getAllByRole('columnheader')).toHaveLength(5);
    expect(within(table).getAllByRole('rowheader')).toHaveLength(6);
    expect(within(table).getByRole('columnheader', { name: 'OpenAI API usage' })).toBeInTheDocument();
    expect(within(table).getByRole('columnheader', { name: 'Raw serving engine' })).toBeInTheDocument();
    expect(within(table).getByRole('columnheader', { name: 'Hosted inference API' })).toBeInTheDocument();
    expect(within(table).getByRole('columnheader', { name: 'Infera' })).toBeInTheDocument();
    expect(screen.getAllByText(/verify .* externally/i).length).toBeGreaterThan(0);
  });

  it('keeps the FAQ keyboard operable with native disclosures and repository evidence', () => {
    renderEvaluation();

    const selfHosted = screen.getByText('Is Infera self-hosted or managed?').closest('details');
    expect(selfHosted).not.toBeNull();
    expect(selfHosted).not.toHaveAttribute('open');

    fireEvent.click(within(selfHosted!).getByText('Is Infera self-hosted or managed?'));

    expect(selfHosted).toHaveAttribute('open');
    expect(within(selfHosted!).getByText(/documents self-hosted local and production Compose deployments/i)).toBeInTheDocument();
    expect(within(selfHosted!).getByRole('link', { name: /Repository evidence/ })).toHaveAttribute('target', '_blank');
  });

  it('states deployment, hardware, persistence, provider, and API limitations without unsupported promises', () => {
    renderEvaluation();

    expect(screen.getByText(/No Kubernetes deployment is documented/i)).toBeInTheDocument();
    expect(screen.getByText(/There is no repository-wide minimum GPU claim/i)).toBeInTheDocument();
    expect(screen.getByText(/SQLite is limited to one gateway replica/i)).toBeInTheDocument();
    expect(screen.getByText(/RunPod and Vast.ai provider adapters/i)).toBeInTheDocument();
    expect(screen.getByText(/Legacy completions and embeddings are current limitations/i)).toBeInTheDocument();
    expect(screen.getByText(/time boxes organize the evaluation; they are not setup-time or performance promises/i)).toBeInTheDocument();
  });
});
