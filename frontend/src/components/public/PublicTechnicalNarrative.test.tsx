/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { OperatorWorkflow } from './OperatorWorkflow';
import { TechnicalProof } from './TechnicalProof';

function renderNarrative() {
  return render(
    <MemoryRouter>
      <TechnicalProof />
      <OperatorWorkflow />
    </MemoryRouter>,
  );
}

describe('public technical narrative', () => {
  it('presents the request architecture in source-backed order', () => {
    renderNarrative();

    const flow = screen.getByRole('list', { name: 'Inference request data flow' });
    const stages = within(flow).getAllByRole('listitem');

    expect(stages).toHaveLength(4);
    expect(stages[0]).toHaveTextContent('OpenAI SDK or HTTP');
    expect(stages[1]).toHaveTextContent('Authenticate and route');
    expect(stages[2]).toHaveTextContent('Run the model');
    expect(stages[3]).toHaveTextContent('Keep the outcome inspectable');
    expect(screen.getByRole('link', { name: 'Read the compatibility contract', exact: true })).toHaveAttribute('href', '/docs');
    expect(screen.getByRole('link', { name: 'Read the compatibility contract', exact: true })).not.toHaveAttribute('target');
    expect(screen.getByRole('link', { name: 'Inspect the gateway request path (opens in a new tab)' })).toHaveAttribute(
      'href',
      expect.stringContaining('17d0e16233d6db13691e7f3c288d3d39d78eec37/go/internal/gateway/inference_service.go'),
    );
    expect(screen.getByRole('link', { name: 'Inspect the gateway request path (opens in a new tab)' })).toHaveAttribute('target', '_blank');
  });

  it('states the operator loop and the logs-to-audit boundary without invented proof', () => {
    renderNarrative();

    expect(screen.getByRole('heading', { name: 'Prepare. Serve. Test. Inspect.' })).toBeInTheDocument();
    expect(screen.getByText('Workspace · Nodes')).toBeInTheDocument();
    expect(screen.getByText('Models · Nodes')).toBeInTheDocument();
    expect(screen.getByText('Playground · API')).toBeInTheDocument();
    expect(screen.getByText('Logs · API Keys · Workspace')).toBeInTheDocument();
    expect(screen.getByText(/current Logs screen is an operator console, not the durable audit ledger/i)).toBeInTheDocument();
    expect(screen.queryByText(/customer|benchmark|uptime|faster/i)).not.toBeInTheDocument();
  });
});
