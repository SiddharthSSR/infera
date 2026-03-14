/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Logs } from './Logs';

vi.mock('../hooks/useIsMobile', () => ({
  useIsMobile: () => true,
}));

describe('Logs mobile layout', () => {
  it('renders mobile log feed cards on Logs page', () => {
    const { container } = render(
      <MemoryRouter>
        <Logs />
      </MemoryRouter>,
    );

    expect(container.querySelectorAll('.mobile-data-card').length).toBeGreaterThan(0);
    expect(container.textContent).not.toContain('Timestamp');
  });
});
