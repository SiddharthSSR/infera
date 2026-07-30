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
  it('renders an honest unavailable state instead of synthetic log cards', () => {
    const { container } = render(
      <MemoryRouter>
        <Logs />
      </MemoryRouter>,
    );

    expect(container.querySelectorAll('.mobile-data-card')).toHaveLength(0);
    expect(container).toHaveTextContent('No runtime log source is connected');
    expect(container.textContent).not.toContain('Timestamp');
    expect(container.textContent).not.toContain('GPU utilization');
  });
});
