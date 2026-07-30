/// <reference types="vitest/globals" />
import { createRef } from 'react';
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { DashboardLogsPanel } from './DashboardLogsPanel';

describe('DashboardLogsPanel', () => {
  it('reports the unavailable source without fabricating operational events', () => {
    render(
      <DashboardLogsPanel
        dashLogs={[]}
        dashLogsRef={createRef<HTMLDivElement>()}
        onOpenLogs={vi.fn()}
      />,
    );

    expect(screen.getByRole('status')).toHaveTextContent('No runtime log source is available');
    expect(screen.getByRole('status')).toHaveTextContent('will not synthesize operational events');
    expect(screen.queryByText(/GPU utilization/i)).not.toBeInTheDocument();
  });
});
