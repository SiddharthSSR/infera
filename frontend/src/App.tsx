import { useState, useEffect, useCallback, Suspense, useMemo, useRef } from 'react';
import { Routes, Route, NavLink, useLocation, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import { toast } from 'sonner';
import { cn } from './lib/utils';
import { destroySession, fetchWorkspaces, getSession, switchSessionWorkspace } from './lib/authAccessClient';
import { AuthContext, useAuthSession } from './lib/auth-context';
import { ErrorBoundary } from './components/ErrorBoundary';
import { ChatContext, type ChatContextType, type Message, type PlaygroundHistoryEntry } from './lib/chat-context';
import { lazyWithRetry } from './lib/lazyWithRetry';
import { useIsMobile } from './hooks/useIsMobile';
import type { AgentAnalysisDepth, AgentExecutionMode, PlaygroundMode, SessionInfo, WorkspaceRecord } from './types';
import { AppShell, PageHeader } from './components/shared';
import { CommandPalette } from './components/CommandPalette';
import { PageTransition } from './components/PageTransition';

const Login = lazyWithRetry(() => import('./pages/Login').then((module) => ({ default: module.Login })), 'login');
const PublicLanding = lazyWithRetry(() => import('./pages/PublicLanding').then((module) => ({ default: module.PublicLanding })), 'public-landing');
const PublicApiDocs = lazyWithRetry(() => import('./pages/PublicApiDocs').then((module) => ({ default: module.PublicApiDocs })), 'docs');
const Evaluation = lazyWithRetry(() => import('./pages/Evaluation').then((module) => ({ default: module.Evaluation })), 'evaluation');
const GettingStarted = lazyWithRetry(() => import('./pages/GettingStarted').then((module) => ({ default: module.GettingStarted })), 'getting-started');
const Trust = lazyWithRetry(() => import('./pages/Trust').then((module) => ({ default: module.Trust })), 'trust');
const Company = lazyWithRetry(() => import('./pages/Company').then((module) => ({ default: module.Company })), 'company');
const Security = lazyWithRetry(() => import('./pages/Security').then((module) => ({ default: module.Security })), 'security');
const AcceptInvitation = lazyWithRetry(() => import('./pages/AcceptInvitation').then((module) => ({ default: module.AcceptInvitation })), 'accept-invite');
const Dashboard = lazyWithRetry(() => import('./pages/Dashboard').then((module) => ({ default: module.Dashboard })), 'dashboard');
const Playground = lazyWithRetry(() => import('./pages/Playground').then((module) => ({ default: module.Playground })), 'playground');
const Instances = lazyWithRetry(() => import('./pages/Instances').then((module) => ({ default: module.Instances })), 'instances');
const Logs = lazyWithRetry(() => import('./pages/Logs').then((module) => ({ default: module.Logs })), 'logs');
const Models = lazyWithRetry(() => import('./pages/Models').then((module) => ({ default: module.Models })), 'models');
const ApiKeys = lazyWithRetry(() => import('./pages/ApiKeys').then((module) => ({ default: module.ApiKeys })), 'api-keys');
const WorkspaceAdmin = lazyWithRetry(() => import('./pages/WorkspaceAdmin').then((module) => ({ default: module.WorkspaceAdmin })), 'workspace');
const workspacePreferenceKeyPrefix = 'infera:last-workspace:';

// Query Client
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 2000,
    },
  },
});

// Navigation items — primary (always visible) and secondary (utility)
const primaryNavItems = [
  { path: '/', label: 'Dashboard' },
  { path: '/models', label: 'Models' },
  { path: '/instances', label: 'Nodes' },
  { path: '/playground', label: 'Playground' },
];

const secondaryNavItems = [
  { path: '/logs', label: 'Logs' },
  { path: '/api-keys', label: 'API Keys' },
  { path: '/workspace', label: 'Settings' },
];

type PageMeta = {
  title: string;
  navLabel: string;
  eyebrow: string;
  description: string;
};

const pageMeta: Record<string, PageMeta> = {
  '/': {
    title: 'Inference',
    navLabel: 'Inference',
    eyebrow: 'Workspace command center',
    description: 'Track deployment readiness, serving health, usage, and the next action for the active workspace.',
  },
  '/models': {
    title: 'Models',
    navLabel: 'Models',
    eyebrow: 'Registry and serving state',
    description: 'Curate the model catalog, inspect verification freshness, and move from registry to live serving without losing runtime context.',
  },
  '/instances': {
    title: 'Nodes',
    navLabel: 'Nodes',
    eyebrow: 'Infrastructure operations',
    description: 'Provision, inspect, and recover the infrastructure that runs your serving workloads.',
  },
  '/playground': {
    title: 'Playground',
    navLabel: 'Playground',
    eyebrow: 'Live request testing',
    description: 'Run real requests against the workspace and inspect output behavior, latency, and model response quality.',
  },
  '/logs': {
    title: 'Logs',
    navLabel: 'Logs',
    eyebrow: 'Operational history',
    description: 'Review runtime events, request traces, and recent operational signals without leaving the control surface.',
  },
  '/api-keys': {
    title: 'API Keys',
    navLabel: 'API Keys',
    eyebrow: 'Access and automation',
    description: 'Manage access keys for people, services, and integrations while keeping scope and ownership clear.',
  },
  '/workspace': {
    title: 'Workspace',
    navLabel: 'Settings',
    eyebrow: 'Admin and configuration',
    description: 'Configure providers, quota, memberships, and invitations for the active workspace.',
  },
};

// Top Navigation
function TopNav({ onLogout }: { onLogout: () => void }) {
  const location = useLocation();
  const isMobile = useIsMobile(900);
  const { session, availableWorkspaces, switchWorkspace, switchingWorkspace } = useAuthSession();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const currentPage = pageMeta[location.pathname];
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
            <div className="nav-brand-mark">INFERA.AI</div>
            <div className="nav-mobile-route-label">{currentPage?.navLabel || 'Infera'}</div>
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
      <div className="nav-brand-block nav-brand-block-desktop">
        <div className="nav-brand-mark">INFERA.AI</div>
        <div className="nav-brand-caption">Inference control plane</div>
      </div>
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
      <div className="nav-group nav-auth-group">
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
  const [playgroundMode, setPlaygroundMode] = useState<PlaygroundMode>('chat');
  const [selectedAgentID, setSelectedAgentID] = useState('');
  const [agentMaxSteps, setAgentMaxSteps] = useState(8);
  const [agentExecutionMode, setAgentExecutionMode] = useState<AgentExecutionMode>('operations');
  const [agentAnalysisDepth, setAgentAnalysisDepth] = useState<AgentAnalysisDepth>('standard');
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
    setPlaygroundMode('chat');
    setSelectedAgentID('');
    setAgentMaxSteps(8);
    setAgentExecutionMode('operations');
    setAgentAnalysisDepth('standard');
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
  const sessionKeyID = session?.key?.id ?? '';
  const workspaceID = session?.workspace?.id ?? '';
  const workspaceSlug = session?.workspace?.slug ?? '';
  const workspaceName = session?.workspace?.name ?? '';
  const fallbackWorkspaces = useMemo(() => (
    workspaceID
      ? [{
        id: workspaceID,
        slug: workspaceSlug,
        name: workspaceName,
        created_at: '',
        status: 'active',
      }]
      : []
  ), [workspaceID, workspaceName, workspaceSlug]);

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
      setPlaygroundMode('chat');
      setSelectedAgentID('');
      setAgentMaxSteps(8);
      setAgentExecutionMode('operations');
      setAgentAnalysisDepth('standard');
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
    if (!sessionKeyID) {
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
        setAvailableWorkspaces(fallbackWorkspaces);
      });

    return () => {
      active = false;
    };
  }, [fallbackWorkspaces, sessionKeyID]);

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

  // Listen for auth-expired events from authenticated client requests.
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
          <Route path="/" element={<PublicLanding />} />
          <Route path="/sign-in" element={<Login onAuthenticated={(nextSession: SessionInfo) => setSession(nextSession)} />} />
          <Route path="/docs" element={<PublicApiDocs />} />
          <Route path="/evaluation" element={<Evaluation />} />
          <Route path="/getting-started" element={<GettingStarted />} />
          <Route path="/trust" element={<Trust />} />
          <Route path="/company" element={<Company />} />
          <Route path="/security" element={<Security />} />
          <Route path="/accept-invite" element={<AcceptInvitation onAccepted={(nextSession: SessionInfo) => setSession(nextSession)} />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Suspense>
    );
  }

  const currentPage = pageMeta[location.pathname] ?? {
    title: 'Infera',
    navLabel: 'Infera',
    eyebrow: 'Workspace console',
    description: 'Monitor the platform, operating state, and user-facing surfaces from one place.',
  };
  const docsRoutes = ['/docs', '/evaluation', '/getting-started', '/trust', '/company', '/security'];
  const hideAppChrome = docsRoutes.includes(location.pathname);
  const hideDisplayHeader = hideAppChrome || location.pathname === '/playground';

  const chatContextValue: ChatContextType = {
    messages,
    setMessages,
    history,
    setHistory,
    playgroundMode,
    setPlaygroundMode,
    selectedAgentID,
    setSelectedAgentID,
    agentMaxSteps,
    setAgentMaxSteps,
    agentExecutionMode,
    setAgentExecutionMode,
    agentAnalysisDepth,
    setAgentAnalysisDepth,
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
      <AppShell variant="auth">
        {!hideAppChrome && <TopNav onLogout={handleLogout} />}
        {!hideDisplayHeader && (
          <PageHeader
            eyebrow={currentPage.eyebrow}
            title={currentPage.title}
            description={currentPage.description}
          />
        )}
        <Suspense fallback={<RouteLoader />}>
          <PageTransition>
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/playground" element={<Playground />} />
              <Route path="/models" element={<Models />} />
              <Route path="/instances" element={<Instances />} />
              <Route path="/logs" element={<Logs />} />
              <Route path="/api-keys" element={<ApiKeys />} />
              <Route path="/workspace" element={<WorkspaceAdmin />} />
              <Route path="/docs" element={<PublicApiDocs />} />
              <Route path="/evaluation" element={<Evaluation />} />
              <Route path="/getting-started" element={<GettingStarted />} />
              <Route path="/trust" element={<Trust />} />
              <Route path="/company" element={<Company />} />
              <Route path="/security" element={<Security />} />
              <Route path="/accept-invite" element={<AcceptInvitation onAccepted={(nextSession: SessionInfo) => setSession(nextSession)} />} />
              <Route path="/sign-in" element={<Navigate to="/" replace />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </PageTransition>
        </Suspense>
        <CommandPalette />
      </AppShell>
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
