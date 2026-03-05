import { useState, useEffect, createContext, useContext } from 'react';
import { Routes, Route, NavLink, useLocation, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import {
  LayoutDashboard, MessageSquare, Server, Database,
  Settings, ChevronLeft, ChevronRight, Sun, Moon,
  Terminal, Zap, Menu, X
} from 'lucide-react';
import { cn } from './lib/utils';
import { Dashboard } from './pages/Dashboard';
import { Playground } from './pages/Playground';
import { Instances } from './pages/Instances';
import { Logs } from './pages/Logs';
import { SettingsPage } from './pages/Settings';
import { Models } from './pages/Models';
import type { ChatMessage } from './types';

// Chat message with metadata
interface Message extends ChatMessage {
  id: string;
  timestamp: Date;
}

// Theme Context
interface ThemeContextType {
  theme: 'dark' | 'light';
  toggleTheme: () => void;
}

const ThemeContext = createContext<ThemeContextType>({
  theme: 'dark',
  toggleTheme: () => {},
});

export const useTheme = () => useContext(ThemeContext);

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
  { path: '/', label: 'Dashboard', icon: LayoutDashboard },
  { path: '/playground', label: 'Playground', icon: MessageSquare },
  { path: '/models', label: 'Models', icon: Database },
  { path: '/instances', label: 'Instances', icon: Server },
  { path: '/logs', label: 'Logs', icon: Terminal },
  { path: '/settings', label: 'Settings', icon: Settings },
];

// Page titles
const pageTitles: Record<string, { title: string; subtitle?: string }> = {
  '/': { title: 'Dashboard', subtitle: 'Overview of your inference platform' },
  '/playground': { title: 'Playground', subtitle: 'Test your models interactively' },
  '/models': { title: 'Model Registry', subtitle: 'Browse and manage your model catalog' },
  '/instances': { title: 'GPU Instances', subtitle: 'Manage your compute resources' },
  '/logs': { title: 'Logs', subtitle: 'Real-time system logs' },
  '/settings': { title: 'Settings', subtitle: 'Configure your platform' },
};

// Sidebar Component
function Sidebar({
  collapsed,
  onToggleCollapse,
  mobileOpen,
  onMobileClose,
}: {
  collapsed: boolean;
  onToggleCollapse: () => void;
  mobileOpen: boolean;
  onMobileClose: () => void;
}) {
  const { theme, toggleTheme } = useTheme();

  return (
    <>
      {/* Mobile backdrop */}
      {mobileOpen && (
        <div
          className="fixed inset-0 bg-background/80 backdrop-blur-sm z-40 md:hidden"
          onClick={onMobileClose}
        />
      )}

      <aside className={cn(
        "fixed left-0 top-0 h-screen bg-sidebar border-r border-sidebar-border z-40",
        "flex flex-col transition-all duration-300 ease-out",
        // Desktop
        "hidden md:flex",
        collapsed ? "md:w-16" : "md:w-64",
        // Mobile overlay
        mobileOpen && "!flex w-64"
      )}>
        {/* Logo */}
        <div className={cn(
          "flex items-center h-16 px-4 border-b border-sidebar-border",
          collapsed && !mobileOpen ? "justify-center" : "gap-3"
        )}>
          <div className="w-9 h-9 rounded-xl bg-primary flex items-center justify-center shadow-lg flex-shrink-0">
            <Zap className="w-5 h-5 text-primary-foreground" />
          </div>
          {(!collapsed || mobileOpen) && (
            <div className="animate-fade-in flex-1">
              <h1 className="text-lg font-bold text-sidebar-foreground">Infera</h1>
              <p className="text-xs text-muted-foreground -mt-0.5">AI Inference Platform</p>
            </div>
          )}
          {/* Mobile close button */}
          {mobileOpen && (
            <button onClick={onMobileClose} className="md:hidden p-1 text-muted-foreground hover:text-foreground">
              <X className="w-5 h-5" />
            </button>
          )}
        </div>

        {/* Navigation */}
        <nav className="flex-1 py-4 space-y-1 overflow-y-auto scrollbar-thin">
          {navItems.map((item) => {
            const Icon = item.icon;

            return (
              <NavLink
                key={item.path}
                to={item.path}
                end={item.path === '/'}
                onClick={onMobileClose}
                className={({ isActive }) => cn(
                  "flex items-center gap-3 px-3 py-2.5 mx-2 rounded-lg",
                  "text-sidebar-foreground font-medium text-sm",
                  "transition-all duration-200",
                  "hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                  isActive && "bg-sidebar-accent text-sidebar-primary border-l-2 border-primary",
                  collapsed && !mobileOpen && "justify-center mx-1 px-2"
                )}
                title={collapsed && !mobileOpen ? item.label : undefined}
              >
                {({ isActive }) => (
                  <>
                    <Icon className={cn("w-5 h-5 flex-shrink-0", isActive && "text-sidebar-primary")} />
                    {(!collapsed || mobileOpen) && <span className="animate-fade-in">{item.label}</span>}
                  </>
                )}
              </NavLink>
            );
          })}
        </nav>

        {/* Bottom section */}
        <div className="p-3 border-t border-sidebar-border space-y-2">
          {/* Theme Toggle */}
          <button
            onClick={toggleTheme}
            className={cn(
              "flex items-center gap-3 px-3 py-2.5 mx-2 rounded-lg w-full",
              "text-sidebar-foreground font-medium text-sm",
              "transition-all duration-200",
              "hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
              collapsed && !mobileOpen && "justify-center mx-0 px-2"
            )}
            title={collapsed && !mobileOpen ? (theme === 'dark' ? 'Light mode' : 'Dark mode') : undefined}
          >
            {theme === 'dark' ? (
              <Sun className="w-5 h-5 text-warning" />
            ) : (
              <Moon className="w-5 h-5 text-primary" />
            )}
            {(!collapsed || mobileOpen) && <span>{theme === 'dark' ? 'Light Mode' : 'Dark Mode'}</span>}
          </button>

          {/* Collapse Toggle (desktop only) */}
          <button
            onClick={onToggleCollapse}
            className={cn(
              "hidden md:flex items-center gap-3 px-3 py-2.5 mx-2 rounded-lg w-full",
              "text-sidebar-foreground font-medium text-sm",
              "transition-all duration-200",
              "hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
              collapsed && "justify-center mx-0 px-2"
            )}
            title={collapsed ? 'Expand' : 'Collapse'}
          >
            {collapsed ? (
              <ChevronRight className="w-5 h-5" />
            ) : (
              <>
                <ChevronLeft className="w-5 h-5" />
                <span>Collapse</span>
              </>
            )}
          </button>
        </div>
      </aside>
    </>
  );
}

// Top Bar Component
function TopBar({ title, subtitle, onMenuClick }: { title: string; subtitle?: string; onMenuClick: () => void }) {
  return (
    <header className="h-16 border-b border-border bg-background/80 backdrop-blur-sm flex items-center justify-between px-6 sticky top-0 z-30">
      <div className="flex items-center gap-3">
        <button onClick={onMenuClick} className="md:hidden p-2 -ml-2 text-muted-foreground hover:text-foreground rounded-lg">
          <Menu className="w-5 h-5" />
        </button>
        <div>
          <h1 className="text-xl font-semibold text-foreground">{title}</h1>
          {subtitle && <p className="text-sm text-muted-foreground">{subtitle}</p>}
        </div>
      </div>

      <div className="flex items-center gap-4">
        <div className="hidden md:flex items-center gap-6 text-sm">
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full bg-success shadow-[0_0_8px_var(--success)]" />
            <span className="text-muted-foreground">System Online</span>
          </div>
        </div>
      </div>
    </header>
  );
}

// Main App Content
function AppContent() {
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const location = useLocation();

  // Chat state - persisted across page switches
  const [messages, setMessages] = useState<Message[]>([]);
  const [selectedModel, setSelectedModel] = useState<string>('');
  const [temperature, setTemperature] = useState(0.7);
  const [maxTokens, setMaxTokens] = useState(512);

  const pageInfo = pageTitles[location.pathname] || { title: 'Infera' };

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
      <div className="min-h-screen bg-background">
        <Sidebar
          collapsed={sidebarCollapsed}
          onToggleCollapse={() => setSidebarCollapsed(!sidebarCollapsed)}
          mobileOpen={mobileMenuOpen}
          onMobileClose={() => setMobileMenuOpen(false)}
        />

        <main className={cn(
          "transition-all duration-300",
          "md:ml-16",
          !sidebarCollapsed && "md:ml-64"
        )}>
          <TopBar {...pageInfo} onMenuClick={() => setMobileMenuOpen(true)} />
          <div className="p-6">
            <Routes>
              <Route path="/" element={<Dashboard />} />
              <Route path="/playground" element={<Playground />} />
              <Route path="/models" element={<Models />} />
              <Route path="/instances" element={<Instances />} />
              <Route path="/logs" element={<Logs />} />
              <Route path="/settings" element={<SettingsPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </div>
        </main>
      </div>
    </ChatContext.Provider>
  );
}

// Root App with Providers
function App() {
  const [theme, setTheme] = useState<'dark' | 'light'>('dark');

  useEffect(() => {
    const savedTheme = localStorage.getItem('infera-theme') as 'dark' | 'light' | null;
    if (savedTheme) {
      setTheme(savedTheme);
    } else if (window.matchMedia('(prefers-color-scheme: light)').matches) {
      setTheme('light');
    }
  }, []);

  useEffect(() => {
    document.documentElement.classList.remove('light', 'dark');
    document.documentElement.classList.add(theme);
    localStorage.setItem('infera-theme', theme);
  }, [theme]);

  const toggleTheme = () => {
    setTheme(prev => prev === 'dark' ? 'light' : 'dark');
  };

  return (
    <ThemeContext.Provider value={{ theme, toggleTheme }}>
      <QueryClientProvider client={queryClient}>
        <AppContent />
        <Toaster
          theme={theme}
          position="bottom-right"
          toastOptions={{
            style: {
              background: 'var(--card)',
              border: '1px solid var(--border)',
              color: 'var(--foreground)',
            },
          }}
        />
      </QueryClientProvider>
    </ThemeContext.Provider>
  );
}

export default App;
