import { useState, createContext, useContext } from 'react';
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
import type { ChatMessage } from './types';

// Chat message with metadata
interface Message extends ChatMessage {
  id: string;
  timestamp: Date;
}

// Chat Context - to persist chat state across page switches
interface ChatContextType {
  messages: Message[];
  setMessages: React.Dispatch<React.SetStateAction<Message[]>>;
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
];

// Page display titles
const pageTitles: Record<string, string> = {
  '/': 'INFERENCE',
  '/models': 'REGISTRY',
  '/instances': 'INSTANCES',
  '/playground': 'PLAYGROUND',
  '/logs': 'SYSTEM LOGS',
  '/api-keys': 'API KEYS',
};

// Top Navigation
function TopNav() {
  return (
    <nav className="top-nav">
      <div style={{ fontWeight: 700, letterSpacing: '-0.02em' }}>INFERA.AI</div>
      <div className="nav-group">
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
      <div className="nav-group" style={{ gap: '1rem' }}>
        <a href="/api/health" target="_blank" rel="noopener noreferrer" className="nav-link">
          DOCS
        </a>
      </div>
    </nav>
  );
}

// Main App Content
function AppContent() {
  const location = useLocation();

  // Chat state - persisted across page switches
  const [messages, setMessages] = useState<Message[]>([]);
  const [selectedModel, setSelectedModel] = useState<string>('');
  const [temperature, setTemperature] = useState(0.7);
  const [maxTokens, setMaxTokens] = useState(2048);

  const pageTitle = pageTitles[location.pathname] || 'INFERA';

  const chatContextValue: ChatContextType = {
    messages,
    setMessages,
    selectedModel,
    setSelectedModel,
    temperature,
    setTemperature,
    maxTokens,
    setMaxTokens,
  };

  return (
    <ChatContext.Provider value={chatContextValue}>
      <div className="app-shell">
        <TopNav />
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
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </div>
    </ChatContext.Provider>
  );
}

// Root App with Providers
function App() {
  return (
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
  );
}

export default App;
