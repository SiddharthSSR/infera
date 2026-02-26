import { DollarSign, TrendingUp } from 'lucide-react';
import type { CostSummary } from '../types';

interface CostDisplayProps {
  costs: CostSummary | undefined;
  isLoading: boolean;
}

export function CostDisplay({ costs, isLoading }: CostDisplayProps) {
  if (isLoading || !costs) {
    return (
      <div className="stat-card">
        <div className="flex items-center justify-between mb-2">
          <span className="text-gray-400 text-sm font-medium">Cost</span>
          <DollarSign className="w-5 h-5 text-emerald-400" />
        </div>
        <div className="h-8 w-20 bg-gray-800 rounded animate-pulse" />
      </div>
    );
  }

  return (
    <div className="stat-card">
      <div className="flex items-center justify-between mb-2">
        <span className="text-gray-400 text-sm font-medium">Cost</span>
        <DollarSign className="w-5 h-5 text-emerald-400" />
      </div>
      <div className="text-2xl font-bold text-white">
        ${costs.current_hourly.toFixed(2)}<span className="text-sm text-gray-400 font-normal">/hr</span>
      </div>
      <div className="flex items-center gap-2 text-sm mt-1">
        <span className="text-gray-500">Today: ${costs.today_total.toFixed(2)}</span>
        <span className="text-gray-600">•</span>
        <span className="text-gray-500">Month: ${costs.month_total.toFixed(2)}</span>
      </div>
      {costs.projected_month > 0 && (
        <div className="flex items-center gap-1 text-xs text-amber-400 mt-2">
          <TrendingUp className="w-3 h-3" />
          <span>Projected: ${costs.projected_month.toFixed(2)}/mo</span>
        </div>
      )}
    </div>
  );
}
