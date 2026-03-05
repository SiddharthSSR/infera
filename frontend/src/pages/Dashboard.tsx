import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Server, Cpu, Activity, DollarSign,
  ArrowUpRight, Zap, MessageSquare, BarChart3
} from 'lucide-react';
import {
  AreaChart, Area, ResponsiveContainer, Tooltip, XAxis, YAxis, CartesianGrid
} from 'recharts';
import { cn } from '../lib/utils';
import { useWorkers, useStats, useInstances, useCosts, useModels } from '../hooks/useApi';

// Generate mock time-series data for charts
function generateMockTimeSeries(points: number, base: number, variance: number) {
  const now = Date.now();
  return Array.from({ length: points }, (_, i) => {
    const time = new Date(now - (points - 1 - i) * 5000);
    return {
      time: time.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
      value: Math.max(0, base + (Math.random() - 0.5) * variance),
    };
  });
}

function generateMockLatencyData(points: number) {
  const now = Date.now();
  return Array.from({ length: points }, (_, i) => {
    const time = new Date(now - (points - 1 - i) * 5000);
    const avg = 80 + Math.random() * 60;
    return {
      time: time.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }),
      avg: Math.round(avg),
      p50: Math.round(avg * 0.8),
      p99: Math.round(avg * 2.2),
    };
  });
}

// Sparkline for stat cards
function Sparkline({ data, color, height = 32 }: { data: number[]; color: string; height?: number }) {
  const sparkData = data.map((v, i) => ({ i, v }));
  return (
    <ResponsiveContainer width={60} height={height}>
      <AreaChart data={sparkData} margin={{ top: 2, right: 0, left: 0, bottom: 2 }}>
        <defs>
          <linearGradient id={`spark-${color}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity={0.3} />
            <stop offset="100%" stopColor={color} stopOpacity={0} />
          </linearGradient>
        </defs>
        <Area
          type="monotone"
          dataKey="v"
          stroke={color}
          strokeWidth={1.5}
          fill={`url(#spark-${color})`}
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  );
}

function StatCard({
  title,
  value,
  subtitle,
  icon: Icon,
  variant = 'default',
  sparkData,
}: {
  title: string;
  value: string | number;
  subtitle?: string;
  icon: typeof Activity;
  variant?: 'default' | 'success' | 'warning' | 'primary';
  sparkData?: number[];
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

  const sparkColors: Record<string, string> = {
    default: 'var(--muted-foreground)',
    success: 'var(--success)',
    warning: 'var(--warning)',
    primary: 'var(--primary)',
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
        {sparkData && <Sparkline data={sparkData} color={sparkColors[variant]} />}
      </div>
      <div className="text-2xl sm:text-3xl font-light tabular-nums font-mono text-foreground mb-1 truncate">{value}</div>
      <div className="text-xs uppercase tracking-wider text-muted-foreground">{title}</div>
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
          <div className="text-foreground font-mono tabular-nums font-medium">{worker.gpu_utilization}%</div>
        </div>
        <div>
          <div className="text-muted-foreground mb-1">Memory</div>
          <div className="text-foreground font-mono tabular-nums font-medium">{memoryPercent}%</div>
        </div>
        <div>
          <div className="text-muted-foreground mb-1">Latency</div>
          <div className="text-foreground font-mono tabular-nums font-medium">{worker.avg_latency_ms}ms</div>
        </div>
        <div>
          <div className="text-muted-foreground mb-1">RPS</div>
          <div className="text-foreground font-mono tabular-nums font-medium">{worker.requests_per_sec.toFixed(1)}</div>
        </div>
      </div>
    </div>
  );
}

// Custom tooltip for charts
function ChartTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null;
  return (
    <div className="bg-popover border border-border rounded-lg px-3 py-2 shadow-xl text-xs">
      <div className="text-muted-foreground mb-1">{label}</div>
      {payload.map((entry: any) => (
        <div key={entry.name} className="flex items-center gap-2">
          <div className="w-2 h-2 rounded-full" style={{ backgroundColor: entry.color }} />
          <span className="text-foreground font-mono tabular-nums">
            {entry.name}: {typeof entry.value === 'number' ? entry.value.toFixed(1) : entry.value}
          </span>
        </div>
      ))}
    </div>
  );
}

export function Dashboard() {
  const navigate = useNavigate();
  const { data: workers, isLoading: workersLoading } = useWorkers();
  const { data: stats } = useStats();
  const { data: instances } = useInstances();
  const { data: costs } = useCosts();
  const { data: models } = useModels();

  const activeInstances = instances?.filter(i => i.status === 'running') || [];
  const healthyWorkers = workers?.filter(w => w.status === 'healthy') || [];

  // Mock chart data (stable across renders via useMemo)
  const throughputData = useMemo(() => generateMockTimeSeries(20, 12, 8), []);
  const latencyData = useMemo(() => generateMockLatencyData(20), []);

  // Sparkline data for stat cards
  const instanceSparkData = useMemo(() => Array.from({ length: 10 }, () => Math.floor(Math.random() * 5) + 1), []);
  const workerSparkData = useMemo(() => Array.from({ length: 10 }, () => Math.floor(Math.random() * 4) + 1), []);
  const latencySparkData = useMemo(() => Array.from({ length: 10 }, () => Math.floor(Math.random() * 80) + 40), []);
  const costSparkData = useMemo(() => Array.from({ length: 10 }, () => Math.random() * 2 + 0.5), []);

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
          sparkData={instanceSparkData}
        />
        <StatCard
          icon={Cpu}
          title="Workers Online"
          value={healthyWorkers.length}
          subtitle={`${workers?.length || 0} registered`}
          variant="success"
          sparkData={workerSparkData}
        />
        <StatCard
          icon={Activity}
          title="Avg Latency"
          value={stats?.latency?.avg_ms ? `${stats.latency.avg_ms}ms` : '-'}
          subtitle="Response time"
          variant="default"
          sparkData={latencySparkData}
        />
        <StatCard
          icon={DollarSign}
          title="Cost / Hour"
          value={costs?.current_hourly ? `$${costs.current_hourly.toFixed(2)}` : '$0.00'}
          subtitle={costs?.today_total ? `$${costs.today_total.toFixed(2)} today` : 'No costs yet'}
          variant="warning"
          sparkData={costSparkData}
        />
      </div>

      {/* Performance Charts */}
      <div className="grid md:grid-cols-2 gap-6">
        <div className="bg-card border border-border rounded-xl p-6 shadow-sm">
          <h2 className="text-lg font-semibold tracking-tight text-foreground mb-4">Request Throughput</h2>
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={throughputData} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
              <defs>
                <linearGradient id="throughputGradient" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="var(--chart-1)" stopOpacity={0.3} />
                  <stop offset="100%" stopColor="var(--chart-1)" stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
              <XAxis dataKey="time" tick={{ fontSize: 10, fill: 'var(--muted-foreground)' }} tickLine={false} axisLine={false} />
              <YAxis tick={{ fontSize: 10, fill: 'var(--muted-foreground)' }} tickLine={false} axisLine={false} />
              <Tooltip content={<ChartTooltip />} />
              <Area
                type="monotone"
                dataKey="value"
                name="req/s"
                stroke="var(--chart-1)"
                strokeWidth={2}
                fill="url(#throughputGradient)"
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>

        <div className="bg-card border border-border rounded-xl p-6 shadow-sm">
          <h2 className="text-lg font-semibold tracking-tight text-foreground mb-4">Latency Distribution</h2>
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={latencyData} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
              <defs>
                <linearGradient id="avgGradient" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="var(--chart-2)" stopOpacity={0.3} />
                  <stop offset="100%" stopColor="var(--chart-2)" stopOpacity={0} />
                </linearGradient>
                <linearGradient id="p99Gradient" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="var(--chart-5)" stopOpacity={0.2} />
                  <stop offset="100%" stopColor="var(--chart-5)" stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
              <XAxis dataKey="time" tick={{ fontSize: 10, fill: 'var(--muted-foreground)' }} tickLine={false} axisLine={false} />
              <YAxis tick={{ fontSize: 10, fill: 'var(--muted-foreground)' }} tickLine={false} axisLine={false} unit="ms" />
              <Tooltip content={<ChartTooltip />} />
              <Area type="monotone" dataKey="p99" name="p99" stroke="var(--chart-5)" strokeWidth={1.5} fill="url(#p99Gradient)" />
              <Area type="monotone" dataKey="avg" name="avg" stroke="var(--chart-2)" strokeWidth={2} fill="url(#avgGradient)" />
              <Area type="monotone" dataKey="p50" name="p50" stroke="var(--chart-4)" strokeWidth={1.5} fill="none" strokeDasharray="4 4" />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      </div>

      {/* Main Content Grid */}
      <div className="grid lg:grid-cols-3 gap-6">
        {/* Quick Actions */}
        <div className="lg:col-span-2 space-y-4">
          <h2 className="text-lg font-semibold tracking-tight text-foreground">Quick Actions</h2>
          <div className="grid sm:grid-cols-2 gap-4">
            <QuickAction
              icon={MessageSquare}
              title="Open Playground"
              description="Test your models with interactive chat"
              onClick={() => navigate('/playground')}
            />
            <QuickAction
              icon={Server}
              title="New Instance"
              description="Provision a new GPU instance"
              onClick={() => navigate('/instances')}
            />
            <QuickAction
              icon={BarChart3}
              title="View Logs"
              description="Monitor real-time system logs"
              onClick={() => navigate('/logs')}
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
            <h2 className="text-lg font-semibold tracking-tight text-foreground">Active Workers</h2>
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
                onClick={() => navigate('/instances')}
                className="text-primary text-sm mt-2 hover:underline"
              >
                Provision an instance
              </button>
            </div>
          ) : (
            <div className="space-y-3">
              {healthyWorkers.slice(0, 3).map(worker => (
                <WorkerMiniCard key={worker.worker_id} worker={worker} />
              ))}
              {healthyWorkers.length > 3 && (
                <button
                  onClick={() => navigate('/instances')}
                  className="w-full text-center text-sm text-primary hover:text-primary/80 py-2"
                >
                  View all {healthyWorkers.length} workers
                </button>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Models Available */}
      {models && models.length > 0 && (
        <div className="bg-card border border-border rounded-xl p-6 shadow-sm">
          <h2 className="text-lg font-semibold tracking-tight text-foreground mb-4">Available Models</h2>
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
