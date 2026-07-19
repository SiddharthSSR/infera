/// <reference types="vitest/globals" />
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { PublicApiDocs } from './PublicApiDocs';

describe('PublicApiDocs', () => {
  it('provides one named main landmark with a skip target and page heading', () => {
    render(
      <MemoryRouter>
        <PublicApiDocs />
      </MemoryRouter>,
    );

    expect(screen.getByRole('link', { name: 'Skip to main content' })).toHaveAttribute('href', '#main-content');
    expect(screen.getByRole('main')).toHaveAttribute('id', 'main-content');
    expect(screen.getByRole('heading', { level: 1, name: 'Build against Infera without rewriting your client.' })).toBeInTheDocument();
    expect(screen.getByRole('navigation', { name: 'On this page' })).toBeInTheDocument();
  });
});
