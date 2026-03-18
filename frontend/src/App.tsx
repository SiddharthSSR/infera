import { useState, useEffect, useCallback, Suspense, useMemo, useRef } from 'react';
import { Routes, Route, NavLink, useLocation, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import { toast } from 'sonner';
import { cn } from './lib/utils';
import { destroySession, fetchWorkspaces, getSession, switchSessionWorkspace, type SessionInfo, type WorkspaceRecord } from './lib/api';
import { AuthContext, useAuthSession } from './lib/auth-context';
import { ErrorBoundary } from './components/ErrorBoundary';
import { ChatContext, type ChatContextType, type Message, type PlaygroundHistoryEntry } from './lib/chat-context';
import { lazyWithRetry } from './lib/lazyWithRetry';
import { useIsMobile } from './hooks/useIsMobile';

import { Dashboard } from './pages/Dashboard';
import { Playground } from './pages/Playground';
import { Instances } from './pages/Instances';
import { Logs } from './pages/Logs';
import { Models } from './pages/Models';
import { ApiKeys } from './pages/ApiKeys';
import { WorkspaceAdmin } from './pages/WorkspaceAdmin';

const Login = lazyWithRetry(() => import('./pages/Login').then((module) => ({ default: module.Login })), 'login');
const PublicApiDocs = lazyWithRetry(() => import('./pages/PublicApiDocs').then((module) => ({ default: module.PublicApiDocs })), 'docs');
const GettingStarted = lazyWithRetry(() => import('./pages/GettingStarted').then((module) => ({ default: module.GettingStarted })), 'getting-started');
const AcceptInvitation = lazyWithRetry(() => import('./pages/AcceptInvitation').then((module) => ({ default: module.AcceptInvitation })), 'accept-invite');
const workspacePreferenceKeyPrefix = 'infera:last-workspace:';

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

// Navigation items — primary (always visible) and secondary (utility)
const primaryNavItems = [
  { path: '/', label: 'DASHBOARD' },
  { path: '/models', label: 'MODELS' },
  { path: '/instances', label: 'NODES' },
  { path: '/playground', label: 'PLAYGROUND' },
];

const secondaryNavItems = [
  { path: '/logs', label: 'LOGS' },
  { path: '/api-keys', label: 'API KEYS' },
  { path: '/workspace', label: 'SETTINGS' },
];

// Page display titles
const pageTitles: Record<string, string> = {
  '/': 'INFERENCE',
  '/models': 'MODELS',
  '/instances': 'NODES',
  '/playground': 'PLAYGROUND',
  '/logs': 'LOGS',
  '/api-keys': 'API KEYS',
  '/workspace': 'SETTINGS',
};

// Top Navigation
function TopNav({ onLogout }: { onLogout: () => void }) {
  const location = useLocation();
  const isMobile = useIsMobile(900);
  const { session, availableWorkspaces, switchWorkspace, switchingWorkspace } = useAuthSession();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const workspaceNavLabel = session?.workspace?.slug
    ? session.workspace.slug.replace(/[-_]+/g, ' ').toUpperCase()
    : session?.workspace?.name?.toUpperCase();
  const switchableWorkspaces = availableWorkspaces.length > 1;

  useEffect(() => {
    setDrawerOpen(false);
  }, [location.pathname]);

  const navLinks = (
    <>
      <div className="nav-section">
        <div className="nav-section-label">Primary</div>
        <div className="nav-section-links">
          {primaryNavItems.map((item) => (
            <NavLink
              key={item.path}
              to={item.path}
              end={item.path === '/'}
              className={({ isActive }) => cn('nav-link', isActive && 'active')}
            >
              {item.label}
            </NavLink>
          ))}
        </div>
      </div>
      <div className="nav-section">
        <div className="nav-section-label">Workspace</div>
        <div className="nav-section-links secondary">
          {secondaryNavItems.map((item) => (
            <NavLink
              key={item.path}
              to={item.path}
              end={item.path === '/'}
              className={({ isActive }) => cn('nav-link nav-link-secondary', isActive && 'active')}
            >
              {item.label}
            </NavLink>
          ))}
        </div>
      </div>
    </>
  );

  const workspaceUtility = (
    <>
      {switchableWorkspaces && session?.workspace?.id ? (
        <label className="nav-workspace-switcher">
          <span className="sr-only">Switch workspace</span>
          <select
            aria-label="Switch workspace"
            className="nav-workspace-select"
            value={session.workspace.id}
            onChange={(event) => void switchWorkspace(event.target.value)}
            disabled={switchingWorkspace}
          >
            {availableWorkspaces.map((workspace) => (
              <option key={workspace.id} value={workspace.id}>
                {workspace.name}
              </option>
            ))}
          </select>
        </label>
      ) : session?.workspace?.name && workspaceNavLabel && (
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
    </>
  );

  if (isMobile) {
    return (
      <nav className="top-nav top-nav-mobile">
        <div className="top-nav-mobile-bar">
          <div className="nav-brand-block">
            <div style={{ fontWeight: 700, letterSpacing: '-0.02em' }}>INFERA.AI</div>
            <div className="nav-mobile-route-label">
              {(pageTitles[location.pathname] || 'INFERA').replace(/_/g, ' ')}
            </div>
          </div>
          <div className="nav-mobile-utility">
            <div className="nav-group nav-auth-group nav-auth-group-mobile">
              {workspaceUtility}
            </div>
            <button
              className="nav-icon-button nav-menu-button"
              type="button"
              aria-expanded={drawerOpen}
              aria-controls="mobile-nav-drawer"
              aria-label={drawerOpen ? 'Close navigation' : 'Open navigation'}
              onClick={() => setDrawerOpen((value) => !value)}
            >
              {drawerOpen ? '×' : '☰'}
            </button>
          </div>
        </div>
        {drawerOpen ? (
          <div id="mobile-nav-drawer" className="mobile-nav-drawer">
            {navLinks}
          </div>
        ) : null}
      </nav>
    );
  }

  return (
    <nav className="top-nav">
      <div style={{ fontWeight: 700, letterSpacing: '-0.02em' }}>INFERA.AI</div>
      <div className="nav-group nav-links-group">
        {primaryNavItems.map((item, i) => (
          <span key={item.path} className="contents">
            {i > 0 && <span className="nav-diamond">&#9671;</span>}
            <NavLink
              to={item.path}
              end={item.path === '/'}
              className={({ isActive }) => cn('nav-link', isActive && 'active')}
            >
              {item.label}
            </NavLink>
          </span>
        ))}
        <span className="nav-group-divider" aria-hidden="true">|</span>
        <span className="nav-secondary-group">
          {secondaryNavItems.map((item, i) => (
            <span key={item.path} className="contents">
              {i > 0 && <span className="nav-sep">·</span>}
              <NavLink
                to={item.path}
                end={item.path === '/'}
                className={({ isActive }) => cn('nav-link nav-link-secondary', isActive && 'active')}
              >
                {item.label}
              </NavLink>
            </span>
          ))}
        </span>
      </div>
      <div className="nav-group nav-auth-group" style={{ gap: '1rem' }}>
        {workspaceUtility}
      </div>
    </nav>
  );
}

function RouteLoader() {
  return (
    <div style={{
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      minHeight: '40vh',
      color: 'var(--text-secondary)',
      fontFamily: 'var(--font-main)',
      fontSize: '0.8rem',
      letterSpacing: '0.08em',
    }}>
      LOADING VIEW...
    </div>
  );
}

// Main App Content
function AppContent() {
  const location = useLocation();
  const [session, setSession] = useState<SessionInfo | null>(null);
  const [loadingSession, setLoadingSession] = useState(true);
  const [availableWorkspaces, setAvailableWorkspaces] = useState<WorkspaceRecord[]>([]);
  const [switchingWorkspace, setSwitchingWorkspace] = useState(false);
  const autoRestoreAttemptRef = useRef<string | null>(null);

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
      if (!nextSession) {
        setAvailableWorkspaces([]);
      }
    } finally {
      setLoadingSession(false);
    }
  }, []);

  useEffect(() => {
    setLoadingSession(true);
    getSession()
      .then((nextSession) => {
        setSession(nextSession);
        if (!nextSession) {
          setAvailableWorkspaces([]);
        }
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
    setAvailableWorkspaces([]);
    queryClient.clear();
  }, [setMessages, setHistory, setSelectedModel, setTemperature, setMaxTokens]);

  const workspacePreferenceKey = useMemo(() => {
    const email = session?.member?.email?.trim().toLowerCase();
    if (!email) return null;
    return `${workspacePreferenceKeyPrefix}${email}`;
  }, [session?.member?.email]);

  const handleWorkspaceSwitch = useCallback(async (workspaceId: string) => {
    if (!session?.workspace?.id || workspaceId === session.workspace.id) {
      return;
    }

    setSwitchingWorkspace(true);
    try {
      const nextSession = await switchSessionWorkspace(workspaceId);
      if (workspacePreferenceKey) {
        window.localStorage.setItem(workspacePreferenceKey, workspaceId);
      }
      setMessages([]);
      setHistory([]);
      setSelectedModel('');
      setTemperature(0.7);
      setMaxTokens(2048);
      setSession(nextSession);
      queryClient.clear();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to switch workspace';
      toast.error(message);
    } finally {
      setSwitchingWorkspace(false);
    }
  }, [session?.workspace?.id, workspacePreferenceKey, setHistory, setMaxTokens, setMessages, setSelectedModel, setTemperature]);

  useEffect(() => {
    if (!session) {
      return;
    }

    let active = true;
    fetchWorkspaces()
      .then((workspaces) => {
        if (!active) return;
        setAvailableWorkspaces(workspaces);
      })
      .catch(() => {
        if (!active) return;
        setAvailableWorkspaces(session.workspace ? [{
          id: session.workspace.id,
          slug: session.workspace.slug,
          name: session.workspace.name,
          created_at: '',
          status: 'active',
        }] : []);
      });

    return () => {
      active = false;
    };
  }, [session?.key?.id, session?.workspace?.id, session?.workspace?.name, session?.workspace?.slug]);

  useEffect(() => {
    if (!session?.session?.id || !session.workspace?.id || !workspacePreferenceKey) {
      autoRestoreAttemptRef.current = null;
      return;
    }
    if (availableWorkspaces.length < 2 || switchingWorkspace) {
      return;
    }
    if (autoRestoreAttemptRef.current === session.session.id) {
      return;
    }

    autoRestoreAttemptRef.current = session.session.id;
    const preferredWorkspaceId = window.localStorage.getItem(workspacePreferenceKey);
    if (!preferredWorkspaceId || preferredWorkspaceId === session.workspace.id) {
      return;
    }
    if (!availableWorkspaces.some((workspace) => workspace.id === preferredWorkspaceId)) {
      return;
    }
    void handleWorkspaceSwitch(preferredWorkspaceId);
  }, [availableWorkspaces, handleWorkspaceSwitch, session?.session?.id, session?.workspace?.id, switchingWorkspace, workspacePreferenceKey]);

  useEffect(() => {
    if (workspacePreferenceKey && session?.workspace?.id) {
      window.localStorage.setItem(workspacePreferenceKey, session.workspace.id);
    }
  }, [session?.workspace?.id, workspacePreferenceKey]);

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
      <Suspense fallback={<RouteLoader />}>
        <Routes>
          <Route path="/docs" element={<PublicApiDocs />} />
          <Route path="/getting-started" element={<GettingStarted />} />
          <Route path="/accept-invite" element={<AcceptInvitation onAccepted={(nextSession: SessionInfo) => setSession(nextSession)} />} />
          <Route path="*" element={<Login onAuthenticated={(nextSession: SessionInfo) => setSession(nextSession)} />} />
        </Routes>
      </Suspense>
    );
  }

  const pageTitle = pageTitles[location.pathname] || 'INFERA';
  const docsRoutes = ['/docs', '/getting-started'];
  const hideAppChrome = docsRoutes.includes(location.pathname);
  const hideDisplayHeader = hideAppChrome || location.pathname === '/playground';

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
    <AuthContext.Provider
      value={{
        session,
        setSession,
        refreshSession,
        availableWorkspaces,
        switchWorkspace: handleWorkspaceSwitch,
        switchingWorkspace,
      }}
    >
    <ChatContext.Provider value={chatContextValue}>
      <div className="app-shell app-shell-auth">
        {!hideAppChrome && <TopNav onLogout={handleLogout} />}
        {!hideDisplayHeader && (
          <header className="display-text">{pageTitle}</header>
        )}
        <Suspense fallback={<RouteLoader />}>
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
            <Route path="/accept-invite" element={<AcceptInvitation onAccepted={(nextSession: SessionInfo) => setSession(nextSession)} />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Suspense>
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
