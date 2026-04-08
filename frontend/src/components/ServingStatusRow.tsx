import { useCountUp } from '../hooks/useCountUp';

export type ServingStatusItem = {
  label: string;
  value: number;
  description: string;
  actionLabel: string;
  onAction: () => void;
};

function AnimatedCount({ value }: { value: number }) {
  const display = useCountUp(value);
  return <>{Math.round(display)}</>;
}

export function ServingStatusRow({ items }: { items: ServingStatusItem[] }) {
  return (
    <div className="grid-row dashboard-serving-row">
      {items.map((item) => (
        <div key={item.label} className="cell dashboard-serving-cell">
          <div className="label-text">{item.label}</div>
          <div className="value-text" style={{ marginTop: '0.85rem' }}>
            <AnimatedCount value={item.value} />
          </div>
          <div className="dashboard-summary-text">{item.description}</div>
          <button className="action-btn" style={{ marginTop: '1rem' }} onClick={item.onAction}>
            {item.actionLabel}
          </button>
        </div>
      ))}
    </div>
  );
}
