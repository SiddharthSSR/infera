import type { ReactNode } from 'react';

export type TrustStatusTone = 'available' | 'unavailable';

export interface TrustStatusProps {
  children: ReactNode;
  tone: TrustStatusTone;
}

export function TrustStatus({ children, tone }: TrustStatusProps) {
  return <span className={`trust-status trust-status-${tone}`}>{children}</span>;
}
