import { Link } from 'react-router-dom';
import { AppShell, PublicFooter, PublicNav, TrustStatus } from '../components/shared';
import { publicEvidenceLinks } from '../lib/publicEvidence';

const implementationEvidence = [
  {
    title: 'API authentication boundary',
    detail: 'The public API documentation describes workspace-scoped Bearer tokens and separates machine credentials from browser sessions.',
    href: publicEvidenceLinks.compatibility,
    linkLabel: 'Review compatibility documentation',
  },
  {
    title: 'Deployment recovery behavior',
    detail: 'The public recovery runbook documents gateway and worker recovery assumptions and explicit failure states.',
    href: publicEvidenceLinks.deploymentRecovery,
    linkLabel: 'Review recovery runbook',
  },
  {
    title: 'Shared audit ledger operation',
    detail: 'A public runbook documents shared-ledger migration, backup, restore, and rollback procedures.',
    href: publicEvidenceLinks.sharedAuditLedger,
    linkLabel: 'Review ledger runbook',
  },
  {
    title: 'Frontend ingress configuration',
    detail: 'The repository includes an ingress configuration with security headers. This is configuration evidence, not proof of every live deployment.',
    href: publicEvidenceLinks.ingressConfiguration,
    linkLabel: 'Inspect ingress configuration',
  },
];

const openPublicationDecisions = [
  'Approve a monitored, non-public vulnerability intake path',
  'Name supported versions, acknowledgement target, and disclosure expectations',
  'Approve privacy roles, data categories, retention, deletion, and request handling',
  'Approve subprocessors and transfer disclosures for the intended service scope',
  'Provide authoritative evidence before publishing audit or certification claims',
  'Assign ownership before linking a public status page or service commitment',
];

export function Security() {
  return (
    <AppShell variant="public" className="trust-shell">
      <a className="public-skip-link" href="#main-content">Skip to main content</a>
      <PublicNav title="SECURITY RECORD" />

      <main id="main-content">
        <header className="trust-page-header trust-page-header-security">
          <span className="landing-meta">Security / data handling</span>
          <h1>Inspect the controls. Keep the claims bounded.</h1>
          <p>
            This page points to security-relevant source and operational documentation that exists today. It does not
            claim that a particular deployment is certified, independently audited, breach-proof, or covered by an SLA.
          </p>
        </header>

        <section className="trust-section" aria-labelledby="security-evidence-heading">
          <div className="trust-section-heading">
            <div>
              <span className="landing-meta">Repository-backed controls</span>
              <h2 id="security-evidence-heading">Inspect the implementation record.</h2>
            </div>
            <p>Each link is an authoritative repository source. Deployment configuration must still be reviewed in the environment where Infera is operated.</p>
          </div>
          <div className="security-evidence-grid">
            {implementationEvidence.map((record) => (
              <article key={record.title}>
                <TrustStatus tone="available">Repository evidence</TrustStatus>
                <h3>{record.title}</h3>
                <p>{record.detail}</p>
                <a className="trust-evidence-link" href={record.href} target="_blank" rel="noreferrer">
                  {record.linkLabel}<span className="sr-only"> (opens in a new tab)</span>
                </a>
              </article>
            ))}
          </div>
        </section>

        <section className="trust-section trust-section-tone" aria-labelledby="security-gaps-heading">
          <div className="trust-section-heading">
            <div>
              <span className="landing-meta">Owner decisions</span>
              <h2 id="security-gaps-heading">A precise path to publishable policy.</h2>
            </div>
            <p>None of the items below should be inferred from source code, deployment assets, or internal operational documents.</p>
          </div>
          <ul className="security-gap-list">
            {openPublicationDecisions.map((material) => (
              <li key={material}>
                <TrustStatus tone="unavailable">Decision required</TrustStatus>
                <span>{material}</span>
              </li>
            ))}
          </ul>
          <a className="trust-decision-card" href={publicEvidenceLinks.publicationReadiness} target="_blank" rel="noreferrer">
            <span className="landing-meta">Publication boundary</span>
            <strong>Review the evidence table and the exact security, privacy, retention, status, and terms decisions.</strong>
            <span>Read the decision record<span className="sr-only"> (opens in a new tab)</span> ↗</span>
          </a>
          <aside className="trust-caution">
            <strong>No private security intake is published</strong>
            <p>
              Do not place vulnerability details, secrets, credentials, or personal data in a public GitHub issue.
              A private reporting path remains a named blocker.
            </p>
          </aside>
        </section>

        <section className="landing-final-cta" aria-labelledby="security-cta-heading">
          <div>
            <span className="landing-meta">Product evaluation</span>
            <h2 id="security-cta-heading">Keep evaluation narrow and observable.</h2>
          </div>
          <Link className="landing-button" to="/getting-started">Run the migration quickstart</Link>
        </section>
      </main>

      <PublicFooter />
    </AppShell>
  );
}
