/// <reference types="vitest/globals" />
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { CodeExample } from './CodeExample';

describe('CodeExample', () => {
  const writeText = vi.fn();

  beforeEach(() => {
    writeText.mockReset();
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    });
  });

  it('announces successful copy feedback', async () => {
    writeText.mockResolvedValue(undefined);
    render(<CodeExample code="curl /v1/models" language="shell" />);

    fireEvent.click(screen.getByRole('button', { name: 'COPY' }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith('curl /v1/models');
      expect(screen.getByRole('button', { name: 'COPIED' })).toHaveAttribute('data-copy-state', 'success');
      expect(screen.getByRole('status')).toHaveTextContent('Copied to clipboard.');
    });
  });

  it('announces copy failures with a manual recovery path', async () => {
    writeText.mockRejectedValue(new Error('clipboard unavailable'));
    render(<CodeExample code="curl /v1/models" language="shell" />);

    fireEvent.click(screen.getByRole('button', { name: 'COPY' }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'TRY AGAIN' })).toHaveAttribute('data-copy-state', 'error');
      expect(screen.getByRole('status')).toHaveTextContent('Copy failed. Select the code to copy it manually.');
    });
  });
});
