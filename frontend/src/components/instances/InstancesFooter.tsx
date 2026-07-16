import { Badge, Cell, GridRow, LabelText } from '../shared';
import { getProviderDisplayName } from '../../lib/providerInventory';

export function InstancesFooter({
  providerSummary,
  totalCostPerHour,
}: {
  providerSummary: string[];
  totalCostPerHour: number;
}) {
  return (
    <GridRow className="instances-footer-row">
      <Cell>
        <LabelText as="div">PROVIDER</LabelText>
        <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
          {providerSummary.length > 0 ? providerSummary.map((provider) => getProviderDisplayName(provider)).join(', ') : '—'}
        </div>
      </Cell>
      <Cell>
        <LabelText as="div">TOTAL COST</LabelText>
        <div style={{ marginTop: '0.5rem', fontSize: '0.8rem' }}>
          ${totalCostPerHour.toFixed(2)}/hr
        </div>
      </Cell>
      <Cell span={2}>
        <LabelText as="div">TAGS</LabelText>
        <div style={{ display: 'flex', gap: '0.75rem', marginTop: '0.5rem' }}>
          <Badge>INFERENCE</Badge>
          <Badge>GPU</Badge>
          <Badge>PRODUCTION</Badge>
        </div>
      </Cell>
    </GridRow>
  );
}
