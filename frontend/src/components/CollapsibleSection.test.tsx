/// <reference types="vitest/globals" />
/// <reference types="@testing-library/jest-dom" />
import React from 'react';
import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { CollapsibleSection } from './CollapsibleSection';

describe('CollapsibleSection', () => {
  it('starts collapsed and expands on toggle', () => {
    render(
      <CollapsibleSection title="DEPLOYMENT HISTORY" description="Recent attempts">
        <div>Expanded body</div>
      </CollapsibleSection>,
    );

    const toggle = screen.getByRole('button', { name: /deployment history/i });

    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByText('Expanded body')).not.toBeInTheDocument();

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('Expanded body')).toBeInTheDocument();
  });
});
