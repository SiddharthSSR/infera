import { useState, useEffect, createContext, useContext } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { 
  LayoutDashboard, MessageSquare, Server, 
  Settings, ChevronLeft, ChevronRight, Sun, Moon,
  Terminal, Zap
} from 'lucide-react';
import { cn } from './lib/utils';
import { Dashboard } from './pages/Dashboard';
import { Playground } from './pages/Playground';
import { Instances } from './pages/Instances';
import { Logs } from './pages/Logs';
import { SettingsPage } from './pages/Settings';
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
  { id: 'dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { id: 'playground', label: 'Playground', icon: MessageSquare },
  { id: 'instances', label: 'Instances', icon: Server },
  { id: 'logs', label: 'Logs', icon: Terminal },
  { id: 'settings', label: 'Settings', icon: Settings },
];

// Sidebar Component
function Sidebar({ 
  currentPage, 
  onNavigate, 
  collapsed, 
  onToggleCollapse 
}: { 
  currentPage: string;
  onNavigate: (page: string) => void;
  collapsed: boolean;
  onToggleCollapse: () => void;
}) {
  const { theme, toggleTheme } = useTheme();

  return (
    <aside className={cn(
      "fixed left-0 top-0 h-screen bg-sidebar border-r border-sidebar-border z-40",
      "flex flex-col transition-all duration-300 ease-out",
      collapsed ? "w-16" : "w-64"
    )}>
      {/* Logo */}
      <div className={cn(
        "flex items-center h-16 px-4 border-b border-sidebar-border",
        collapsed ? "justify-center" : "gap-3"
      )}>
        <div className="w-9 h-9 rounded-xl bg-primary flex items-center justify-center shadow-lg flex-shrink-0">
          <Zap className="w-5 h-5 text-primary-foreground" />
        </div>
        {!collapsed && (
          <div className="animate-fade-in">
            <h1 className="text-lg font-bold text-sidebar-foreground">Infera</h1>
            <p className="text-xs text-muted-foreground -mt-0.5">AI Inference Platform</p>
          </div>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex-1 py-4 space-y-1 overflow-y-auto scrollbar-thin">
        {navItems.map((item) => {
          const Icon = item.icon;
          const isActive = currentPage === item.id;
          
          return (
            <button
              key={item.id}
              onClick={() => onNavigate(item.id)}
              className={cn(
                "flex items-center gap-3 px-3 py-2.5 mx-2 rounded-lg",
                "text-sidebar-foreground font-medium text-sm",
                "transition-all duration-200",
                "hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                isActive && "bg-sidebar-accent text-sidebar-primary",
                collapsed && "justify-center mx-1 px-2"
              )}
              title={collapsed ? item.label : undefined}
            >
              <Icon className={cn("w-5 h-5 flex-shrink-0", isActive && "text-sidebar-primary")} />
              {!collapsed && <span className="animate-fade-in">{item.label}</span>}
            </button>
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
            collapsed && "justify-center mx-0 px-2"
          )}
          title={collapsed ? (theme === 'dark' ? 'Light mode' : 'Dark mode') : undefined}
        >
          {theme === 'dark' ? (
            <Sun className="w-5 h-5 text-warning" />
          ) : (
            <Moon className="w-5 h-5 text-primary" />
          )}
          {!collapsed && <span>{theme === 'dark' ? 'Light Mode' : 'Dark Mode'}</span>}
        </button>

        {/* Collapse Toggle */}
        <button
          onClick={onToggleCollapse}
          className={cn(
            "flex items-center gap-3 px-3 py-2.5 mx-2 rounded-lg w-full",
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
  );
}

// Top Bar Component
function TopBar({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <header className="h-16 border-b border-border bg-background/80 backdrop-blur-sm flex items-center justify-between px-6 sticky top-0 z-30">
      <div>
        <h1 className="text-xl font-semibold text-foreground">{title}</h1>
        {subtitle && <p className="text-sm text-muted-foreground">{subtitle}</p>}
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

// Page titles
const pageTitles: Record<string, { title: string; subtitle?: string }> = {
  dashboard: { title: 'Dashboard', subtitle: 'Overview of your inference platform' },
  playground: { title: 'Playground', subtitle: 'Test your models interactively' },
  instances: { title: 'GPU Instances', subtitle: 'Manage your compute resources' },
  logs: { title: 'Logs', subtitle: 'Real-time system logs' },
  settings: { title: 'Settings', subtitle: 'Configure your platform' },
};

// Main App Content
function AppContent() {
  const [currentPage, setCurrentPage] = useState('dashboard');
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  
  // Chat state - persisted across page switches
  const [messages, setMessages] = useState<Message[]>([]);
  const [selectedModel, setSelectedModel] = useState<string>('');
  const [temperature, setTemperature] = useState(0.7);
  const [maxTokens, setMaxTokens] = useState(512);

  const pageInfo = pageTitles[currentPage] || { title: 'Infera' };

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

  const renderPage = () => {
    switch (currentPage) {
      case 'dashboard':
        return <Dashboard onNavigate={setCurrentPage} />;
      case 'playground':
        return <Playground />;
      case 'instances':
        return <Instances />;
      case 'logs':
        return <Logs />;
      case 'settings':
        return <SettingsPage />;
      default:
        return <Dashboard onNavigate={setCurrentPage} />;
    }
  };

  return (
    <ChatContext.Provider value={chatContextValue}>
      <div className="min-h-screen bg-background">
        <Sidebar 
          currentPage={currentPage}
          onNavigate={setCurrentPage}
          collapsed={sidebarCollapsed}
          onToggleCollapse={() => setSidebarCollapsed(!sidebarCollapsed)}
        />
        
        <main className={cn(
          "transition-all duration-300",
          sidebarCollapsed ? "ml-16" : "ml-64"
        )}>
          <TopBar {...pageInfo} />
          <div className="p-6">
            {renderPage()}
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
      </QueryClientProvider>
    </ThemeContext.Provider>
  );
}

export default App;
