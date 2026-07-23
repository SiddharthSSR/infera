import { Link } from 'react-router-dom';
import {
  designPartnerRequestEndpoint,
  getPublicAcquisitionTarget,
} from '../../lib/designPartnerRequest';
import { publicEvidenceLinks } from '../../lib/publicEvidence';
import { publicAnalytics } from '../../lib/publicAnalytics';

export interface PublicFooterProps {
  intakeEndpoint?: string;
}

export function PublicFooter({ intakeEndpoint = designPartnerRequestEndpoint }: PublicFooterProps) {
  const acquisition = getPublicAcquisitionTarget(intakeEndpoint);

  return (
    <footer className="public-footer">
      <div className="public-footer-brand">
        <span>INFERA.AI</span>
        <p>Public evidence is labeled by availability. No implied certifications, customers, or uptime promises.</p>
      </div>
      <nav className="public-footer-links" aria-label="Public information">
        <Link to="/evaluation">Evaluation guide</Link>
        <Link
          to={acquisition.path}
          onClick={() => publicAnalytics.track('public_primary_cta_clicked', {
            action: acquisition.action,
            placement: 'footer',
          })}
        >
          {acquisition.path === '/request-access' ? 'Request design-partner access' : 'Evaluate deployment fit'}
        </Link>
        <Link to="/getting-started">Migration quickstart</Link>
        <Link to="/trust">Trust</Link>
        <Link to="/company">Company</Link>
        <Link to="/security">Security</Link>
        <a href={publicEvidenceLinks.publicationReadiness} target="_blank" rel="noreferrer">
          Publication decisions<span className="sr-only"> (opens in a new tab)</span>
        </a>
        <a href={publicEvidenceLinks.repository} target="_blank" rel="noreferrer">
          Source repository<span className="sr-only"> (opens in a new tab)</span>
        </a>
      </nav>
    </footer>
  );
}
