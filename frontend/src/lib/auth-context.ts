import { createContext, useContext } from 'react';
import type { SessionInfo } from './api';

export const AuthContext = createContext<{
  session: SessionInfo | null;
  setSession: React.Dispatch<React.SetStateAction<SessionInfo | null>>;
  refreshSession: () => Promise<void>;
} | null>(null);

export function useAuthSession() {
  const context = useContext(AuthContext);
  if (!context) throw new Error('useAuthSession must be used within AuthContext');
  return context;
}
