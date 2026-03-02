import { 
  Server, Cpu, Activity, DollarSign, 
  ArrowUpRight, Zap, MessageSquare, BarChart3
} from 'lucide-react';
import { cn } from '../lib/utils';
import { useWorkers, useStats, useInstances, useCosts, useModels } from '../hooks/useApi';

interface DashboardProps {
  onNavigate: (page: string) => void;
}

function StatCard({ 
  title, 
  value, 
  subtitle, 
  icon: Icon, 
  variant = 'default'
}: { 
  title: string;
  value: string | number;
  subtitle?: string;
  icon: typeof Activity;
  variant?: 'default' | 'success' | 'warning' | 'primary';
}) {
  const variantClasses = {
    default: 'bg-card border-border shadow-sm',
    success: 'bg-success/10 border-success/30 shadow-sm shadow-success/5',
    warning: 'bg-warning/10 border-warning/30 shadow-sm shadow-warning/5',
    primary: 'bg-primary/10 border-primary/30 shadow-sm shadow-primary/5',
  };

  const iconVariants = {
    default: 'bg-muted text-muted-foreground',
    success: 'bg-success/20 text-success',
    warning: 'bg-warning/20 text-warning',
    primary: 'bg-primary/20 text-primary',
  };

  return (
    <div className={cn(
      "rounded-xl border p-4 transition-all duration-200 hover:shadow-md",
      variantClasses[variant]
    )}>
      <div className="flex items-start justify-between mb-3">
        <div className={cn("w-10 h-10 rounded-lg flex items-center justify-center", iconVariants[variant])}>
          <Icon className="w-5 h-5" />
        </div>
      </div>
      <div className="text-2xl font-bold text-foreground mb-1">{value}</div>
      <div className="text-sm text-muted-foreground">{title}</div>
      {subtitle && <div className="text-xs text-muted-foreground/70 mt-1">{subtitle}</div>}
    </div>
  );
}

function QuickAction({ 
  icon: Icon, 
  title, 
  description, 
  onClick 
}: { 
  icon: typeof Activity;
  title: string;
  description: string;
  onClick: () => void;
}) {
  return (
    <button 
      onClick={onClick}
      className={cn(
        "bg-card border border-border rounded-xl p-6 text-left group shadow-sm",
        "hover:border-primary/50 hover:shadow-lg transition-all duration-200 hover:-translate-y-0.5"
      )}
    >
      <div className="flex items-start gap-4">
        <div className="w-12 h-12 rounded-lg bg-muted group-hover:bg-primary/10 flex items-center justify-center transition-colors border border-border/50">
          <Icon className="w-6 h-6 text-muted-foreground group-hover:text-primary transition-colors" />
        </div>
        <div className="flex-1">
          <h3 className="font-semibold text-foreground group-hover:text-primary transition-colors">{title}</h3>
          <p className="text-sm text-muted-foreground mt-1">{description}</p>
        </div>
        <ArrowUpRight className="w-5 h-5 text-muted-foreground group-hover:text-primary transition-colors" />
      </div>
    </button>
  );
}

function WorkerMiniCard({ worker }: { worker: any }) {
  const memoryPercent = worker.memory_total > 0 
    ? Math.round((worker.memory_used / worker.memory_total) * 100) 
    : 0;

  return (
    <div className="p-4 bg-muted/50 border border-border rounded-lg hover:border-primary/30 transition-colors">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-3">
          <div className={cn(
            "w-2 h-2 rounded-full",
            worker.status === 'healthy' 
              ? "bg-success shadow-[0_0_8px_var(--success)]" 
              : "bg-warning shadow-[0_0_8px_var(--warning)]"
          )} />
          <span className="font-medium text-foreground text-sm truncate max-w-[150px]">
            {worker.worker_id.slice(0, 8)}
          </span>
        </div>
        <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-primary/10 text-primary border border-primary/20">
          {worker.models?.[0]?.split('/').pop() || 'No model'}
        </span>
      </div>
      
      <div className="grid grid-cols-2 gap-3 text-xs">
        <div>
          <div className="text-muted-foreground mb-1">GPU</div>
          <div className="text-foreground font-medium">{worker.gpu_utilization}%</div>
        </div>
        <div>
          <div className="text-muted-foreground mb-1">Memory</div>
          <div className="text-foreground font-medium">{memoryPercent}%</div>
        </div>
        <div>
          <div className="text-muted-foreground mb-1">Latency</div>
          <div className="text-foreground font-medium">{worker.avg_latency_ms}ms</div>
        </div>
        <div>
          <div className="text-muted-foreground mb-1">RPS</div>
          <div className="text-foreground font-medium">{worker.requests_per_sec.toFixed(1)}</div>
        </div>
      </div>
    </div>
  );
}

export function Dashboard({ onNavigate }: DashboardProps) {
  const { data: workers, isLoading: workersLoading } = useWorkers();
  const { data: stats } = useStats();
  const { data: instances } = useInstances();
  const { data: costs } = useCosts();
  const { data: models } = useModels();

  const activeInstances = instances?.filter(i => i.status === 'running') || [];
  const healthyWorkers = workers?.filter(w => w.status === 'healthy') || [];

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Stats Grid */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          icon={Server}
          title="Active Instances"
          value={activeInstances.length}
          subtitle={`${instances?.length || 0} total`}
          variant="primary"
        />
        <StatCard
          icon={Cpu}
          title="Workers Online"
          value={healthyWorkers.length}
          subtitle={`${workers?.length || 0} registered`}
          variant="success"
        />
        <StatCard
          icon={Activity}
          title="Avg Latency"
          value={stats?.latency?.avg_ms ? `${stats.latency.avg_ms}ms` : '-'}
          subtitle="Response time"
          variant="default"
        />
        <StatCard
          icon={DollarSign}
          title="Cost / Hour"
          value={costs?.current_hourly ? `$${costs.current_hourly.toFixed(2)}` : '$0.00'}
          subtitle={costs?.today_total ? `$${costs.today_total.toFixed(2)} today` : 'No costs yet'}
          variant="warning"
        />
      </div>

      {/* Main Content Grid */}
      <div className="grid lg:grid-cols-3 gap-6">
        {/* Quick Actions */}
        <div className="lg:col-span-2 space-y-4">
          <h2 className="text-lg font-semibold text-foreground">Quick Actions</h2>
          <div className="grid sm:grid-cols-2 gap-4">
            <QuickAction
              icon={MessageSquare}
              title="Open Playground"
              description="Test your models with interactive chat"
              onClick={() => onNavigate('playground')}
            />
            <QuickAction
              icon={Server}
              title="New Instance"
              description="Provision a new GPU instance"
              onClick={() => onNavigate('instances')}
            />
            <QuickAction
              icon={BarChart3}
              title="View Logs"
              description="Monitor real-time system logs"
              onClick={() => onNavigate('logs')}
            />
            <QuickAction
              icon={Zap}
              title="API Documentation"
              description="Integrate with OpenAI-compatible API"
              onClick={() => window.open('/api/health', '_blank')}
            />
          </div>
        </div>

        {/* Active Workers */}
        <div className="bg-card border border-border rounded-xl p-6 shadow-sm">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold text-foreground">Active Workers</h2>
            <span className="px-2.5 py-1 rounded-full text-xs font-medium bg-success/10 text-success border border-success/30">
              {healthyWorkers.length} online
            </span>
          </div>

          {workersLoading ? (
            <div className="space-y-3">
              {[1, 2].map(i => (
                <div key={i} className="h-24 bg-muted rounded-lg animate-pulse" />
              ))}
            </div>
          ) : healthyWorkers.length === 0 ? (
            <div className="text-center py-8">
              <div className="w-12 h-12 rounded-lg bg-muted flex items-center justify-center mx-auto mb-3">
                <Cpu className="w-6 h-6 text-muted-foreground" />
              </div>
              <p className="text-muted-foreground text-sm">No workers online</p>
              <button 
                onClick={() => onNavigate('instances')}
                className="text-primary text-sm mt-2 hover:underline"
              >
                Provision an instance →
              </button>
            </div>
          ) : (
            <div className="space-y-3">
              {healthyWorkers.slice(0, 3).map(worker => (
                <WorkerMiniCard key={worker.worker_id} worker={worker} />
              ))}
              {healthyWorkers.length > 3 && (
                <button 
                  onClick={() => onNavigate('instances')}
                  className="w-full text-center text-sm text-primary hover:text-primary/80 py-2"
                >
                  View all {healthyWorkers.length} workers →
                </button>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Models Available */}
      {models && models.length > 0 && (
        <div className="bg-card border border-border rounded-xl p-6 shadow-sm">
          <h2 className="text-lg font-semibold text-foreground mb-4">Available Models</h2>
          <div className="flex flex-wrap gap-2">
            {models.map(model => (
              <span key={model.id} className="px-3 py-1.5 rounded-full text-sm font-medium bg-primary/10 text-primary border border-primary/30">
                {model.id.split('/').pop()}
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}