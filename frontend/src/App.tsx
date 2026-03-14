import { useState, useEffect, useCallback, createContext, useContext } from 'react';
import { Routes, Route, NavLink, useLocation, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import { cn } from './lib/utils';
import { Dashboard } from './pages/Dashboard';
import { Playground } from './pages/Playground';
import { Instances } from './pages/Instances';
import { Logs } from './pages/Logs';
import { Models } from './pages/Models';
import { ApiKeys } from './pages/ApiKeys';
import { WorkspaceAdmin } from './pages/WorkspaceAdmin';
import { Login } from './pages/Login';
import { PublicApiDocs } from './pages/PublicApiDocs';
import { GettingStarted } from './pages/GettingStarted';
import { AcceptInvitation } from './pages/AcceptInvitation';
import { getSession, destroySession, type SessionInfo } from './lib/api';
import { AuthContext, useAuthSession } from './lib/auth-context';
import { ErrorBoundary } from './components/ErrorBoundary';
import type { ChatMessage } from './types';

// Chat message with metadata
interface Message extends ChatMessage {
  id: string;
  timestamp: Date;
}

interface PlaygroundHistoryEntry {
  id: string;
  time: string;
  latencyMs: number;
  preview: string;
  promptTokens?: number;
  completionTokens?: number;
}

// Chat Context - to persist chat state across page switches
interface ChatContextType {
  messages: Message[];
  setMessages: React.Dispatch<React.SetStateAction<Message[]>>;
  history: PlaygroundHistoryEntry[];
  setHistory: React.Dispatch<React.SetStateAction<PlaygroundHistoryEntry[]>>;
  selectedModel: string;
  setSelectedModel: (model: string) => void;
  temperature: number;
  setTemperature: (temp: number) => void;
  maxTokens: number;
  setMaxTokens: (tokens: number) => void;
}

const ChatContext = createContext<ChatContextType | null>(null);

export const useChat = () => {
  const context = useContext(ChatContext);
  if (!context) throw new Error('useChat must be used within ChatProvider');
  return context;
};

// Query Client
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 2000,
      refetchInterval: 5000,
    },
  },
});

// Navigation items
const navItems = [
  { path: '/', label: 'DASHBOARD' },
  { path: '/models', label: 'MODELS' },
  { path: '/instances', label: 'CLUSTERS' },
  { path: '/playground', label: 'PLAYGROUND' },
  { path: '/logs', label: 'LOGS' },
  { path: '/api-keys', label: 'API KEYS' },
  { path: '/workspace', label: 'WORKSPACE' },
];

// Page display titles
const pageTitles: Record<string, string> = {
  '/': 'INFERENCE',
  '/models': 'MODELS',
  '/instances': 'INSTANCES',
  '/playground': 'PLAYGROUND',
  '/logs': 'SYSTEM LOGS',
  '/api-keys': 'API KEYS',
  '/workspace': 'WORKSPACE',
};

// Top Navigation
function TopNav({ onLogout }: { onLogout: () => void }) {
  const { session } = useAuthSession();
  const workspaceNavLabel = session?.workspace?.slug
    ? session.workspace.slug.replace(/[-_]+/g, ' ').toUpperCase()
    : session?.workspace?.name?.toUpperCase();

  return (
    <nav className="top-nav">
      <div style={{ fontWeight: 700, letterSpacing: '-0.02em' }}>INFERA.AI</div>
      <div className="nav-group nav-links-group">
        {navItems.map((item, i) => (
          <span key={item.path} className="contents">
            {i > 0 && <span className="nav-diamond">&#9671;</span>}
            <NavLink
              to={item.path}
              end={item.path === '/'}
              className={({ isActive }) =>
                cn('nav-link', isActive && 'active')
              }
            >
              {item.label}
            </NavLink>
          </span>
        ))}
      </div>
      <div className="nav-group nav-auth-group" style={{ gap: '1rem' }}>
        {session?.workspace?.name && workspaceNavLabel && (
          <span
            className="nav-workspace-chip"
            title={session.workspace.name}
            aria-label={`Current workspace: ${session.workspace.name}`}
          >
            {workspaceNavLabel}
          </span>
        )}
        <button
          className="nav-icon-button"
          onClick={onLogout}
          type="button"
          title="Log out"
          aria-label="Log out"
        >
          ⏻
        </button>
      </div>
    </nav>
  );
}

// Main App Content
function AppContent() {
  const location = useLocation();
  const [session, setSession] = useState<SessionInfo | null>(null);
  const [loadingSession, setLoadingSession] = useState(true);

  // Chat state - persisted across page switches
  const [messages, setMessages] = useState<Message[]>([]);
  const [history, setHistory] = useState<PlaygroundHistoryEntry[]>([]);
  const [selectedModel, setSelectedModel] = useState<string>('');
  const [temperature, setTemperature] = useState(0.7);
  const [maxTokens, setMaxTokens] = useState(2048);

  const refreshSession = useCallback(async () => {
    try {
      const nextSession = await getSession();
      setSession(nextSession);
    } finally {
      setLoadingSession(false);
    }
  }, []);

  useEffect(() => {
    setLoadingSession(true);
    getSession()
      .then((nextSession) => {
        setSession(nextSession);
      })
      .catch(() => {
        setSession(null);
      })
      .finally(() => {
        setLoadingSession(false);
      });
  }, []);

  const handleLogout = useCallback(() => {
    setMessages([]);
    setHistory([]);
    setSelectedModel('');
    setTemperature(0.7);
    setMaxTokens(2048);
    destroySession();
    setSession(null);
    queryClient.clear();
  }, [setMessages, setHistory, setSelectedModel, setTemperature, setMaxTokens]);

  // Listen for auth-expired events from api.ts
  useEffect(() => {
    const handler = () => handleLogout();
    window.addEventListener('auth-expired', handler);
    return () => window.removeEventListener('auth-expired', handler);
  }, [handleLogout]);

  if (loadingSession) {
    return (
      <div style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        minHeight: '100vh',
        color: 'var(--text-secondary)',
        fontFamily: 'var(--font-main)',
        fontSize: '0.85rem',
        letterSpacing: '0.05em',
      }}>
        LOADING...
      </div>
    );
  }

  if (!session) {
    return (
      <Routes>
        <Route path="/docs" element={<PublicApiDocs />} />
        <Route path="/getting-started" element={<GettingStarted />} />
        <Route path="/accept-invite" element={<AcceptInvitation onAccepted={(nextSession) => setSession(nextSession)} />} />
        <Route path="*" element={<Login onAuthenticated={(nextSession) => setSession(nextSession)} />} />
      </Routes>
    );
  }

  const pageTitle = pageTitles[location.pathname] || 'INFERA';

  const chatContextValue: ChatContextType = {
    messages,
    setMessages,
    history,
    setHistory,
    selectedModel,
    setSelectedModel,
    temperature,
    setTemperature,
    maxTokens,
    setMaxTokens,
  };

  return (
    <AuthContext.Provider value={{ session, setSession, refreshSession }}>
    <ChatContext.Provider value={chatContextValue}>
      <div className="app-shell">
        <TopNav onLogout={handleLogout} />
        {/* Display header - skip for playground which has its own layout */}
        {location.pathname !== '/playground' && (
          <header className="display-text">{pageTitle}</header>
        )}
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/playground" element={<Playground />} />
          <Route path="/models" element={<Models />} />
          <Route path="/instances" element={<Instances />} />
          <Route path="/logs" element={<Logs />} />
          <Route path="/api-keys" element={<ApiKeys />} />
          <Route path="/workspace" element={<WorkspaceAdmin />} />
          <Route path="/docs" element={<PublicApiDocs />} />
          <Route path="/getting-started" element={<GettingStarted />} />
          <Route path="/accept-invite" element={<Navigate to="/workspace" replace />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </div>
    </ChatContext.Provider>
    </AuthContext.Provider>
  );
}

// Root App with Providers
function App() {
  return (
    <ErrorBoundary>
    <QueryClientProvider client={queryClient}>
      <AppContent />
      <Toaster
        position="bottom-right"
        toastOptions={{
          style: {
            background: 'var(--bg-paper)',
            border: '1px solid var(--border-color)',
            color: 'var(--text-primary)',
            fontFamily: 'var(--font-main)',
          },
        }}
      />
    </QueryClientProvider>
    </ErrorBoundary>
  );
}

export default App;
