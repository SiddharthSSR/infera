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

const missingSecurityMaterials = [
  'Private vulnerability-reporting channel',
  'Published security policy or response timeline',
  'Independent penetration-test report',
  'Compliance certification or attestation',
  'Public privacy policy, DPA, retention schedule, or subprocessor list',
  'Customer-facing SLA or public service-status history',
];

export function Security() {
  return (
    <AppShell variant="public" className="trust-shell">
      <a className="public-skip-link" href="#main-content">Skip to main content</a>
      <PublicNav title="SECURITY RECORD" />

      <main id="main-content">
        <header className="trust-page-header trust-page-header-security">
          <span className="landing-meta">Security / data handling</span>
          <h1>Implementation evidence is not a certification.</h1>
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
              <span className="landing-meta">Not published</span>
              <h2 id="security-gaps-heading">Security materials still blocking enterprise review.</h2>
            </div>
            <p>None of the items below should be inferred from source code, deployment assets, or internal operational documents.</p>
          </div>
          <ul className="security-gap-list">
            {missingSecurityMaterials.map((material) => (
              <li key={material}>
                <TrustStatus tone="unavailable">Unavailable</TrustStatus>
                <span>{material}</span>
              </li>
            ))}
          </ul>
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
